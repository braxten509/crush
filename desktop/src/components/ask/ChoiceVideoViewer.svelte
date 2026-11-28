<script>
  // The larger player for a question choice's video (owner, 2026-11-27: "a
  // button that opens a larger player window in crush"). It covers the window
  // under the title bar like the other viewers, carries on from where the
  // small player was, with sound, and closes with Esc or Close.
  //   ui.viewer.choiceVideo = { path, name, at } (openChoiceVideo)
  import { unfold } from "../../lib/motion.js";
  import { bindKey } from "../../lib/keys.js";
  import { ui } from "../../lib/stores/ui.svelte.js";
  import Rule from "../Rule.svelte";

  let root = $state(null);
  let player = $state(null);
  let failed = $state(false);
  let opener = null;

  const item = $derived(ui.viewer?.choiceVideo ?? null);
  const open = $derived(Boolean(item));
  const src = $derived(item ? `app://localhost/question-video?path=${encodeURIComponent(item.path)}` : "");

  function close() {
    if (ui.viewer?.choiceVideo) ui.viewer = null;
  }

  function loaded() {
    if (!player || !item) return;
    if (item.at > 0 && item.at < player.duration) player.currentTime = item.at;
    player.play().catch(() => {});
  }

  function togglePlay() {
    if (!player) return;
    if (player.paused) player.play().catch(() => {});
    else player.pause();
  }

  $effect(() => {
    src;
    failed = false;
  });

  $effect(() => {
    if (!open) return;
    const keys = { Escape: close, Space: togglePlay };
    const unbind = Object.entries(keys).map(([combo, run]) =>
      bindKey("overlay", combo, () => {
        if (!ui.viewer?.choiceVideo) return false;
        run();
        return true;
      }),
    );
    return () => unbind.forEach((off) => off());
  });

  // Focus stays inside while it is open and goes back to the form after.
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
  <div class="viewer" role="dialog" aria-modal="true" aria-label="Video {item.name}" tabindex="-1" data-choice-video-viewer bind:this={root} transition:unfold={{ from: "top" }}>
    <div class="bar">
      <p class="name" title={item.name}>{item.name}</p>
      <div class="buttons">
        <button type="button" data-choice-video-close onclick={close}>Close <span class="key">Esc</span></button>
      </div>
    </div>
    <Rule marks="both" class="viewer-rule" />
    <div class="screen">
      {#if failed}
        <p class="failed">This video can't be played.</p>
      {:else}
        {#key src}
          <!-- svelte-ignore a11y_media_has_caption -->
          <video bind:this={player} {src} controls preload="auto" tabindex="-1" onloadedmetadata={loaded} onerror={() => (failed = true)}></video>
        {/key}
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
  .buttons {
    flex: none;
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
  .screen {
    flex: 1;
    min-height: 0;
    display: flex;
    margin: calc(24 * var(--px));
  }
  /* Fills the space, keeping its shape; a small video grows too. */
  video {
    display: block;
    width: 100%;
    height: 100%;
    object-fit: contain;
  }
  .failed {
    margin: auto;
    font: var(--t-small);
    color: var(--slate);
  }
</style>
