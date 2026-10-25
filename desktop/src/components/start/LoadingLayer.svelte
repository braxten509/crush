<script>
  // The gray loading layer (owner, 2026-10-25): while the app starts or a
  // folder opens (startmodel.js loadingView), the whole window under the
  // title bar grays out behind the activity mark and what is loading, and
  // takes the clicks. It waits 400 ms, as the engine gate does, so a quick
  // load never flickers. Dialogs and notices stay above it.
  import { fade } from "svelte/transition";
  import { probeSection } from "../../lib/probe.js";
  import { conn } from "../../lib/stores/connection.svelte.js";
  import { sessions } from "../../lib/stores/sessions.svelte.js";
  import ActivityDots from "./ActivityDots.svelte";
  import { loadingLayer } from "./loading.svelte.js";
  import { start } from "./start.svelte.js";
  import { loadingView } from "./startmodel.js";

  const DELAY_MS = 400;
  const FADE_MS = 150;

  const view = $derived(
    loadingView({
      state: conn.state,
      workspace: conn.workspace,
      switching: conn.switching,
      switchingTo: conn.switchingTo,
      listLoaded: sessions.loaded || start.listGaveUp,
    }),
  );
  const pending = $derived(view !== null);

  $effect(() => {
    if (!pending) {
      loadingLayer.shown = false;
      return;
    }
    const timer = setTimeout(() => (loadingLayer.shown = true), DELAY_MS);
    return () => clearTimeout(timer);
  });
  // The label follows the load under way; the last one stays while the layer fades.
  $effect(() => {
    if (view) loadingLayer.label = view.label;
  });

  probeSection("loading", () => ({ pending, shown: loadingLayer.shown, label: loadingLayer.label }));
</script>

{#if loadingLayer.shown}
  <div class="layer" transition:fade={{ duration: FADE_MS }} data-probe="loading-layer">
    <p class="line" role="status">
      <ActivityDots />
      <span class="label">{loadingLayer.label}</span>
    </p>
  </div>
{/if}

<style>
  /* The page washed toward the paper and drained of color: grayed out, with
     the line still reading on it. */
  .layer {
    position: absolute;
    inset: 0;
    z-index: 5;
    display: flex;
    align-items: center;
    justify-content: center;
    background: color-mix(in srgb, var(--paper) 62%, transparent);
    backdrop-filter: grayscale(1);
    cursor: progress;
  }
  .line {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: calc(14 * var(--px));
    font: var(--t-small);
    color: var(--ink);
    white-space: nowrap;
  }
</style>
