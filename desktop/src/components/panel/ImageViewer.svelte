<script>
  // The picture viewer (4.5, CHAT-007, TYPE-037): a picture chip, a picture in
  // a reply or a figure in VISUALS opens it over the whole window. The paper
  // scrim, a bar with the name, "W × H · N KB", Fit, Actual size and Close Esc,
  // a rule with + marks, then the picture centered 24 in from the edges, fit
  // by default; Actual size (or F) shows it 1:1, panned by dragging or by the
  // arrow keys in steps of 40. Transparent pictures sit on --paper-raised.
  // Esc closes; focus stays inside while it is open and goes back to what
  // opened it. Its keys take the overlay scope, which is the only one asked
  // while the viewer is open (lib/keys.js). Placed by OverlayHost.
  import { untrack } from "svelte";
  import { file_read } from "../../lib/engine/commands.js";
  import { bindKey } from "../../lib/keys.js";
  import { conn } from "../../lib/stores/connection.svelte.js";
  import { openViewer, ui } from "../../lib/stores/ui.svelte.js";
  import { PICTURE_FAILED, PICTURE_MAX_BYTES } from "../render/FigureView.svelte";
  import Rule from "../Rule.svelte";
  import { viewing } from "./panel.svelte.js";
  import { base64Bytes, dataBytes, pictureName, pictureSize, viewerSource } from "./viewer.js";

  const PAN_STEP = 40;

  let root = $state(null);
  let canvas = $state(null);
  let src = $state("");
  let bytes = $state(null);
  let natural = $state({ width: 0, height: 0 });
  let opener = null;

  const open = $derived(Boolean(ui.viewer));
  const name = $derived(pictureName(ui.viewer));
  const size = $derived(pictureSize(natural.width, natural.height, bytes));
  const ratio = () => globalThis.devicePixelRatio || 1;
  const scale = () => parseFloat(getComputedStyle(document.documentElement).fontSize) / 16;

  $effect(() => {
    viewing.name = open ? name : "";
    viewing.size = open ? size : "";
  });

  // A new picture: read it, from the project folder or as it is.
  $effect(() => {
    const source = viewerSource(ui.viewer);
    src = "";
    bytes = null;
    natural = { width: 0, height: 0 };
    viewing.fit = true;
    if (!ui.viewer) {
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
      return;
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

  // Actual size starts centered on the picture.
  function setFit(fit) {
    viewing.fit = fit;
    if (fit) return;
    requestAnimationFrame(() => {
      if (!canvas) return;
      canvas.scrollLeft = (canvas.scrollWidth - canvas.clientWidth) / 2;
      canvas.scrollTop = (canvas.scrollHeight - canvas.clientHeight) / 2;
    });
  }

  function pan(dx, dy) {
    if (viewing.fit || !canvas) return;
    const step = PAN_STEP * scale();
    canvas.scrollLeft += dx * step;
    canvas.scrollTop += dy * step;
  }

  // Dragging pans the picture at actual size.
  let drag = null;
  function down(event) {
    if (viewing.fit || event.button !== 0 || !canvas) return;
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
  };

  $effect(() => {
    if (!open) return;
    const unbind = Object.entries(KEYS).map(([combo, run]) =>
      bindKey("overlay", combo, () => {
        if (!ui.viewer) return false;
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
  <div class="viewer" role="dialog" aria-modal="true" aria-label="Picture {name}" tabindex="-1" data-image-viewer bind:this={root}>
    <div class="bar">
      <p class="name" title={name}>{name}</p>
      {#if size}<p class="size">{size}</p>{/if}
      <div class="buttons">
        <button type="button" class:active={viewing.fit} aria-pressed={viewing.fit} data-viewer-fit onclick={() => setFit(true)}>Fit</button>
        <button type="button" class:active={!viewing.fit} aria-pressed={!viewing.fit} data-viewer-actual onclick={() => setFit(false)}>Actual size</button>
        <button type="button" data-viewer-close onclick={close}>Close <span class="key">Esc</span></button>
      </div>
    </div>
    <Rule marks="both" class="viewer-rule" />
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="canvas" class:actual={!viewing.fit} bind:this={canvas} onmousedown={down}>
      {#if viewing.state === "failed"}
        <p class="failed">{PICTURE_FAILED}</p>
      {:else if src}
        <img
          {src}
          alt={name}
          draggable="false"
          style:width={!viewing.fit && natural.width ? `${natural.width / ratio()}px` : null}
          style:height={!viewing.fit && natural.height ? `${natural.height / ratio()}px` : null}
          onload={loaded}
          onerror={failed}
        />
      {/if}
    </div>
  </div>
{/if}

<style>
  .viewer {
    position: fixed;
    inset: 0;
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
  .key {
    color: var(--slate);
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
