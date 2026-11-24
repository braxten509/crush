<script>
  // Inline audio controls for a question choice row (layout B): duration and a
  // Play/Pause button on the row, plus a clickable time bar underneath while
  // this choice is the active sound. Audio stays manual — clicking the choice
  // selects it without playing, and pressing Play stops any other row.
  import { file_read } from "../../lib/engine/commands.js";
  import { projectDir } from "../../lib/stores/composer.svelte.js";

  const MAX_BYTES = 25 * 1024 * 1024;
  const cache = new Map(); // path → { url, mime } | null

  let { path = "", active = false, onActivate = () => {} } = $props();

  let src = $state(null);
  let loading = $state(false);
  let failed = $state(false);
  let playing = $state(false);
  let current = $state(0);
  let duration = $state(0);
  let audioEl = $state(null);
  let wantPlay = false;

  function formatTime(seconds) {
    if (!Number.isFinite(seconds) || seconds < 0) return "0:00";
    const total = Math.floor(seconds);
    const mins = Math.floor(total / 60);
    const secs = total % 60;
    return `${mins}:${secs < 10 ? "0" : ""}${secs}`;
  }

  function toBlobUrl(base64, mime) {
    const raw = atob(base64);
    const bytes = new Uint8Array(raw.length);
    for (let i = 0; i < raw.length; i++) bytes[i] = raw.charCodeAt(i);
    return URL.createObjectURL(new Blob([bytes], { type: mime }));
  }

  async function ensureLoaded() {
    const wanted = path;
    if (!wanted) return false;
    if (cache.has(wanted)) {
      const hit = cache.get(wanted);
      src = hit?.url ?? null;
      failed = !hit;
      return Boolean(hit);
    }
    loading = true;
    failed = false;
    try {
      const file = await file_read(wanted, projectDir(), MAX_BYTES);
      const mime = file?.mime ?? "";
      if (file?.base64 && /^audio\//i.test(mime)) {
        const entry = { url: toBlobUrl(file.base64, mime), mime };
        cache.set(wanted, entry);
        if (path === wanted) {
          src = entry.url;
          failed = false;
        }
        return true;
      }
      cache.set(wanted, null);
      if (path === wanted) failed = true;
      return false;
    } catch {
      cache.set(wanted, null);
      if (path === wanted) failed = true;
      return false;
    } finally {
      if (path === wanted) loading = false;
    }
  }

  // Preload metadata quietly so each choice row shows its duration right away.
  $effect(() => {
    const wanted = path;
    src = null;
    failed = false;
    playing = false;
    current = 0;
    duration = 0;
    wantPlay = false;
    if (!wanted) return;
    ensureLoaded();
  });

  // Stop immediately when another row becomes active or the tab/form closes.
  $effect(() => {
    if (!active && audioEl) {
      wantPlay = false;
      if (!audioEl.paused) audioEl.pause();
      if (audioEl.currentTime !== 0) audioEl.currentTime = 0;
      playing = false;
      current = 0;
    }
  });

  // Always stop audio if the form unmounts mid-playback.
  $effect(() => {
    const el = audioEl;
    return () => {
      if (el && !el.paused) el.pause();
    };
  });

  $effect(() => {
    if (src && audioEl && wantPlay) {
      wantPlay = false;
      audioEl.play().catch(() => {
        playing = false;
        failed = true;
      });
    }
  });

  async function togglePlay(event) {
    event.stopPropagation();
    if (loading) return;
    if (playing && audioEl) {
      audioEl.pause();
      return;
    }
    onActivate();
    if (!src) {
      wantPlay = true;
      const ok = await ensureLoaded();
      if (!ok) wantPlay = false;
      return;
    }
    if (audioEl) {
      if (audioEl.ended || (duration > 0 && audioEl.currentTime >= duration)) audioEl.currentTime = 0;
      audioEl.play().catch(() => {
        playing = false;
        failed = true;
      });
    }
  }

  function seek(event) {
    event.stopPropagation();
    if (!audioEl || !duration) return;
    const rect = event.currentTarget.getBoundingClientRect();
    if (rect.width <= 0) return;
    const ratio = Math.max(0, Math.min(1, (event.clientX - rect.left) / rect.width));
    audioEl.currentTime = ratio * duration;
    current = audioEl.currentTime;
  }

  const ratio = $derived(duration > 0 ? Math.max(0, Math.min(100, (current / duration) * 100)) : 0);
  const timeText = $derived(
    failed ? "Can't play" : active && duration > 0 ? `${formatTime(current)} / ${formatTime(duration)}` : duration > 0 ? formatTime(duration) : loading ? "…" : "",
  );
</script>

<audio
  bind:this={audioEl}
  {src}
  preload="metadata"
  onloadedmetadata={() => {
    if (audioEl && Number.isFinite(audioEl.duration)) duration = audioEl.duration;
  }}
  ontimeupdate={() => {
    if (audioEl) current = audioEl.currentTime;
  }}
  onplay={() => (playing = true)}
  onpause={() => (playing = false)}
  onended={() => {
    playing = false;
    current = 0;
  }}
  onerror={() => {
    playing = false;
    failed = true;
  }}
></audio>

<div class="audio-controls" data-probe="question-audio-controls">
  {#if timeText}
    <span class="time" class:err={failed}>{timeText}</span>
  {/if}
  <button
    type="button"
    class="play-btn"
    class:playing
    tabindex="-1"
    disabled={loading || failed}
    aria-label={playing ? "Pause sound" : "Play sound"}
    onclick={togglePlay}
    onmousedown={(e) => e.stopPropagation()}
  >
    <span class="glyph" aria-hidden="true">{playing ? "❚❚" : "▶"}</span>
    <span>{playing ? "Pause" : "Play"}</span>
  </button>
</div>

{#if active && !failed}
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div class="progress-wrap" data-probe="question-audio-progress" onclick={seek} onmousedown={(e) => e.stopPropagation()}>
    <div class="track">
      <div class="fill" style:width={`${ratio}%`}></div>
    </div>
  </div>
{/if}

<style>
  audio {
    display: none;
  }
  .audio-controls {
    display: flex;
    align-items: center;
    gap: calc(10 * var(--px));
    margin-left: auto;
    flex: none;
  }
  .time {
    font: var(--t-mono);
    color: var(--slate);
    white-space: nowrap;
  }
  .time.err {
    color: var(--error-ink);
  }
  .play-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: calc(6 * var(--px));
    min-width: calc(84 * var(--px));
    height: calc(30 * var(--px));
    padding: 0 calc(10 * var(--px));
    border: 1px solid var(--rule-control);
    border-radius: 0;
    background: var(--control-rest);
    font: var(--t-button);
    color: var(--ink);
    cursor: pointer;
    white-space: nowrap;
  }
  .play-btn:hover:not(:disabled) {
    background: var(--control-hover);
  }
  .play-btn.playing {
    border-color: var(--ink);
    background: var(--paper);
  }
  .play-btn:disabled {
    background: var(--control-disabled);
    color: var(--slate);
    cursor: default;
  }
  .glyph {
    font-size: 0.78em;
    line-height: 1;
  }
  .progress-wrap {
    flex-basis: 100%;
    padding: calc(6 * var(--px)) 0 calc(2 * var(--px)) calc(28 * var(--px));
    cursor: pointer;
  }
  .track {
    height: calc(6 * var(--px));
    background: var(--meter-track);
    border: 1px solid var(--meter-edge);
    box-sizing: border-box;
    overflow: hidden;
  }
  .fill {
    height: 100%;
    background: var(--choice);
  }
</style>
