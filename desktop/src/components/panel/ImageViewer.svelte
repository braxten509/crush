<script>
  // The picture viewer (4.5, CHAT-007, TYPE-037): a picture chip, a picture in
  // a reply or a figure in VISUALS opens it over the whole window. The paper
  // scrim, a bar with the name, "W × H · N KB", Fit, Actual size and Close Esc,
  // a rule, then the picture centered 24 in from the edges, fit
  // by default; Actual size (or F) shows it 1:1, panned by dragging or by the
  // arrow keys in steps of 40. Transparent pictures sit on --paper-raised.
  // Esc closes; focus stays inside while it is open and goes back to what
  // opened it. Its keys take the overlay scope, which is the only one asked
  // while the viewer is open (lib/keys.js). Placed by OverlayHost.
  //
  // The save button (an arrow into a box, left of Fit) saves the picture
  // through the system's Save window (electron/main.cjs, will-download). A
  // picture attached but not sent yet (ui.viewer.attachment) also gets the
  // red pen and Undo (Ctrl+Z): lines drawn on it go into the attachment
  // when the viewer closes (marks.js, markAttachment). Owner, 2026-11-01.
  import { unfold } from "../../lib/motion.js";
  import { tick, untrack } from "svelte";
  import { file_read } from "../../lib/engine/commands.js";
  import { markAttachment } from "../../lib/stores/composer.svelte.js";
  import Icon from "../Icon.svelte";
  import { bakeMarks, drawLines, penWidth } from "./marks.js";
  import { bindKey } from "../../lib/keys.js";
  import { conn } from "../../lib/stores/connection.svelte.js";
  import { openViewer, ui } from "../../lib/stores/ui.svelte.js";
  import { PICTURE_FAILED, PICTURE_MAX_BYTES } from "../render/FigureView.svelte";
  import Rule from "../Rule.svelte";
  import { viewing } from "./panel.svelte.js";
  import { base64Bytes, dataBytes, dataMime, pictureName, pictureSize, saveName, viewerSource } from "./viewer.js";

  const PAN_STEP = 40;
  // Ctrl + mouse wheel zooms in up to 10 times the starting size, never
  // smaller than it (owner, 2026-11-08).
  const MAX_ZOOM = 10;

  let root = $state(null);
  let canvas = $state(null);
  let picture = $state(null);
  let sheet = $state(null);
  let pen = $state(false);
  let lines = $state([]);
  let place = $state({ left: 0, top: 0, width: 0, height: 0 });
  let src = $state("");
  let bytes = $state(null);
  let natural = $state({ width: 0, height: 0 });
  let zoom = $state(1);
  let base = $state({ width: 0, height: 0 });
  let opener = null;

  const pic = $derived(ui.viewer?.video ? null : ui.viewer);
  const open = $derived(Boolean(pic));
  const zoomed = $derived(zoom > 1 && base.width > 0);
  const movable = $derived(!viewing.fit || zoomed);
  const markable = $derived(Boolean(pic?.attachment));
  const name = $derived(pictureName(pic));
  const size = $derived(pictureSize(natural.width, natural.height, bytes));
  const ratio = () => globalThis.devicePixelRatio || 1;
  const scale = () => parseFloat(getComputedStyle(document.documentElement).fontSize) / 16;

  $effect(() => {
    viewing.name = open ? name : "";
    viewing.size = open ? size : "";
    viewing.pen = pen;
    viewing.lines = lines.length;
  });

  // A new picture: read it, from the project folder or as it is.
  $effect(() => {
    const source = viewerSource(pic);
    src = "";
    bytes = null;
    natural = { width: 0, height: 0 };
    viewing.fit = true;
    zoom = 1;
    pen = false;
    lines = [];
    // Closing (or another picture) puts the lines into the attachment.
    const attachment = pic?.attachment;
    const keep = () => {
      if (!attachment) return;
      const drawn = untrack(() => $state.snapshot(lines));
      const from = untrack(() => src);
      if (drawn.length && from) bakeMarks(from, drawn).then((baked) => markAttachment(attachment, baked), () => {});
    };
    if (!pic) {
      viewing.state = "idle";
      return;
    }
    if (!source) {
      viewing.state = "failed";
      return;
    }
    if (source.kind === "url") {
      viewing.state = "loading";
      src = source.src;
      bytes = dataBytes(source.src);
      return keep;
    }
    let live = true;
    viewing.state = "loading";
    const cwd = untrack(() => conn.project.path || conn.project.cwd);
    file_read(source.path, cwd, PICTURE_MAX_BYTES).then(
      (answer) => {
        if (!live) return;
        if (!/^image\//iu.test(answer?.mime ?? "")) {
          viewing.state = "failed";
          return;
        }
        bytes = base64Bytes(answer.base64);
        src = `data:${answer.mime};base64,${answer.base64}`;
      },
      () => {
        if (live) viewing.state = "failed";
      },
    );
    return () => {
      live = false;
      keep();
    };
  });

  function loaded(event) {
    natural = { width: event.currentTarget.naturalWidth, height: event.currentTarget.naturalHeight };
    viewing.state = "shown";
  }

  function failed() {
    viewing.state = "failed";
  }

  function close() {
    openViewer(null);
  }

  // Save: the picture as shown, with its lines when it has any.
  async function save() {
    if (!src) return;
    const drawn = $state.snapshot(lines);
    const url = drawn.length ? (await bakeMarks(src, drawn)).url : src;
    const link = document.createElement("a");
    link.href = url;
    link.download = saveName(name, dataMime(url));
    link.click();
  }

  // The pen's sheet lies exactly over the picture, in the picture's own pixels.
  $effect(() => {
    if (!picture || !canvas) return;
    const element = picture;
    const measure = () => (place = { left: element.offsetLeft, top: element.offsetTop, width: element.offsetWidth, height: element.offsetHeight });
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    observer.observe(canvas);
    return () => observer.disconnect();
  });

  $effect(() => {
    if (!sheet || !natural.width) return;
    const context = sheet.getContext("2d");
    context.clearRect(0, 0, sheet.width, sheet.height);
    drawLines(context, lines, penWidth(natural.width, natural.height));
  });

  function pointAt(event) {
    const box = sheet.getBoundingClientRect();
    return {
      x: ((event.clientX - box.left) / box.width) * natural.width,
      y: ((event.clientY - box.top) / box.height) * natural.height,
    };
  }

  function drawStart(event) {
    if (!pen || event.button !== 0 || !natural.width) return;
    event.preventDefault();
    event.stopPropagation();
    sheet.setPointerCapture(event.pointerId);
    lines.push([pointAt(event)]);
  }

  function drawMove(event) {
    if (!pen || !sheet?.hasPointerCapture(event.pointerId)) return;
    lines[lines.length - 1].push(pointAt(event));
  }

  function undo() {
    lines.pop();
  }

  // Actual size starts centered on the picture.
  function setFit(fit) {
    viewing.fit = fit;
    zoom = 1;
    if (fit) return;
    requestAnimationFrame(() => {
      if (!canvas) return;
      canvas.scrollLeft = (canvas.scrollWidth - canvas.clientWidth) / 2;
      canvas.scrollTop = (canvas.scrollHeight - canvas.clientHeight) / 2;
    });
  }

  function pan(dx, dy) {
    if (!movable || !canvas) return;
    const step = PAN_STEP * scale();
    canvas.scrollLeft += dx * step;
    canvas.scrollTop += dy * step;
  }

  // Dragging pans the picture at actual size.
  let drag = null;
  function down(event) {
    if (pen || !movable || event.button !== 0 || !canvas) return;
    event.preventDefault();
    drag = { x: event.clientX, y: event.clientY, left: canvas.scrollLeft, top: canvas.scrollTop };
    const move = (e) => {
      if (!drag || !canvas) return;
      canvas.scrollLeft = drag.left - (e.clientX - drag.x);
      canvas.scrollTop = drag.top - (e.clientY - drag.y);
    };
    const up = () => {
      drag = null;
      window.removeEventListener("mousemove", move);
      window.removeEventListener("mouseup", up);
    };
    window.addEventListener("mousemove", move);
    window.addEventListener("mouseup", up);
  }

  // Ctrl + wheel: zoom toward the mouse, keeping the spot under it in place.
  async function wheel(event) {
    if (!event.ctrlKey || !picture || !canvas || !natural.width) return;
    event.preventDefault();
    const next = Math.max(1, Math.min(MAX_ZOOM, zoom * Math.exp(-event.deltaY * 0.002)));
    if (next === zoom) return;
    if (zoom === 1) base = { width: picture.offsetWidth, height: picture.offsetHeight };
    const box = canvas.getBoundingClientRect();
    const x = event.clientX - box.left;
    const y = event.clientY - box.top;
    const fx = (canvas.scrollLeft + x - picture.offsetLeft) / picture.offsetWidth;
    const fy = (canvas.scrollTop + y - picture.offsetTop) / picture.offsetHeight;
    zoom = next;
    await tick();
    if (!picture || !canvas) return;
    canvas.scrollLeft = picture.offsetLeft + fx * picture.offsetWidth - x;
    canvas.scrollTop = picture.offsetTop + fy * picture.offsetHeight - y;
  }

  $effect(() => {
    if (!canvas) return;
    const element = canvas;
    element.addEventListener("wheel", wheel, { passive: false });
    return () => element.removeEventListener("wheel", wheel);
  });

  // Tab and Shift+Tab walk the bar's buttons and stay inside.
  function cycle(back) {
    const buttons = [...(root?.querySelectorAll("button") ?? [])];
    if (buttons.length === 0) return;
    const at = buttons.indexOf(document.activeElement);
    const next = at < 0 ? (back ? buttons.length - 1 : 0) : (at + (back ? -1 : 1) + buttons.length) % buttons.length;
    buttons[next].focus();
  }

  const KEYS = {
    Escape: () => close(),
    f: () => setFit(!viewing.fit),
    F: () => setFit(!viewing.fit),
    ArrowLeft: () => pan(-1, 0),
    ArrowRight: () => pan(1, 0),
    ArrowUp: () => pan(0, -1),
    ArrowDown: () => pan(0, 1),
    Tab: () => cycle(false),
    "Shift+Tab": () => cycle(true),
    "Ctrl+Z": () => undo(),
  };

  $effect(() => {
    if (!open) return;
    const unbind = Object.entries(KEYS).map(([combo, run]) =>
      bindKey("overlay", combo, () => {
        if (!pic) return false;
        run();
        return true;
      }),
    );
    return () => unbind.forEach((off) => off());
  });

  // Focus moves in on open, is kept inside, and goes back on close.
  $effect(() => {
    if (!open || !root) return;
    const element = root;
    opener = document.activeElement instanceof HTMLElement && !element.contains(document.activeElement) ? document.activeElement : null;
    element.focus({ preventScroll: true });
    const keep = (event) => {
      if (!element.contains(event.target)) element.focus({ preventScroll: true });
    };
    document.addEventListener("focusin", keep);
    return () => {
      document.removeEventListener("focusin", keep);
      const back = opener;
      opener = null;
      if (back?.isConnected) back.focus({ preventScroll: true });
    };
  });
</script>

{#if open}
  <div class="viewer" role="dialog" aria-modal="true" aria-label="Picture {name}" tabindex="-1" data-image-viewer bind:this={root} transition:unfold={{ from: "top" }}>
    <div class="bar">
      <p class="name" title={name}>{name}</p>
      {#if size}<p class="size">{size}</p>{/if}
      <div class="buttons">
        {#if markable}
          <button type="button" class="icon pen" class:on={pen} aria-pressed={pen} title="Red pen" aria-label="Red pen" data-viewer-pen onclick={() => (pen = !pen)}><Icon name="pen" size={16} /></button>
          {#if pen}<button type="button" disabled={lines.length === 0} data-viewer-undo onclick={undo}>Undo</button>{/if}
        {/if}
        <button type="button" class="icon" title="Save picture" aria-label="Save picture" disabled={!src} data-viewer-save onclick={save}><Icon name="download" size={16} /></button>
        <button type="button" class:active={viewing.fit} aria-pressed={viewing.fit} data-viewer-fit onclick={() => setFit(true)}>Fit</button>
        <button type="button" class:active={!viewing.fit} aria-pressed={!viewing.fit} data-viewer-actual onclick={() => setFit(false)}>Actual size</button>
        <button type="button" data-viewer-close onclick={close}>Close <span class="key">Esc</span></button>
      </div>
    </div>
    <Rule marks="both" class="viewer-rule" />
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="canvas" class:actual={movable} bind:this={canvas} onmousedown={down}>
      {#if viewing.state === "failed"}
        <p class="failed">{PICTURE_FAILED}</p>
      {:else if src}
        <img
          {src}
          alt={name}
          draggable="false"
          style:width={zoomed ? `${base.width * zoom}px` : !viewing.fit && natural.width ? `${natural.width / ratio()}px` : null}
          style:height={zoomed ? `${base.height * zoom}px` : !viewing.fit && natural.height ? `${natural.height / ratio()}px` : null}
          bind:this={picture}
          onload={loaded}
          onerror={failed}
        />
        {#if markable && natural.width}
          <canvas
            class="sheet"
            class:drawing={pen}
            width={natural.width}
            height={natural.height}
            style:left="{place.left}px"
            style:top="{place.top}px"
            style:width="{place.width}px"
            style:height="{place.height}px"
            bind:this={sheet}
            data-viewer-sheet
            onpointerdown={drawStart}
            onpointermove={drawMove}
          ></canvas>
        {/if}
      {/if}
    </div>
  </div>
{/if}

<style>
  .viewer {
    position: fixed;
    inset: var(--titlebar) 0 0;
    z-index: 20;
    display: flex;
    flex-direction: column;
    background: var(--scrim);
    outline: none;
  }
  .bar {
    flex: none;
    display: flex;
    align-items: center;
    gap: calc(16 * var(--px));
    box-sizing: border-box;
    height: calc(56 * var(--px));
    padding: 0 calc(24 * var(--px));
  }
  .name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font: var(--t-item);
  }
  .size {
    flex: none;
    font: var(--t-small);
    color: var(--slate);
    font-variant-numeric: tabular-nums;
  }
  .buttons {
    flex: none;
    display: flex;
    gap: calc(20 * var(--px));
    margin-left: auto;
  }
  .buttons button {
    font: var(--t-body-small);
    color: var(--ink);
  }
  .buttons button:hover,
  .buttons .active {
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }
  .buttons .icon {
    display: flex;
    align-items: center;
  }
  .buttons button:disabled {
    color: var(--slate);
    text-decoration: none;
  }
  .buttons .pen.on {
    color: #e5322d;
    box-shadow: inset 0 calc(-2 * var(--px)) 0 #e5322d;
  }
  .key {
    color: var(--slate);
  }
  .sheet {
    position: absolute;
    pointer-events: none;
  }
  .sheet.drawing {
    pointer-events: auto;
    cursor: crosshair;
    touch-action: none;
  }
  .viewer :global(.rule.viewer-rule) {
    flex: none;
  }
  /* The picture keeps 24 from every edge. Fit: as large as fits, never
     larger than it is. Actual size: 1:1, panned inside; auto margins center
     a small picture without cutting off a large one's start. */
  .canvas {
    position: relative;
    flex: 1;
    min-height: 0;
    display: flex;
    margin: calc(24 * var(--px));
    overflow: hidden;
  }
  .canvas img {
    display: block;
    flex: none;
    box-sizing: border-box;
    margin: auto;
    max-width: 100%;
    max-height: 100%;
    border: 1px solid var(--rule-faint);
    background: var(--paper-raised);
    user-select: none;
  }
  .canvas.actual {
    cursor: grab;
  }
  .canvas.actual img {
    max-width: none;
    max-height: none;
  }
  .failed {
    margin: auto;
    font: var(--t-small);
    color: var(--slate);
  }
</style>
