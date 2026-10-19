<script>
  // The command palette (desktop-design-5-6 2.2, look 5.3; CMD-001 to
  // CMD-013, CMD-057 to CMD-059): a port of internal/ui/dialog/commands.go.
  // System entries from lib/palette/entries.js; User (custom commands and
  // skills) and MCP (prompts) categories when they have entries, switched
  // with Tab / Shift+Tab, which clears the filter. ↑/Ctrl+P and ↓ move and
  // wrap; Enter/Ctrl+Y runs; any other key equal to a shown entry's shortcut
  // runs it (after Close, Previous, Next, Select and Tab, so Ctrl+Y and
  // Ctrl+P never run an entry from here). The entries rebuild while open,
  // keeping the filter and the selected entry.
  import { onMount, untrack } from "svelte";
  import Field from "../kit/Field.svelte";
  import FilterList from "../kit/FilterList.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import Icon from "../Icon.svelte";
  import { closeDialog, openDialog } from "../../lib/dialogs.js";
  import { api } from "../../lib/engine/api.js";
  import { initProject, sendPrompt, summarize, toggleYolo } from "../../lib/palette/actions.js";
  import { commandArguments, fillArguments, promptArguments } from "../../lib/palette/customcommands.js";
  import { entryFilter, systemEntries } from "../../lib/palette/entries.js";
  import { palette, watchCommands } from "../../lib/palette/store.svelte.js";
  import { attachSkill } from "../../lib/stores/composer.svelte.js";
  import { notify } from "../../lib/stores/ui.svelte.js";

  const ID = "commands";
  const CATEGORIES = ["System", "User", "MCP"];

  const system = systemEntries({ summarize, toggleYolo, initProject });

  let category = $state("System");
  let query = $state("");
  let selected = $state(0);
  let selectedId = null;
  let list = $state(null);

  const userEntries = $derived(
    palette.commands.map((command) => ({
      id: `custom_${command.id}`,
      label: command.skill ? command.skill.name || command.name : command.name,
      detail: command.skill?.description ?? "",
      shortcut: "",
      aliases: [],
      run: () => runCommand(command),
    })),
  );
  const mcpEntries = $derived(
    palette.prompts.map((prompt) => ({
      id: `mcp_${prompt.id}`,
      label: prompt.prompt_id,
      detail: "",
      shortcut: "",
      aliases: [],
      // Without arguments the palette stays, showing "Generating Prompt...".
      keepOpen: promptArguments(prompt).length === 0,
      run: () => runPrompt(prompt),
    })),
  );
  const categories = $derived(CATEGORIES.filter((c) => c === "System" || (c === "User" ? userEntries.length > 0 : mcpEntries.length > 0)));
  const items = $derived(category === "User" ? userEntries : category === "MCP" ? mcpEntries : system.filter((entry) => entry.shown()));

  const help = [
    { key: "tab", text: "switch selection" },
    { key: "↑/↓", text: "choose" },
    { key: "enter", text: "confirm" },
    { key: "esc", text: "cancel" },
  ];

  // A vanished category falls back to System.
  $effect(() => {
    if (!categories.includes(category)) category = "System";
  });

  // The selected entry stays selected by id while the entries rebuild.
  $effect(() => {
    const list = items;
    untrack(() => {
      const at = selectedId ? list.findIndex((entry) => entry.id === selectedId) : -1;
      if (at >= 0) selected = at;
    });
  });
  $effect(() => {
    selectedId = items[selected]?.id ?? null;
  });

  function switchCategory(step) {
    const at = categories.indexOf(category);
    category = categories[(at + step + categories.length) % categories.length];
    query = "";
    selected = 0;
    selectedId = null;
  }

  function run(entry) {
    if (!entry || palette.generating) return;
    if (!entry.keepOpen) closeDialog(ID);
    entry.run();
  }

  // -- User and MCP entries --

  function runCommand(command) {
    if (command.skill) {
      attachSkill({ id: command.skill.skill_file_path, name: command.skill.name || command.name });
      return;
    }
    const args = commandArguments(command);
    if (args.length === 0) {
      sendPrompt(command.content);
      return;
    }
    openDialog("arguments", {
      title: "Custom Command Arguments",
      args,
      onSubmit: (values) => {
        closeDialog("arguments");
        sendPrompt(fillArguments(command.content, values));
      },
    });
  }

  async function fetchPrompt(prompt, values) {
    palette.generating = true;
    try {
      const answer = await api.getMcpPrompt({ client_id: prompt.client_id, prompt_id: prompt.prompt_id, args: values });
      if (!palette.generating) return; // cancelled
      closeDialog("arguments");
      closeDialog(ID);
      if (answer?.prompt) sendPrompt(answer.prompt);
    } catch (error) {
      notify("error", error?.message ?? String(error));
    } finally {
      palette.generating = false;
    }
  }

  function runPrompt(prompt) {
    const args = promptArguments(prompt);
    if (args.length === 0) {
      fetchPrompt(prompt, {});
      return;
    }
    openDialog("arguments", {
      title: prompt.title || "MCP Prompt Arguments",
      args,
      onSubmit: (values) => fetchPrompt(prompt, values),
    });
  }

  // -- Keys (commands.go:170-230) --

  // "ctrl+n" → "Ctrl+N", as lib/keys.js writes combos.
  function comboOfShortcut(shortcut) {
    const parts = shortcut.split("+");
    const key = parts.pop();
    const mods = parts.map((m) => m[0].toUpperCase() + m.slice(1));
    return [...mods, mods.length > 0 && key.length === 1 ? key.toUpperCase() : key].join("+");
  }

  function onKey(event, combo) {
    switch (combo) {
      case "ArrowUp":
      case "Ctrl+P":
        list?.move(-1);
        return true;
      case "ArrowDown":
        list?.move(1);
        return true;
      case "Enter":
      case "Ctrl+Y":
        run(list?.chosen());
        return true;
      case "Tab":
        switchCategory(1);
        return true;
      case "Shift+Tab":
        switchCategory(-1);
        return true;
      default: {
        const entry = items.find((e) => e.shortcut && comboOfShortcut(e.shortcut) === combo);
        if (!entry) return false;
        run(entry);
        return true;
      }
    }
  }

  onMount(() => watchCommands());
</script>

<Sheet id={ID} title="Commands" width="standard" maxHeight={640} help={palette.generating ? [] : help} {onKey} controls={categories.length > 1 ? controls : null}>
  <Field bind:value={query} icon="search" placeholder="Type to filter" label="Filter commands" />
  <FilterList
    bind:this={list}
    {items}
    filterText={entryFilter}
    title={(entry) => entry.label}
    info={(entry) => entry.shortcut}
    detail={category === "User" ? (entry) => entry.detail : null}
    infoBudget={25}
    {query}
    bind:selected
    onChoose={(entry) => run(entry)}
    label="Commands"
  />
  {#if palette.generating}
    <p class="generating"><Icon name="working" size={16} /> Generating Prompt...</p>
  {/if}
</Sheet>

{#snippet controls()}
  {#each categories as c (c)}
    <button
      type="button"
      aria-pressed={category === c}
      onclick={() => {
        category = c;
        query = "";
        selected = 0;
      }}>{c}</button
    >
  {/each}
{/snippet}

<style>
  .generating {
    display: flex;
    align-items: center;
    gap: calc(10 * var(--px));
    margin: calc(12 * var(--px)) 0 0 calc(23 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }
</style>
