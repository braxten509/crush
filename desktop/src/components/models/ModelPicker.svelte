<script>
  // The model picker (desktop-design-5-6 2.3, look 5.4; DLG-021 to DLG-034):
  // a port of internal/ui/dialog/models/models.go. Groups as
  // lib/models/groups.js builds them, the filter matching group and model
  // names without spaces or case. It picks the chat's own model (the last
  // pick with no chat open); opened from Settings with helper, it picks the
  // helper model (titles) instead. Enter/Ctrl+Y or a double click chooses
  // (lib/models/choose.js); Ctrl+E edits the sign-in of a configured
  // provider's model. The data is read again on config_changed while open,
  // keeping the selection on the same row. During onboarding the sheet sits
  // at the bottom left of the main column without the task selector.
  //   onboarding?, helper?, compact? (the compaction model, Settings)
  import { untrack } from "svelte";
  import Field from "../kit/Field.svelte";
  import FilterList from "../kit/FilterList.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { closeDialog } from "../../lib/dialogs.js";
  import { api, SCOPE } from "../../lib/engine/api.js";
  import { chooseModel } from "../../lib/models/choose.js";
  import { buildGroups, filterQuery, filterValue, selectedModelOf } from "../../lib/models/groups.js";
  import { coderModel, modelData, watchModels } from "../../lib/models/store.svelte.js";
  import { notify } from "../../lib/stores/ui.svelte.js";

  const ID = "models";

  let { onboarding = false, helper = false, compact = false } = $props();

  // "large" names the chats' recent list (config.SelectedModelTypeChat).
  const type = $derived(helper ? "small" : compact ? "compact" : "large");
  let query = $state("");
  let selected = $state(-1);
  let selectedId = null; // the selected row's id, kept across rebuilds
  let list = $state(null);
  let firstRead = $state(false);

  // The chat's own model (the last pick with no chat open), or the helper.
  const current = $derived((type === "small" ? modelData.cfg?.models?.small : type === "compact" ? modelData.cfg?.models?.compact : coderModel(modelData).selected) ?? {});
  const built = $derived(modelData.cfg ? buildGroups({ catalog: modelData.catalog, cfg: modelData.cfg, type, current }) : { items: [], selectedKey: "", prunedRecent: null });
  const items = $derived(built.items);
  const placeholder = $derived(onboarding ? "Find your fave" : helper ? "Choose the helper model for titles" : compact ? "Choose the model that compacts API chats" : "Choose a model for this chat");
  const chosenItem = $derived(selected >= 0 ? items[selected] : null);
  const editable = $derived(Boolean(chosenItem && modelData.cfg?.providers.has(chosenItem.provider.id)));
  const help = $derived([
    { key: "↑/↓", text: "choose" },
    { key: "enter", text: "confirm" },
    { key: "ctrl+e", text: "edit", disabled: !editable },
    { key: "esc", text: "close" },
  ]);

  $effect(() => watchModels());

  // A failed first read with nothing known keeps the sheet closed (DLG-029).
  $effect(() => {
    const revision = modelData.revision;
    const error = modelData.error;
    untrack(() => {
      if (revision > 0 && !firstRead) firstRead = true;
      if (error && modelData.catalog.length === 0 && (modelData.cfg?.providers.size ?? 0) === 0) {
        closeDialog(ID);
        notify("error", error);
      }
    });
  });

  // The selection: the same row after a rebuild, else the current model.
  $effect(() => {
    const list = items;
    const key = built.selectedKey;
    untrack(() => {
      let at = selectedId ? list.findIndex((item) => item.id === selectedId) : -1;
      if (at < 0) at = list.findIndex((item) => item.id === key);
      if (at < 0 && list.length > 0) at = 0;
      selected = at;
    });
  });
  $effect(() => {
    selectedId = items[selected]?.id ?? null;
  });

  // Stale "Recently used" entries are dropped from the config (models.go:582-587).
  let savedPrune = "";
  $effect(() => {
    const pruned = built.prunedRecent;
    if (!pruned) return;
    const text = `${type}:${JSON.stringify(pruned)}`;
    if (text === savedPrune) return;
    savedPrune = text;
    api.setConfig(SCOPE.global, `recent_models.${type}`, pruned).catch((error) => notify("error", `failed to update recent models: ${error?.message ?? error}`));
  });

  function choose(item, reAuthenticate = false) {
    if (!item) return;
    chooseModel({ provider: item.provider, model: selectedModelOf(item, modelData.cfg?.model_efforts), modelType: type, reAuthenticate, onboarding });
  }

  function onKey(event, combo) {
    switch (combo) {
      case "Ctrl+P":
        list?.move(-1);
        return true;
      case "Ctrl+N":
        list?.move(1);
        return true;
      case "Enter":
      case "Ctrl+Y":
        choose(list?.chosen());
        return true;
      case "Ctrl+E":
        if (editable) choose(list?.chosen(), true);
        return true;
      default:
        return false;
    }
  }

  const isCurrent = (item) => item.model.id === current.model && item.provider.id === current.provider;
</script>

<Sheet id={ID} title={helper ? "Helper model" : compact ? "Compaction model for API chats" : "Switch model"} width="standard" maxHeight={720} {help} {onKey} {onboarding}>
  {#if onboarding}
    <p class="lead">To start, let's choose a provider and model.</p>
  {/if}
  <Field bind:value={query} icon="search" {placeholder} label="Filter models" />
  {#if firstRead}
    <FilterList
      bind:this={list}
      {items}
      filterText={filterValue}
      title={(item) => item.model.name || item.model.id}
      info={(item) => (item.recent ? item.provider.name : isCurrent(item) ? "current" : "")}
      groups={(item) => (item.configured ? `${item.group} · Configured` : item.group)}
      query={filterQuery(query)}
      bind:selected
      onChoose={(item) => choose(item)}
      label="Models"
    />
  {/if}
</Sheet>

<style>
  .lead {
    margin: 0 0 calc(12 * var(--px));
    font: var(--t-reading);
    color: var(--ink);
  }
</style>
