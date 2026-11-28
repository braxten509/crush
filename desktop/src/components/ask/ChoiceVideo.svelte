<script>
  // A choice's video beside the answers, in the picture's spot (owner,
  // 2026-11-27: layout A, sound on). It streams from app://localhost/
  // question-video, so a long video is never held in memory whole. It plays
  // only when asked, and stops when the choice changes or the page hides.
  //   path (absolute .mp4, .webm or .mov, on this computer)
  //   shown (false while the page is not the active one)
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
  {/if}
</div>

<style>
  .video {
    display: grid;
    place-items: center;
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
  video {
    display: block;
    height: 100%;
    max-width: 100%;
    background: #000;
  }
</style>
