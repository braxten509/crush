<script>
  // A choice's video beside the answers, in the picture's spot (owner,
  // 2026-11-27: layout A, sound on). It streams from app://localhost/
  // question-video, so a long video is never held in memory whole. It plays
  // only when asked, and stops when the choice changes or the page hides.
  //   path (absolute .mp4, .webm or .mov, on this computer)
  //   shown (false while the page is not the active one)
  import { ui } from "../../lib/stores/ui.svelte.js";

  let { path = "", shown = true } = $props();

  let videoEl = $state(null);
  let failed = $state(false);

  const src = $derived(path ? `app://localhost/question-video?path=${encodeURIComponent(path)}` : "");

  $effect(() => {
    path;
    failed = false;
  });

  $effect(() => {
    if (!shown && videoEl && !videoEl.paused) videoEl.pause();
  });

  // The larger player carries on from here; this one waits.
  function openLarger() {
    const at = videoEl?.currentTime ?? 0;
    videoEl?.pause();
    ui.viewer = { choiceVideo: { path, name: path.split("/").pop(), at } };
  }
</script>

<div class="video" data-probe="question-video">
  {#if !path}
    <p>No video for this choice.</p>
  {:else if failed}
    <p>This video can't be played.</p>
  {:else}
    {#key src}
      <!-- svelte-ignore a11y_media_has_caption -->
      <video bind:this={videoEl} {src} controls preload="metadata" playsinline tabindex="-1" onerror={() => (failed = true)}></video>
    {/key}
    <button type="button" class="larger" tabindex="-1" data-probe="question-video-larger" onclick={openLarger}>Open larger player</button>
  {/if}
</div>

<style>
  .video {
    display: grid;
    grid-template-rows: minmax(0, 1fr) auto;
    place-items: center;
    gap: calc(8 * var(--px));
    flex: none;
    height: calc(416 * var(--px));
    max-width: 100%;
    overflow: hidden;
  }
  p {
    margin: 0;
    font: var(--t-small);
    color: var(--slate);
  }
  .larger {
    justify-self: end;
    height: calc(30 * var(--px));
    padding: 0 calc(10 * var(--px));
    border: 1px solid var(--rule-control);
    border-radius: 0;
    background: var(--control-rest);
    font: var(--t-button);
    color: var(--ink);
    cursor: pointer;
  }
  .larger:hover {
    background: var(--control-hover);
  }
  /* Its own shape, as big as the slot allows. */
  video {
    display: block;
    width: auto;
    height: auto;
    max-width: 100%;
    max-height: 100%;
  }
</style>
