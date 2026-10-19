<script>
  // START-001: with no usable model (the agent is not ready and no provider is
  // set up) the middle column shows the wordmark (in the head) and the model
  // picker, opened as openDialog("models", {onboarding: true}); closing the
  // picker opens it again. The picker is phase 5's sheet: until it is
  // registered openDialog only logs, and this line says what comes next (the
  // picker's own onboarding title, internal/ui/dialog/models/models.go:282).
  import { hasDialog, openDialog } from "../../lib/dialogs.js";
  import StartLayer from "./StartLayer.svelte";
  import { screen } from "./start.svelte.js";
  import { shown } from "./shown.js";

  const LEAD = "To start, let's choose a provider and model.";

  const active = $derived(screen() === "onboarding");
  const open = $derived(hasDialog("models"));

  // Once on entry, then again each time the picker closes.
  let tried = false;
  $effect(() => {
    if (!active) {
      tried = false;
      return;
    }
    if (open) {
      tried = false;
      return;
    }
    if (tried) return;
    tried = true;
    openDialog("models", { onboarding: true });
  });

  $effect(() => {
    shown.onboarding = active ? { lead: LEAD, picker: open } : null;
    return () => {
      shown.onboarding = null;
    };
  });
</script>

{#if active}
  <StartLayer kind="onboarding" label="Choose a model">
    <div class="block">
      <p class="lead">{LEAD}</p>
    </div>
  </StartLayer>
{/if}

<style>
  /* CHAT-001's centred block, as on the landing page. */
  .block {
    box-sizing: border-box;
    width: 100%;
    max-width: calc(840 * var(--px));
    margin: 0 auto;
    padding: calc(30 * var(--px)) calc(60 * var(--px)) 0;
  }
  .lead {
    font: var(--t-reading);
    color: var(--ink);
  }
</style>
