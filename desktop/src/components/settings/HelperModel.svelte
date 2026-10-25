<script>
  // The helper model row in Settings (owner, 2026-10-24): the one fixed model
  // titles and branch summaries run on. Each chat owns its own model, so this
  // is the only model set in the config. "Change…" opens the model picker for
  // the helper; a choice there writes POST config/model {model_type: "small"}.
  import { openDialog } from "../../lib/dialogs.js";
  import { modelInfo } from "../../lib/models/providers.js";
  import { modelData, watchModels } from "../../lib/models/store.svelte.js";

  const helper = $derived(modelData.cfg?.models?.small ?? null);
  const name = $derived(helper ? modelInfo(modelData.cfg, modelData.catalog, helper)?.name || helper.model : "Not set");

  $effect(() => watchModels());
</script>

<div class="row">
  <span class="name">Helper model (titles)</span>
  <span class="value">{name}</span>
  <button type="button" class="act" onclick={() => openDialog("models", { helper: true })}>Change…</button>
</div>

<style>
  /* As the Settings page's rows (Settings.svelte). */
  .row {
    display: flex;
    align-items: center;
    gap: calc(16 * var(--px));
    min-height: calc(56 * var(--px));
    padding: calc(6 * var(--px)) 0;
    border-bottom: 1px solid var(--rule-control);
  }
  .name {
    flex: 1;
    min-width: 0;
    font: var(--t-item);
    color: var(--ink);
  }
  .value {
    font: var(--t-small);
    color: var(--slate);
  }
  .act {
    height: calc(36 * var(--px));
    padding: 0 calc(16 * var(--px));
    border: 1px solid var(--rule-control);
    border-radius: 0;
    background: var(--control-rest);
    font: var(--t-button);
    color: var(--ink);
    cursor: pointer;
  }
  .act:hover {
    background: var(--control-hover);
  }
  .act:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }
</style>
