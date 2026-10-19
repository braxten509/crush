<script module>
  // One figure (desktop-design 4.3): no box; a heading (the title, its last two
  // words kept together as SidePanel.svelte does) that shows the figure's chip
  // in the chat, then the body. Inline, while the right panel is hidden, a
  // small-caps caption line (kind and title) takes the heading's place.
  //   diagram  Mermaid (mermaid.js), in idle time; its source until then, and
  //            the source with DIAGRAM_FAILED when it cannot be drawn
  //   math     KaTeX display, centered, 1.1em, in idle time
  //   table    the picture's table (SidePanel.svelte); wide tables scroll
  //   picture  data: and blob: pictures as they are, a file through file_read,
  //            a web picture as a link chip (the content rule blocks loading
  //            it); full width at most, a faint frame; a click opens the viewer
  // A figure still streaming (its block not yet stable) shows a diagram's or
  // math's source only. The root's data-state says where the drawing is:
  // "source", "pending", "drawn", "fallback" or "link" (the safety probe and the
  // checks read it). Placed by VisualsSection (P3.8); MarkdownView inline.
  export function keepLastWords(label) {
    const text = String(label ?? "");
    return text.split(" ").length > 2 ? text.replace(/ (\S+)$/, " $1") : text;
  }

  // Pictures read from a file: the attachments' limit (TYPE-012).
  export const PICTURE_MAX_BYTES = 5 * 1024 * 1024;
  export const PICTURE_FAILED = "This picture could not be shown here.";
</script>

<script>
  import { untrack } from "svelte";
  import { file_read } from "../../lib/engine/commands.js";
  import { KIND_LABEL, pictureOf } from "../../lib/render/figures.js";
  import { linkGuard } from "../../lib/render/links.js";
  import { renderMath } from "../../lib/render/math.js";
  import { DIAGRAM_FAILED, renderDiagram, whenIdle } from "../../lib/render/mermaid.js";
  import { drawTable } from "../../lib/render/table.js";
  import { conn } from "../../lib/stores/connection.svelte.js";
  import { openViewer, revealInChat } from "../../lib/stores/ui.svelte.js";
  import VisualChip from "./VisualChip.svelte";

  let { figure, inline = false, streaming = false } = $props();

  // A press that moves farther than this before the release is a drag.
  const DRAG_SLOP = 4;

  const picture = $derived(figure.kind === "picture" ? pictureOf(figure.src ?? figure.source) : null);
  const web = $derived(picture?.kind === "web");
  // Redraw only when what is drawn changes, not when the panel hands over a
  // new object for the same figure.
  const drawKey = $derived(`${figure.kind}\u0000${figure.source}\u0000${figure.src ?? ""}\u0000${streaming ? 1 : 0}`);

  let body = $state(null);
  let drawState = $state("pending");
  let press = null;

  function sourceBlock(doc, source) {
    const pre = doc.createElement("pre");
    pre.className = "md-code figure-source";
    const code = doc.createElement("code");
    code.className = "md-code-body";
    code.textContent = source;
    pre.append(code);
    return pre;
  }

  function line(doc, text) {
    const p = doc.createElement("p");
    p.className = "figure-failed";
    p.textContent = text;
    return p;
  }

  function fitDiagram(target) {
    // Scaled to the width it is given (the styles below), so its text scales
    // with it: Mermaid's own size limits go.
    const svg = target.querySelector("svg");
    if (!svg) return;
    svg.setAttribute("width", "100%");
    svg.removeAttribute("height");
    svg.style.removeProperty("max-width");
    svg.style.removeProperty("height");
  }

  function draw(target, current) {
    const doc = target.ownerDocument;
    const { kind, source, title } = figure;
    if (kind === "table") {
      const table = drawTable(source, { doc });
      target.replaceChildren(table ?? sourceBlock(doc, source));
      drawState = table ? "drawn" : "fallback";
      return;
    }
    if (kind === "picture") return drawPicture(target, doc, title, current);
    target.replaceChildren(sourceBlock(doc, source));
    if (streaming) {
      drawState = "source";
      return;
    }
    drawState = "pending";
    whenIdle(async () => {
      if (!current()) return;
      if (kind === "diagram") {
        const fragment = await renderDiagram(source);
        if (!current()) return;
        if (fragment) {
          target.replaceChildren(fragment);
          fitDiagram(target);
          drawState = "drawn";
        } else {
          target.replaceChildren(sourceBlock(doc, source), line(doc, DIAGRAM_FAILED));
          drawState = "fallback";
        }
      } else {
        const fragment = renderMath(source, { display: true, doc });
        if (!fragment) {
          drawState = "fallback";
          return;
        }
        const holder = doc.createElement("div");
        holder.className = "figure-math";
        holder.append(fragment);
        target.replaceChildren(holder);
        drawState = "drawn";
      }
    });
  }

  function showPicture(target, doc, src, alt) {
    const img = doc.createElement("img");
    img.className = "figure-picture";
    img.alt = alt;
    img.tabIndex = 0;
    img.addEventListener("error", () => {
      target.replaceChildren(line(doc, PICTURE_FAILED));
      drawState = "fallback";
    });
    img.src = src;
    target.replaceChildren(img);
    drawState = "drawn";
  }

  function drawPicture(target, doc, title, current) {
    if (!picture) {
      target.replaceChildren(line(doc, PICTURE_FAILED));
      drawState = "fallback";
      return;
    }
    if (picture.kind === "data" || picture.kind === "blob") return showPicture(target, doc, picture.src, title);
    target.replaceChildren();
    drawState = "pending";
    file_read(picture.src, conn.project.path || conn.project.cwd, PICTURE_MAX_BYTES).then(
      ({ mime, base64 }) => {
        if (!current()) return;
        if (/^image\//i.test(mime ?? "")) showPicture(target, doc, `data:${mime};base64,${base64}`, title);
        else {
          target.replaceChildren(line(doc, PICTURE_FAILED));
          drawState = "fallback";
        }
      },
      () => {
        if (!current()) return;
        target.replaceChildren(line(doc, PICTURE_FAILED));
        drawState = "fallback";
      },
    );
  }

  $effect(() => {
    const target = body;
    drawKey;
    if (!target) {
      if (web) drawState = "link";
      return;
    }
    let live = true;
    untrack(() => draw(target, () => live));
    return () => {
      live = false;
    };
  });

  function showInChat() {
    revealInChat({ sessionId: figure.sessionId, messageId: figure.messageId, figureId: figure.id });
  }

  function down(event) {
    press = event.button === 0 ? { x: event.clientX, y: event.clientY } : null;
  }

  function viewPicture() {
    openViewer(picture.kind === "file" ? { path: picture.src, name: picture.name || figure.title } : { src: picture.src, name: figure.title });
  }

  function openPicture(event) {
    if (!picture || event.button !== 0 || !event.target.closest?.("img")) return;
    if (!press || Math.hypot(event.clientX - press.x, event.clientY - press.y) > DRAG_SLOP) return;
    viewPicture();
  }

  function pictureKey(event) {
    if (!picture || !event.target.closest?.("img") || (event.key !== "Enter" && event.key !== " ")) return;
    event.preventDefault();
    viewPicture();
  }
</script>

<figure class="figure-view" class:inline data-figure={figure.id} data-kind={figure.kind} data-state={drawState} use:linkGuard>
  {#if inline}
    <figcaption class="caption"
      ><span class="caption-kind">{KIND_LABEL[figure.kind]}</span><span class="caption-dotbox"><span class="caption-dot"></span></span><span
        class="caption-title">{figure.title}</span
      ></figcaption
    >
  {:else}
    <figcaption><button type="button" class="heading" onclick={showInChat}>{keepLastWords(figure.title)}</button></figcaption>
  {/if}
  {#if web}
    <div class="figure-body md">
      <VisualChip figureId={figure.id} kind="picture" title={figure.title} href={picture.src} host={picture.host} />
    </div>
  {:else}
    <!-- The handlers act on the picture inside, which is focusable. -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="figure-body md" bind:this={body} onmousedown={down} onclick={openPicture} onkeydown={pictureKey}></div>
  {/if}
</figure>

<style>
  .figure-view {
    margin: 0;
    min-width: 0;
  }
  .heading {
    font: var(--t-heading);
    color: var(--ink);
  }
  .heading:hover {
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }
  .figure-body {
    margin-top: calc(16 * var(--px));
  }

  /* Inline (panel hidden): the chip's line, without the box, over the figure. */
  .caption {
    display: flex;
    align-items: baseline;
    white-space: nowrap;
    min-width: 0;
  }
  .caption-kind {
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
    color: var(--slate);
  }
  .caption-dotbox {
    font: var(--t-small);
  }
  .caption-dot {
    display: inline-block;
    vertical-align: middle;
    width: calc(4 * var(--px));
    height: calc(4 * var(--px));
    margin: 0 calc(10 * var(--px)) 0 calc(9 * var(--px));
    border-radius: 50%;
    background: var(--ink);
  }
  .caption-title {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    font: var(--t-small);
  }
  .inline .figure-body {
    margin-top: calc(8 * var(--px));
  }
  .inline :global(.figure-math),
  .inline :global(svg) {
    max-width: calc(540 * var(--px));
  }

  .figure-body :global(.figure-failed) {
    font: var(--t-small);
    color: var(--slate);
  }

  .figure-body :global(.figure-math) {
    text-align: center;
    overflow-x: auto;
  }
  .figure-body :global(.figure-math .katex) {
    font-size: 1.1em;
  }

  .figure-body :global(svg) {
    display: block;
    width: 100%;
    max-width: 100%;
    height: auto;
  }

  .figure-body :global(.figure-picture) {
    display: block;
    box-sizing: border-box;
    max-width: 100%;
    height: auto;
    border: 1px solid var(--rule-faint);
    cursor: zoom-in;
  }

  /* The picture's table (SidePanel.svelte); a wide one scrolls inside the figure. */
  .figure-body :global(.figure-table) {
    overflow-x: auto;
    scrollbar-width: thin;
    scrollbar-color: var(--rule-control) transparent;
  }
  .figure-body :global(.md-table) {
    width: 100%;
    border-collapse: collapse;
  }
  .figure-body :global(.md-table :is(th, td)) {
    text-align: left;
    padding-left: calc(17 * var(--px));
    padding-right: calc(17 * var(--px));
    border: 1px solid var(--rule-table-inner);
    white-space: nowrap;
  }
  .figure-body :global(.md-table th) {
    padding-top: calc(15 * var(--px));
    padding-bottom: calc(10 * var(--px));
    font: var(--t-label);
    letter-spacing: 0.02em;
    text-transform: uppercase;
  }
  .figure-body :global(.md-table td) {
    padding-top: calc(12 * var(--px));
    padding-bottom: calc(9 * var(--px));
    font: var(--t-body-small);
  }
  .figure-body :global(.md-table thead th) {
    border-top-color: var(--rule-table);
  }
  .figure-body :global(.md-table tr > :first-child) {
    border-left-color: var(--rule-table);
  }
  .figure-body :global(.md-table tr > :last-child) {
    border-right-color: var(--rule-table);
  }
  .figure-body :global(.md-table tbody tr:last-child td) {
    border-bottom-color: var(--rule-table);
  }
  .figure-body :global(.md-table .md-align-center) {
    text-align: center;
  }
  .figure-body :global(.md-table .md-align-right) {
    text-align: right;
  }
</style>
