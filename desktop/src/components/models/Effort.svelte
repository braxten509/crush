<script>
  // The effort sheet (desktop-design-5-6 2.3 "Effort sheet", look 5.4;
  // DLG-035 to DLG-040): a port of internal/ui/dialog/reasoning.go. Rows are
  // the coder model's reasoning levels in Title Case, "current" on the level
  // in force (else the model's default). With Ultracode support, Tab flips
  // the Ultracode line, saved together with the level. Enter/Ctrl+Y saves; a
  // failed save keeps the sheet open with its error; Esc cancels both.
  // Opening errors close it at once with the error (DLG-040).
  import { untrack } from "svelte";
  import Field from "../kit/Field.svelte";
  import FilterList from "../kit/FilterList.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import Icon from "../Icon.svelte";
  import { closeDialog } from "../../lib/dialogs.js";
  import { capabilities } from "../../lib/models/capabilities.svelte.js";
  import { levelTitle } from "../../lib/models/groups.js";
  import { coderModel, modelData, refreshModels } from "../../lib/models/store.svelte.js";
  import { setEffort } from "../../lib/models/toggles.js";
  import { notify } from "../../lib/stores/ui.svelte.js";

  const ID = "reasoning";

  let query = $state("");
  let selected = $state(-1);
  let list = $state(null);
  let ready = $state(false);
  let ultracode = $state(null); // null without Ultracode support
  let error = $state("");
  let saving = false;

  const coder = $derived(coderModel(modelData));
  const levels = $derived(coder.info?.reasoning_levels ?? []);
  const currentLevel = $derived(coder.selected?.reasoning_effort || coder.info?.default_reasoning_effort || "");
  const help = $derived([
    { key: "↑/↓", text: "choose" },
    { key: "tab", text: "ultracode", disabled: ultracode === null },
    { key: "enter", text: "confirm" },
    { key: "esc", text: "close" },
  ]);

  (async () => {
    if (!modelData.cfg) await refreshModels();
    const { selected: model, info } = coderModel(modelData);
    const problem = !modelData.cfg || !model ? "agent configuration not found" : !info ? "model configuration not found" : !(info.reasoning_levels?.length > 0) ? "no reasoning levels available" : "";
    if (problem) {
      closeDialog(ID);
      notify("error", problem);
      return;
    }
    ultracode = capabilities.supports_ultracode ? model.ultracode === true : null;
    ready = true;
  })();

  // The level in force is selected when the rows first show.
  $effect(() => {
    if (!ready) return;
    untrack(() => {
      const at = levels.indexOf(currentLevel);
      selected = at >= 0 ? at : levels.length > 0 ? 0 : -1;
    });
  });

  async function confirm() {
    const level = list?.chosen();
    if (!level || saving) return; // no selection: Enter does nothing (reasoning.go:150)
    saving = true;
    try {
      await setEffort(level, ultracode);
      closeDialog(ID);
    } catch (e) {
      error = e?.message ?? String(e);
    } finally {
      saving = false;
    }
  }

  function onKey(event, combo) {
    switch (combo) {
      case "Tab":
        if (ultracode !== null) ultracode = !ultracode;
        return true;
      case "Ctrl+P":
        list?.move(-1);
        return true;
      case "Ctrl+N":
        list?.move(1);
        return true;
      case "Enter":
      case "Ctrl+Y":
        confirm();
        return true;
      default:
        return false;
    }
  }
</script>

{#if ready}
  <Sheet id={ID} title="Select reasoning effort" width="small" {help} {onKey}>
    <Field bind:value={query} icon="search" placeholder="Type to filter" label="Filter levels" />
    <FilterList
      bind:this={list}
      items={levels}
      filterText={levelTitle}
      info={(level) => (level === currentLevel ? "current" : "")}
      {query}
      bind:selected
      onChoose={() => confirm()}
      label="Reasoning levels"
    />
    {#if ultracode !== null}
      <button type="button" class="switch" aria-pressed={ultracode} onclick={() => (ultracode = !ultracode)}>
        <span class="box" class:on={ultracode}>{#if ultracode}<Icon name="check" size={12} />{/if}</span>
        <span>Ultracode {ultracode ? "on" : "off"} · workflows on every task</span>
      </button>
    {/if}
    {#if error}
      <p class="error">{error}</p>
    {/if}
  </Sheet>
{/if}

<style>
  .switch {
    display: flex;
    align-items: center;
    gap: calc(10 * var(--px));
    margin: calc(12 * var(--px)) 0 0;
    padding: 0;
    border: 0;
    background: none;
    font: var(--t-small);
    color: var(--ink);
    cursor: pointer;
  }
  .box {
    box-sizing: border-box;
    display: grid;
    place-items: center;
    width: calc(16 * var(--px));
    height: calc(16 * var(--px));
    border: 1px solid var(--rule-control);
    color: var(--paper);
  }
  .box.on {
    background: var(--ink);
    border-color: var(--ink);
  }
  .error {
    margin: calc(8 * var(--px)) 0 0;
    font: var(--t-small);
    color: var(--error-ink);
  }
</style>
