<script>
  // Add OpenRouter models (owner, 2026-10-22, pick B): opened by "+ Add
  // model" under the OpenRouter group of the model menu. It reads the
  // account's catalog (GET …/openrouter/catalog: the account's provider and
  // privacy preferences applied, tool-capable models only), searchable by
  // name or id, each row with its price per million tokens in and out and
  // its context size. Space, Enter or a click ticks a row (the pointer selects, as in the palette); Done saves the
  // ticked ones after the saved ones (providers.<id>.models in the global
  // config) and opens the model menu again on the group, so they show at
  // once. Saved models show ticked and stay; the menu's right-click removes.
  //   providerId, providerName, anchor? (the model button, to reopen the menu)
  import { onMount } from "svelte";
  import Buttons from "../kit/Buttons.svelte";
  import Field from "../kit/Field.svelte";
  import FilterList from "../kit/FilterList.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import Icon from "../Icon.svelte";
  import { closeDialog } from "../../lib/dialogs.js";
  import { api, SCOPE } from "../../lib/engine/api.js";
  import { catalogRows, contextText, priceText, savedWith } from "../../lib/models/openrouter.js";
  import { modelData, refreshModels } from "../../lib/models/store.svelte.js";
  import { notify, openMenu } from "../../lib/stores/ui.svelte.js";

  const ID = "openrouter_add";

  let { providerId = "openrouter", providerName = "OpenRouter", anchor = null } = $props();

  let catalog = $state(null); // null while reading
  let failed = $state("");
  let query = $state("");
  let selected = $state(0);
  let picked = $state(new Set());
  let list = $state(null);
  let saving = false;

  const savedIds = $derived(new Set((modelData.cfg?.providers.get(providerId)?.models ?? []).map((m) => m.id)));
  const rows = $derived(catalog ? catalogRows(catalog, query) : []);
  const count = $derived(picked.size);
  const help = [
    { key: "↑/↓", text: "choose" },
    { key: "space", text: "tick" },
    { key: "ctrl+s", text: "done" },
    { key: "esc", text: "close" },
  ];

  function toggle(model) {
    if (!model || savedIds.has(model.id)) return;
    const next = new Set(picked);
    if (next.has(model.id)) next.delete(model.id);
    else next.add(model.id);
    picked = next;
  }

  async function done() {
    if (saving) return;
    saving = true;
    const chosen = (catalog ?? []).filter((m) => picked.has(m.id));
    try {
      if (chosen.length > 0) {
        // The saved entries as the engine holds them, prices included (the
        // menu's copy keeps only what it draws).
        const raw = await api.config();
        await api.setConfig(SCOPE.global, `providers.${providerId}.models`, savedWith(raw?.providers?.[providerId]?.models, chosen));
        await refreshModels();
        notify("info", `Saved ${chosen.length} ${providerName} model${chosen.length === 1 ? "" : "s"}`);
      }
    } catch (error) {
      notify("error", error?.message ?? String(error));
      saving = false;
      return;
    }
    closeDialog(ID);
    if (anchor?.isConnected) openMenu({ kind: "model", anchor, group: providerName });
  }

  function onKey(event, combo) {
    switch (combo) {
      case "Space":
        if (event.target?.tagName === "INPUT") return false;
        toggle(list?.chosen());
        return true;
      case "Enter":
        toggle(list?.chosen());
        return true;
      case "Ctrl+S":
        done();
        return true;
      default:
        return false;
    }
  }

  onMount(() => {
    if (!modelData.cfg) refreshModels();
    api
      .openRouterCatalog()
      .then((models) => (catalog = Array.isArray(models) ? models : []))
      .catch((error) => {
        failed = error?.message ?? String(error);
        catalog = [];
      });
  });
</script>

<Sheet id={ID} title="Add {providerName} models" width="standard" maxHeight={720} {help} {onKey}>
  <Field bind:value={query} icon="search" placeholder={catalog?.length ? `Search ${catalog.length} models` : catalog ? "Search models" : "Reading the catalog…"} label="Search models" />
  <p class="heads"><span>Name</span><span>In / Out per 1M · Context</span></p>
  <FilterList
    bind:this={list}
    items={rows}
    filterText={(m) => `${m.name}${m.id}`}
    bind:selected
    row={modelRow}
    onChoose={(m) => toggle(m)}
    hoverRuns
    empty={failed || (catalog ? "No models match." : "Reading the catalog…")}
    label="{providerName} models"
  />
  <div class="end">
    <span class="count">{count > 0 ? `${count} to add` : ""}</span>
    <Buttons buttons={[{ id: "done", label: "Done" }]} selected="done" onPress={done} />
  </div>
</Sheet>

{#snippet modelRow(m)}
  {@const on = savedIds.has(m.id) || picked.has(m.id)}
  <span class="line" class:saved={savedIds.has(m.id)}>
    <span class="box" role="checkbox" aria-checked={on} aria-label={m.name}>{#if on}<Icon name="check" size={12} />{/if}</span>
    <span class="name">{m.name}</span>
    <span class="info">{savedIds.has(m.id) ? "saved · " : ""}{priceText(m)}{contextText(m.context_window) ? ` · ${contextText(m.context_window)}` : ""}</span>
  </span>
{/snippet}

<style>
  .heads {
    display: flex;
    justify-content: space-between;
    margin: calc(10 * var(--px)) 0 calc(4 * var(--px));
    padding: 0 calc(12 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }
  .line {
    display: flex;
    align-items: center;
    gap: calc(12 * var(--px));
    flex: 1;
    min-width: 0;
  }
  .box {
    flex: none;
    display: grid;
    place-items: center;
    box-sizing: border-box;
    width: calc(16 * var(--px));
    height: calc(16 * var(--px));
    border: 1px solid var(--rule-control);
    color: var(--vermilion);
  }
  .saved .box {
    color: var(--slate);
  }
  .name {
    flex: 1;
    min-width: 0;
    font: var(--t-item);
    color: var(--ink);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .info {
    flex: none;
    font: var(--t-small);
    color: var(--slate);
    white-space: nowrap;
  }
  .end {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: calc(16 * var(--px));
    margin-top: calc(12 * var(--px));
  }
  .count {
    font: var(--t-small);
    color: var(--slate);
  }
</style>
