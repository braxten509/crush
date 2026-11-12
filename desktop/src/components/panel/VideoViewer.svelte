<script>
  import { tick, untrack } from "svelte";
  import { unfold } from "../../lib/motion.js";
  import { MAX_ATTACHMENT_BYTES } from "../../lib/composer/paste.js";
  import { clockTime } from "../../lib/composer/recording.js";
  import { setVideoMarks } from "../../lib/stores/composer.svelte.js";
  import { bindKey } from "../../lib/keys.js";
  import { openViewer, ui } from "../../lib/stores/ui.svelte.js";
  import Icon from "../Icon.svelte";
  import Rule from "../Rule.svelte";
  import { bakeMarks, drawLines, NOTE_FONT, noteSize, penWidth } from "./marks.js";

  const FALLBACK_FPS = 30;
  const BIG_STEP = 10;
  // Ctrl + mouse wheel zooms in up to 10 times the starting size, never
  // smaller than it; dragging moves around (owner, 2026-11-08).
  const MAX_ZOOM = 10;

  let root = $state(null);
  let player = $state(null);
  let track = $state(null);
  let at = $state(0);
  let playing = $state(false);
  let failed = $state(false);
  let loadedDuration = $state(0);
  let marks = $state([]);
  let capturing = $state(false);
  let screen = $state(null);
  let zoom = $state(1);
  let base = $state({ width: 0, height: 0 });
  let dragged = false;
  let opener = null;
  // The red pen and Text draw on the frame shown (owner, 2026-11-08). Each
  // frame keeps its own lines and notes; drawing on a frame marks it, and
  // its marked picture is the frame with them (mark.plain is the frame
  // without them).
  let sheet = $state(null);
  let noteBox = $state(null);
  let tool = $state(null); // "pen", "text" or none
  let drawn = $state({}); // frame → lines and notes
  let draft = $state(null); // the note being typed: { frame, x, y, text }
  let spot = $state({ left: 0, top: 0, width: 0, height: 0 });
  let natural = $state({ width: 0, height: 0 });
  // Per opened video, so one video's late work never touches another's.
  let saves = {}; // frame → the latest save's number
  let captures = new Map(); // frame → the picture being taken

  const attachment = $derived(ui.viewer?.video ?? null);
  const open = $derived(Boolean(attachment));
  const info = $derived(attachment?.video ?? { width: 0, height: 0, duration: 0, bytes: 0, frames: [] });
  const duration = $derived(info.duration || loadedDuration);
  const frames = $derived.by(() => {
    if (info.frames?.length) return info.frames;
    if (!duration) return [0];
    const count = Math.max(1, Math.floor(duration * FALLBACK_FPS));
    return Array.from({ length: count }, (_, i) => i / FALLBACK_FPS);
  });
  const time = $derived(frames[at] ?? 0);
  const zoomed = $derived(zoom > 1 && base.width > 0);
  const markedHere = $derived(marks.some((m) => m.frame === at));
  const here = $derived(drawn[at] ?? []);
  const shown = $derived(natural.width ? spot.width / natural.width : 1);
  const sizeText = $derived(
    [info.width && info.height ? `${info.width} × ${info.height}` : "", duration ? clockTime(duration) : "", info.bytes ? `${(info.bytes / 1048576).toFixed(1)} MB` : ""].filter(Boolean).join(" · "),
  );

  $effect(() => {
    const video = attachment;
    at = 0;
    playing = false;
    failed = false;
    loadedDuration = 0;
    capturing = false;
    zoom = 1;
    tool = null;
    draft = null;
    natural = { width: 0, height: 0 };
    marks = video ? untrack(() => [...(video.marks ?? [])]) : [];
    drawn = Object.fromEntries(untrack(() => marks.filter((m) => m.notes?.length).map((m) => [m.frame, [...m.notes]])));
    saves = {};
    captures = new Map();
  });

  function close() {
    openViewer(null);
  }

  function seekTime(index) {
    const here = frames[index] ?? 0;
    const next = frames[index + 1];
    return next === undefined ? here + 0.001 : (here + next) / 2;
  }

  function show(index) {
    if (!player) return;
    const target = Math.max(0, Math.min(frames.length - 1, index));
    keepNote();
    // A frame being marked is taken before the viewer moves on.
    if (captures.size) {
      const pending = captures;
      Promise.all(pending.values()).then(() => {
        if (captures === pending) show(target);
      });
      return;
    }
    if (playing) player.pause();
    at = target;
    player.currentTime = seekTime(target);
  }

  function step(by) {
    show(at + by);
  }

  function frameAt(seconds) {
    let low = 0;
    let high = frames.length - 1;
    while (low < high) {
      const mid = Math.ceil((low + high) / 2);
      if (frames[mid] <= seconds + 1e-6) low = mid;
      else high = mid - 1;
    }
    return low;
  }

  function togglePlay() {
    if (!player) return;
    if (player.paused) {
      if (captures.size) return;
      if (at >= frames.length - 1) player.currentTime = 0;
      player.play().catch(() => {});
    } else {
      player.pause();
      show(frameAt(player.currentTime));
    }
  }

  $effect(() => {
    if (!player || !playing) return;
    const video = player;
    let handle = 0;
    const follow = () => {
      at = frameAt(video.currentTime);
      handle = video.requestVideoFrameCallback(follow);
    };
    handle = video.requestVideoFrameCallback(follow);
    return () => video.cancelVideoFrameCallback(handle);
  });

  // Waits until every pending seek is done (a step during the wait starts
  // another one).
  async function whenShown(video) {
    while (video.seeking) await new Promise((done) => video.addEventListener("seeked", () => done(), { once: true }));
  }

  function grab(video) {
    const canvas = document.createElement("canvas");
    canvas.width = video.videoWidth;
    canvas.height = video.videoHeight;
    canvas.getContext("2d").drawImage(video, 0, 0);
    let url = canvas.toDataURL("image/png");
    let mime = "image/png";
    if ((url.length * 3) / 4 > MAX_ATTACHMENT_BYTES) {
      url = canvas.toDataURL("image/jpeg", 0.9);
      mime = "image/jpeg";
    }
    return { mime, base64: url.slice(url.indexOf(",") + 1) };
  }

  // The picture is kept only if, once the frame is shown, the viewer still
  // shows that frame of that video; a step or a close in the meantime drops
  // it. Gives the mark, or null.
  function capture(frame) {
    if (captures.has(frame)) return captures.get(frame);
    const video = attachment;
    const element = player;
    const mine = captures;
    let job = null;
    job = (async () => {
      capturing = true;
      try {
        await whenShown(element);
        const target = seekTime(frame);
        if (attachment !== video || player !== element || at !== frame || Math.abs(element.currentTime - target) > 1e-3) return null;
        const existing = marks.find((m) => m.frame === frame);
        if (existing) return existing;
        const mark = { frame, time: frames[frame] ?? 0, ...grab(element) };
        marks = [...marks, mark].sort((a, b) => a.frame - b.frame);
        setVideoMarks(video, $state.snapshot(marks));
        return mark;
      } finally {
        if (mine.get(frame) === job) mine.delete(frame);
        if (captures === mine) capturing = mine.size > 0;
      }
    })();
    mine.set(frame, job);
    return job;
  }

  async function toggleMark() {
    if (!player || !attachment || failed || capturing) return;
    if (playing) togglePlay();
    if (marks.some((m) => m.frame === at)) {
      unmark(at);
      return;
    }
    await capture(at);
  }

  // Unmarking drops the frame's lines and notes too.
  function unmark(frame) {
    saves[frame] = (saves[frame] ?? 0) + 1; // a save still running is dropped
    marks = marks.filter((m) => m.frame !== frame);
    delete drawn[frame];
    if (draft?.frame === frame) draft = null;
    setVideoMarks(attachment, $state.snapshot(marks));
  }

  // The frame's marked picture again, with its lines and notes; the frame is
  // marked first if it is not yet. Only the latest save of a frame is kept.
  // A save that ends after the viewer closed still goes into its video.
  async function saveFrame(frame) {
    const video = attachment;
    const counter = saves;
    const number = (counter[frame] = (counter[frame] ?? 0) + 1);
    const notes = $state.snapshot(drawn[frame] ?? []);
    const mark = marks.find((m) => m.frame === frame) ?? (await capture(frame));
    if (!mark || counter[frame] !== number) return;
    const plain = mark.plain ?? { mime: mark.mime, base64: mark.base64 };
    const picture = notes.length ? await bakeMarks(`data:${plain.mime};base64,${plain.base64}`, notes, MAX_ATTACHMENT_BYTES) : plain;
    if (counter[frame] !== number) return;
    const here = attachment === video;
    const list = here ? marks : $state.snapshot(video.marks ?? []);
    if (!list.some((m) => m.frame === frame)) return;
    const next = list.map((m) => (m.frame === frame ? { ...m, plain, notes, mime: picture.mime, base64: picture.base64 } : m));
    if (here) marks = next;
    setVideoMarks(video, here ? $state.snapshot(next) : next);
  }

  function pick(next) {
    keepNote();
    if (playing) togglePlay();
    tool = tool === next ? null : next;
  }

  function sheetPoint(event) {
    const box = sheet.getBoundingClientRect();
    return {
      x: ((event.clientX - box.left) / box.width) * natural.width,
      y: ((event.clientY - box.top) / box.height) * natural.height,
    };
  }

  function drawStart(event) {
    if (!tool || event.button !== 0 || !natural.width || failed) return;
    event.preventDefault();
    event.stopPropagation();
    if (draft) {
      keepNote();
      return;
    }
    if (playing) togglePlay();
    const frame = at;
    if (tool === "text") {
      draft = { frame, ...sheetPoint(event), text: "" };
      tick().then(() => noteBox?.focus({ preventScroll: true }));
      return;
    }
    sheet.setPointerCapture(event.pointerId);
    drawn[frame] = [...(drawn[frame] ?? []), [sheetPoint(event)]];
  }

  function drawMove(event) {
    if (tool !== "pen" || !sheet?.hasPointerCapture(event.pointerId)) return;
    drawn[at]?.at(-1)?.push(sheetPoint(event));
  }

  function drawEnd(event) {
    if (tool !== "pen" || !sheet?.hasPointerCapture(event.pointerId)) return;
    sheet.releasePointerCapture(event.pointerId);
    saveFrame(at);
  }

  function keepNote() {
    if (!draft) return;
    const { frame, x, y } = draft;
    const text = draft.text.trim();
    draft = null;
    if (!text) return;
    drawn[frame] = [...(drawn[frame] ?? []), { text, x, y }];
    saveFrame(frame);
  }

  function noteKey(event) {
    if (event.key === "Enter") keepNote();
    else if (event.key === "Escape") draft = null;
    else return;
    event.preventDefault();
    root?.focus({ preventScroll: true });
  }

  function undo() {
    if (!drawn[at]?.length) return;
    drawn[at] = drawn[at].slice(0, -1);
    saveFrame(at);
  }

  // The sheet lies exactly over the video, in the video's own pixels.
  $effect(() => {
    if (!player || !screen) return;
    const element = player;
    const measure = () => (spot = { left: element.offsetLeft, top: element.offsetTop, width: element.offsetWidth, height: element.offsetHeight });
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    observer.observe(screen);
    return () => observer.disconnect();
  });

  $effect(() => {
    if (!sheet || !natural.width) return;
    const context = sheet.getContext("2d");
    context.clearRect(0, 0, sheet.width, sheet.height);
    drawLines(context, here, penWidth(natural.width, natural.height));
  });

  function trackIndex(event) {
    const box = track.getBoundingClientRect();
    const share = Math.max(0, Math.min(1, (event.clientX - box.left) / box.width));
    return frameAt(share * (duration || frames[frames.length - 1] || 0));
  }

  function trackDown(event) {
    if (event.button !== 0 || !track) return;
    event.preventDefault();
    track.setPointerCapture(event.pointerId);
    show(trackIndex(event));
  }

  function trackMove(event) {
    if (!track?.hasPointerCapture(event.pointerId)) return;
    show(trackIndex(event));
  }

  const place = (seconds) => `${duration ? Math.min(100, (seconds / duration) * 100) : 0}%`;

  function loaded() {
    loadedDuration = Number.isFinite(player?.duration) ? player.duration : 0;
    natural = { width: player?.videoWidth || info.width, height: player?.videoHeight || info.height };
    show(at);
  }

  // Ctrl + wheel: zoom toward the mouse, keeping the spot under it in place.
  async function wheel(event) {
    if (!event.ctrlKey || !player || !screen) return;
    event.preventDefault();
    const next = Math.max(1, Math.min(MAX_ZOOM, zoom * Math.exp(-event.deltaY * 0.002)));
    if (next === zoom) return;
    if (zoom === 1) base = { width: player.offsetWidth, height: player.offsetHeight };
    const box = screen.getBoundingClientRect();
    const x = event.clientX - box.left;
    const y = event.clientY - box.top;
    const fx = (screen.scrollLeft + x - player.offsetLeft) / player.offsetWidth;
    const fy = (screen.scrollTop + y - player.offsetTop) / player.offsetHeight;
    zoom = next;
    await tick();
    if (!player || !screen) return;
    screen.scrollLeft = player.offsetLeft + fx * player.offsetWidth - x;
    screen.scrollTop = player.offsetTop + fy * player.offsetHeight - y;
  }

  $effect(() => {
    if (!screen) return;
    const element = screen;
    element.addEventListener("wheel", wheel, { passive: false });
    return () => element.removeEventListener("wheel", wheel);
  });

  // Dragging moves a zoomed video; a drag is not a click (no play/pause).
  function screenDown(event) {
    dragged = false;
    if (!zoomed || event.button !== 0 || !screen) return;
    event.preventDefault();
    const element = screen;
    const start = { x: event.clientX, y: event.clientY, left: element.scrollLeft, top: element.scrollTop };
    const move = (e) => {
      if (Math.abs(e.clientX - start.x) + Math.abs(e.clientY - start.y) > 3) dragged = true;
      element.scrollLeft = start.left - (e.clientX - start.x);
      element.scrollTop = start.top - (e.clientY - start.y);
    };
    const up = () => {
      window.removeEventListener("mousemove", move);
      window.removeEventListener("mouseup", up);
    };
    window.addEventListener("mousemove", move);
    window.addEventListener("mouseup", up);
  }

  function videoClick() {
    if (dragged) {
      dragged = false;
      return;
    }
    togglePlay();
  }

  function cycle(back) {
    const buttons = [...(root?.querySelectorAll("button") ?? [])];
    if (buttons.length === 0) return;
    const index = buttons.indexOf(document.activeElement);
    const next = index < 0 ? (back ? buttons.length - 1 : 0) : (index + (back ? -1 : 1) + buttons.length) % buttons.length;
    buttons[next].focus();
  }

  const KEYS = {
    Escape: () => close(),
    ArrowLeft: () => step(-1),
    ArrowRight: () => step(1),
    "Shift+ArrowLeft": () => step(-BIG_STEP),
    "Shift+ArrowRight": () => step(BIG_STEP),
    Home: () => show(0),
    End: () => show(frames.length - 1),
    m: () => toggleMark(),
    M: () => toggleMark(),
    "Shift+M": () => toggleMark(),
    "Ctrl+Z": () => undo(),
    Space: () => togglePlay(),
    Tab: () => cycle(false),
    "Shift+Tab": () => cycle(true),
  };

  $effect(() => {
    if (!open) return;
    const unbind = Object.entries(KEYS).map(([combo, run]) =>
      bindKey("overlay", combo, () => {
        if (!ui.viewer?.video || draft) return false;
        run();
        return true;
      }),
    );
    return () => unbind.forEach((off) => off());
  });

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
  <div class="viewer" role="dialog" aria-modal="true" aria-label="Video {attachment.name}" tabindex="-1" data-video-viewer bind:this={root} transition:unfold={{ from: "top" }}>
    <div class="bar">
      <p class="name" title={attachment.name}>{attachment.name}</p>
      {#if sizeText}<p class="size">{sizeText}</p>{/if}
      <div class="buttons">
        <button type="button" data-video-close onclick={close}>Close <span class="key">Esc</span></button>
      </div>
    </div>
    <Rule marks="both" class="viewer-rule" />
    <div class="body">
      <div class="stage">
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="screen" class:zoomed bind:this={screen} onmousedown={screenDown}>
          {#if failed}
            <p class="failed">Couldn't play this recording.</p>
          {:else}
            <!-- svelte-ignore a11y_media_has_caption -->
            <video
              src={attachment.url}
              preload="auto"
              muted
              bind:this={player}
              onloadedmetadata={loaded}
              onplay={() => (playing = true)}
              onpause={() => (playing = false)}
              onended={() => ((playing = false), show(frames.length - 1))}
              onerror={() => (failed = true)}
              style:width={zoomed ? `${base.width * zoom}px` : null}
              style:height={zoomed ? `${base.height * zoom}px` : null}
              onclick={videoClick}
            ></video>
            {#if natural.width && !playing}
              <canvas
                class="sheet"
                class:drawing={tool}
                class:typing={tool === "text"}
                width={natural.width}
                height={natural.height}
                style:left="{spot.left}px"
                style:top="{spot.top}px"
                style:width="{spot.width}px"
                style:height="{spot.height}px"
                bind:this={sheet}
                data-video-sheet
                onpointerdown={drawStart}
                onpointermove={drawMove}
                onpointerup={drawEnd}
                onlostpointercapture={drawEnd}
              ></canvas>
              {#if draft && draft.frame === at}
                <input
                  class="note"
                  type="text"
                  aria-label="Text to add"
                  spellcheck="false"
                  bind:value={draft.text}
                  bind:this={noteBox}
                  style:left="{spot.left + draft.x * shown}px"
                  style:top="{spot.top + draft.y * shown}px"
                  style:font-size="{noteSize(natural.width, natural.height) * shown}px"
                  style:font-family={NOTE_FONT}
                  data-video-note
                  onkeydown={noteKey}
                  onblur={keepNote}
                />
              {/if}
            {/if}
          {/if}
        </div>
        <div class="controls">
          <button type="button" class="box" aria-label="Back {BIG_STEP} frames" title="Back {BIG_STEP} frames (Shift+←)" data-video-back disabled={at === 0} onclick={() => step(-BIG_STEP)}>◀◀</button>
          <button type="button" class="box" aria-label="Previous frame" title="Previous frame (←)" data-video-prev disabled={at === 0} onclick={() => step(-1)}>◀</button>
          <p class="where" data-video-frame={at}>Frame {at + 1} of {frames.length} · {clockTime(time)}</p>
          <button type="button" class="box" aria-label="Next frame" title="Next frame (→)" data-video-next disabled={at >= frames.length - 1} onclick={() => step(1)}>▶</button>
          <button type="button" class="box" aria-label="Forward {BIG_STEP} frames" title="Forward {BIG_STEP} frames (Shift+→)" data-video-forward disabled={at >= frames.length - 1} onclick={() => step(BIG_STEP)}>▶▶</button>
          <button type="button" class="box" title="Play or pause (Space)" data-video-play onclick={togglePlay}>{playing ? "Pause" : "Play"}</button>
          <button type="button" class="box mark" class:on={markedHere} title="Mark this frame (M)" data-video-mark disabled={failed || capturing} onclick={toggleMark}>{markedHere ? "Unmark frame" : "Mark frame"}</button>
        </div>
        <!-- The drawing tools get their own row under the frame buttons (owner, 2026-11-08). -->
        <div class="controls">
          <button type="button" class="box tool pen" class:on={tool === "pen"} aria-pressed={tool === "pen"} title="Red pen: draw on this frame" aria-label="Red pen" data-video-pen disabled={failed} onclick={() => pick("pen")}><Icon name="pen" size={16} /></button>
          <button type="button" class="box tool" class:on={tool === "text"} aria-pressed={tool === "text"} title="Add text: click a spot, type, Enter" data-video-text disabled={failed} onclick={() => pick("text")}>Text</button>
          <button type="button" class="box" title="Undo on this frame (Ctrl+Z)" data-video-undo disabled={here.length === 0} onclick={undo}>Undo</button>
        </div>
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="track" bind:this={track} onpointerdown={trackDown} onpointermove={trackMove} data-video-track>
          <span class="head" style:left={place(time)}></span>
          {#each marks as mark (mark.frame)}
            <span class="tick" style:left={place(mark.time)}></span>
          {/each}
        </div>
      </div>
      <aside class="marked" data-video-marks>
        <p class="heading">Marked frames</p>
        {#if marks.length === 0}
          <p class="empty">None yet. Press Mark frame or M.</p>
        {:else}
          <ul>
            {#each marks as mark (mark.frame)}
              <li class:here={mark.frame === at}>
                <button type="button" class="jump" data-frame={mark.frame} onclick={() => show(mark.frame)}>Frame {mark.frame + 1} · {clockTime(mark.time)}</button>
                <button type="button" class="unmark" aria-label="Unmark frame {mark.frame + 1}" onclick={() => unmark(mark.frame)}>×</button>
              </li>
            {/each}
          </ul>
        {/if}
      </aside>
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
  .buttons button:hover {
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
  .body {
    flex: 1;
    min-height: 0;
    display: flex;
    gap: calc(24 * var(--px));
    margin: calc(24 * var(--px));
  }
  .stage {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: calc(14 * var(--px));
  }
  .screen {
    position: relative;
    flex: 1;
    min-height: 0;
    width: 100%;
    display: flex;
    overflow: hidden;
  }
  /* Auto margins center the video without cutting off a zoomed one's start. */
  video {
    display: block;
    flex: none;
    box-sizing: border-box;
    margin: auto;
    max-width: 100%;
    max-height: 100%;
    border: 1px solid var(--rule-faint);
    background: var(--figure);
  }
  .screen.zoomed {
    cursor: grab;
  }
  .screen.zoomed video {
    max-width: none;
    max-height: none;
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
  .sheet.typing {
    cursor: text;
  }
  /* White with a black shadow, as the note is drawn (marks.js). */
  .note {
    position: absolute;
    min-width: 2ch;
    field-sizing: content;
    padding: 0;
    border: none;
    outline: 1px dashed rgba(255, 255, 255, 0.8);
    background: transparent;
    color: #ffffff;
    caret-color: #ffffff;
    font-weight: 600;
    line-height: 1.2;
    text-shadow: 0 0.05em 0.17em rgba(0, 0, 0, 0.95), 0 0.05em 0.17em rgba(0, 0, 0, 0.95);
    transform: translateY(-50%);
  }
  .box.tool {
    display: flex;
    align-items: center;
  }
  .box.tool.on {
    border-color: #e5322d;
    color: #e5322d;
    box-shadow: inset 0 calc(-2 * var(--px)) 0 #e5322d;
  }
  .failed {
    margin: auto;
    font: var(--t-body-small);
    color: var(--slate);
  }
  .controls {
    flex: none;
    display: flex;
    align-items: center;
    gap: calc(12 * var(--px));
  }
  .where {
    min-width: calc(250 * var(--px));
    text-align: center;
    font: var(--t-body-small);
    font-variant-numeric: tabular-nums;
  }
  .box {
    box-sizing: border-box;
    min-height: calc(36 * var(--px));
    padding: 0 calc(14 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--paper-raised);
    font: var(--t-body-small);
    color: var(--ink);
  }
  .box:hover:not(:disabled) {
    background: var(--control-hover);
  }
  .box:disabled {
    color: var(--slate);
    opacity: 0.6;
  }
  /* Sized by its words; the Send button's colours (owner, 2026-11-07). */
  .box.mark {
    border-color: var(--vermilion-fill);
    background: var(--vermilion-fill);
    color: var(--on-accent);
  }
  .box.mark:hover:not(:disabled) {
    background: var(--vermilion-fill);
    filter: brightness(1.08);
  }
  .box.mark.on,
  .box.mark.on:hover:not(:disabled) {
    background: var(--paper-raised);
    color: var(--vermilion-ink);
    filter: none;
  }
  .box.mark.on:hover:not(:disabled) {
    background: var(--control-hover);
  }
  .track {
    position: relative;
    flex: none;
    width: min(100%, calc(560 * var(--px)));
    height: calc(10 * var(--px));
    margin-bottom: calc(6 * var(--px));
    box-sizing: border-box;
    background: var(--meter-track);
    border: 1px solid var(--meter-edge);
    cursor: pointer;
    touch-action: none;
  }
  .tick,
  .head {
    position: absolute;
    top: calc(-5 * var(--px));
    width: calc(3 * var(--px));
    height: calc(18 * var(--px));
    transform: translateX(-50%);
    pointer-events: none;
  }
  .tick {
    background: var(--vermilion);
  }
  .head {
    width: calc(2 * var(--px));
    background: var(--ink);
  }
  .marked {
    flex: none;
    width: calc(250 * var(--px));
    padding-left: calc(20 * var(--px));
    border-left: 1px solid var(--rule);
    overflow-y: auto;
  }
  .heading {
    margin-bottom: calc(10 * var(--px));
    font: var(--t-author);
  }
  .empty {
    font: var(--t-body-small);
    color: var(--slate);
  }
  ul {
    margin: 0;
    padding: 0;
    list-style: none;
  }
  li {
    display: flex;
    align-items: center;
  }
  li.here {
    background: var(--band);
  }
  .jump {
    flex: 1;
    min-height: calc(36 * var(--px));
    padding: 0 calc(8 * var(--px));
    text-align: left;
    font: var(--t-body-small);
    color: var(--ink);
    font-variant-numeric: tabular-nums;
  }
  .jump:hover {
    text-decoration: underline;
    text-underline-offset: calc(4 * var(--px));
  }
  .unmark {
    flex: none;
    width: calc(36 * var(--px));
    min-height: calc(36 * var(--px));
    font: var(--t-body-small);
    color: var(--slate);
  }
  .unmark:hover {
    color: var(--vermilion-ink);
  }
</style>
