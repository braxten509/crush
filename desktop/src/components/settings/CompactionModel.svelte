<script>
  // The compaction model row in Settings (owner, 2026-10-25): the model that
  // compacts chats on an API model, so a costly model need not compact
  // itself. Unset, each chat compacts on its own model; agent CLI chats keep
  // their own memory either way. "Change…" opens the model picker for it
  // (models.compact); "Use the chat's model" removes it.
  import { openDialog } from "../../lib/dialogs.js";
  import { api, SCOPE } from "../../lib/engine/api.js";
  import { modelInfo } from "../../lib/models/providers.js";
  import { modelData, refreshModels, watchModels } from "../../lib/models/store.svelte.js";
  import { notify } from "../../lib/stores/ui.svelte.js";

  const chosen = $derived(modelData.cfg?.models?.compact ?? null);
  const name = $derived(chosen ? modelInfo(modelData.cfg, modelData.catalog, chosen)?.name || chosen.model : "The chat's own model");

  $effect(() => watchModels());

  async function useChatModel() {
    try {
      await api.removeConfig(SCOPE.global, "models.compact");
      await refreshModels();
      notify("info", "Chats compact on their own model");
    } catch (error) {
      notify("error", error?.message ?? String(error));
    }
  }
</script>

<div class="row">
  <span class="name">Compaction model</span>
  <span class="value">{name}</span>
  {#if chosen}<button type="button" class="act" onclick={useChatModel}>Use the chat's model</button>{/if}
  <button type="button" class="act" onclick={() => openDialog("models", { compact: true })}>Change…</button>
</div>

<style>
  /* As the Settings page's rows (Settings.svelte), and HelperModel.svelte. */
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
