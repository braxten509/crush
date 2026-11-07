<script>
  import { untrack } from "svelte";
  import { unfold } from "../../lib/motion.js";
  import { MAX_ATTACHMENT_BYTES } from "../../lib/composer/paste.js";
  import { clockTime } from "../../lib/composer/recording.js";
  import { setVideoMarks } from "../../lib/stores/composer.svelte.js";
  import { bindKey } from "../../lib/keys.js";
  import { openViewer, ui } from "../../lib/stores/ui.svelte.js";
  import Rule from "../Rule.svelte";

  const FALLBACK_FPS = 30;
  const BIG_STEP = 10;

  let root = $state(null);
  let player = $state(null);
  let track = $state(null);
  let at = $state(0);
  let playing = $state(false);
  let failed = $state(false);
  let loadedDuration = $state(0);
  let marks = $state([]);
  let opener = null;

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
  const markedHere = $derived(marks.some((m) => m.frame === at));
  const sizeText = $derived(
    [info.width && info.height ? `${info.width} × ${info.height}` : "", duration ? clockTime(duration) : "", info.bytes ? `${(info.bytes / 1048576).toFixed(1)} MB` : ""].filter(Boolean).join(" · "),
  );

  $effect(() => {
    const video = attachment;
    at = 0;
    playing = false;
    failed = false;
    loadedDuration = 0;
    marks = video ? untrack(() => [...(video.marks ?? [])]) : [];
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

  function whenShown(video) {
    if (!video.seeking) return Promise.resolve();
    return new Promise((done) => video.addEventListener("seeked", () => done(), { once: true }));
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

  async function toggleMark() {
    if (!player || !attachment || failed) return;
    if (playing) togglePlay();
    const frame = at;
    if (marks.some((m) => m.frame === frame)) {
      marks = marks.filter((m) => m.frame !== frame);
    } else {
      await whenShown(player);
      const picture = grab(player);
      marks = [...marks, { frame, time: frames[frame] ?? 0, ...picture }].sort((a, b) => a.frame - b.frame);
    }
    setVideoMarks(attachment, $state.snapshot(marks));
  }

  function unmark(frame) {
    marks = marks.filter((m) => m.frame !== frame);
    setVideoMarks(attachment, $state.snapshot(marks));
  }

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
    show(at);
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
    Space: () => togglePlay(),
    Tab: () => cycle(false),
    "Shift+Tab": () => cycle(true),
  };

  $effect(() => {
    if (!open) return;
    const unbind = Object.entries(KEYS).map(([combo, run]) =>
      bindKey("overlay", combo, () => {
        if (!ui.viewer?.video) return false;
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
        <div class="screen">
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
              onclick={togglePlay}
            ></video>
          {/if}
        </div>
        <div class="controls">
          <button type="button" class="box" aria-label="Previous frame" title="Previous frame (←)" data-video-prev disabled={at === 0} onclick={() => step(-1)}>◀</button>
          <p class="where" data-video-frame>Frame {at + 1} of {frames.length} · {clockTime(time)}</p>
          <button type="button" class="box" aria-label="Next frame" title="Next frame (→)" data-video-next disabled={at >= frames.length - 1} onclick={() => step(1)}>▶</button>
          <button type="button" class="box" title="Play or pause (Space)" data-video-play onclick={togglePlay}>{playing ? "Pause" : "Play"}</button>
          <button type="button" class="box mark" class:on={markedHere} title="Mark this frame (M)" data-video-mark disabled={failed} onclick={toggleMark}>{markedHere ? "Unmark frame" : "Mark frame"}</button>
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
                <button type="button" class="jump" onclick={() => show(mark.frame)}>Frame {mark.frame + 1} · {clockTime(mark.time)}</button>
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
    flex: 1;
    min-height: 0;
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  video {
    display: block;
    max-width: 100%;
    max-height: 100%;
    border: 1px solid var(--rule-faint);
    background: var(--figure);
  }
  .failed {
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
  .box.mark {
    min-width: calc(150 * var(--px));
    border-color: var(--vermilion);
    background: var(--vermilion);
    color: var(--paper);
  }
  .box.mark:hover:not(:disabled) {
    background: var(--vermilion-fill);
  }
  .box.mark.on {
    background: var(--paper-raised);
    color: var(--vermilion-ink);
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
