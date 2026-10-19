<script>
  // The Themes sheet (desktop-design-5-6 2.14, 3.2, 3.3, look 5.14;
  // THEME-008 to THEME-017): a port of internal/ui/dialog/theme/theme.go,
  // docked at the right edge with no veil. "New Theme..." first, then
  // "System" (built-ins, " (overridden)" when a user file shadows one) and
  // "User"; "current" marks the desktop's own theme. Moving previews the
  // highlighted theme on this window; Enter or Ctrl+Y applies and saves the
  // choice; Esc reverts. Ctrl+E edits, Ctrl+R renames a user theme in its row,
  // Ctrl+X asks before deleting one, Ctrl+D reverts an overridden built-in.
  // The help footer lists only what the highlighted row allows.
  import { onMount, tick } from "svelte";
  import Field from "../kit/Field.svelte";
  import FilterList from "../kit/FilterList.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { closeDialog, openDialog } from "../../lib/dialogs.js";
  import { api } from "../../lib/engine/api.js";
  import { settings_set } from "../../lib/engine/commands.js";
  import { chooseTheme, chosenTheme, endPreview, loadCurrent, previewView, themeSetting } from "../../lib/themes/current.svelte.js";
  import { notify } from "../../lib/stores/ui.svelte.js";

  const ID = "theme";
  const NEW = "\u0000new";

  let list = $state([]); // ThemeListItem[]
  let query = $state("");
  let selected = $state(0);
  let mode = $state("normal"); // "normal" | "rename" | "delete"
  let renameText = $state("");
  let target = null; // the theme name the rename or delete is for
  let applied = false;
  let filter = $state(null);
  const views = new Map(); // name → ThemeView, for this sheet's life
  let previewSeq = 0;

  const items = $derived.by(() => {
    const system = list.filter((t) => t.source === "builtin" || t.overridden).sort((a, b) => (a.name < b.name ? -1 : 1));
    const user = list.filter((t) => t.source === "user" && !t.overridden).sort((a, b) => (a.name < b.name ? -1 : 1));
    return [
      { name: NEW, label: "New Theme...", group: "" },
      ...system.map((t) => ({ ...t, label: t.overridden ? `${t.name} (overridden)` : t.name, group: "System" })),
      ...user.map((t) => ({ ...t, label: t.name, group: "User" })),
    ];
  });
  const current = $derived(items[selected] ?? null);
  const isUser = (item) => item && item.name !== NEW && item.source === "user" && !item.overridden;
  const isOverridden = (item) => item && item.name !== NEW && item.overridden === true;

  const help = $derived.by(() => {
    if (mode === "delete")
      return [
        { key: "y", text: "delete" },
        { key: "n", text: "cancel" },
      ];
    if (mode === "rename")
      return [
        { key: "enter", text: "confirm" },
        { key: "esc", text: "cancel" },
      ];
    const pairs = [
      { key: "↑/↓", text: "choose" },
      { key: "enter", text: "choose" },
    ];
    if (current && current.name !== NEW) pairs.push({ key: "ctrl+e", text: "edit" });
    if (isUser(current)) pairs.push({ key: "ctrl+r", text: "rename" }, { key: "ctrl+x", text: "delete" });
    if (isOverridden(current)) pairs.push({ key: "ctrl+d", text: "revert" });
    pairs.push({ key: "esc", text: "cancel" });
    return pairs;
  });

  async function read(selectName = null) {
    try {
      const answer = await api.themes();
      list = Array.isArray(answer?.themes) ? answer.themes : [];
      views.clear();
    } catch (error) {
      notify("error", error?.message ?? String(error));
      return;
    }
    await tick();
    const want = selectName ?? chosenTheme();
    const at = items.findIndex((item) => item.name === want);
    selected = at >= 0 ? at : 0;
  }

  // The highlighted theme on this window; newest load wins.
  async function preview(item) {
    const mine = ++previewSeq;
    if (!item || item.name === NEW) {
      endPreview();
      return;
    }
    let view = views.get(item.name);
    if (!view) {
      try {
        view = await api.theme(item.name);
      } catch {
        return;
      }
      views.set(item.name, view);
    }
    if (mine === previewSeq && !applied) previewView(view);
  }

  $effect(() => {
    const item = current;
    if (mode === "normal") preview(item);
  });

  async function choose(item) {
    if (!item) return;
    if (item.name === NEW) {
      openDialog("theme_new", {}, { grace: true });
      return;
    }
    try {
      await chooseTheme(item.name);
      applied = true;
      notify("info", `Theme switched to ${item.name}`);
      closeDialog(ID);
    } catch (error) {
      endPreview();
      notify("error", error?.message ?? String(error));
    }
  }

  function startRename() {
    if (!isUser(current)) return;
    target = current.name;
    renameText = current.name;
    mode = "rename";
  }

  async function saveRename() {
    const from = target;
    const to = renameText.trim();
    mode = "normal";
    target = null;
    if (!from || !to || to === from) return;
    try {
      await api.renameTheme(from, to);
      if (themeSetting() === from) await chooseTheme(to);
      notify("info", `Renamed theme ${from} to ${to}`);
      await read(to);
    } catch (error) {
      notify("error", error?.message ?? String(error));
    }
  }

  function startDelete() {
    if (!isUser(current)) return;
    target = current.name;
    mode = "delete";
  }

  async function confirmDelete() {
    const name = target;
    mode = "normal";
    target = null;
    if (!name) return;
    try {
      await api.deleteTheme(name);
      if (themeSetting() === name) {
        await settings_set("theme", "");
        await loadCurrent({ force: true });
      }
      notify("info", `Deleted theme ${name}`);
      await read();
    } catch (error) {
      notify("error", error?.message ?? String(error));
    }
  }

  async function revert() {
    if (!isOverridden(current)) return;
    const name = current.name;
    try {
      await api.revertTheme(name);
      views.delete(name);
      notify("info", `Reverted ${name} to its built-in colors`);
      if (chosenTheme() === name) await loadCurrent({ force: true });
      await read(name);
    } catch (error) {
      notify("error", error?.message ?? String(error));
    }
  }

  function onKey(event, combo) {
    if (mode === "delete") {
      if (combo === "y" || combo === "Shift+Y" || combo === "Enter") confirmDelete();
      else if (combo === "n" || combo === "Shift+N" || combo === "Escape" || combo === "Alt+Escape") {
        mode = "normal";
        target = null;
      }
      return true;
    }
    if (mode === "rename") {
      if (combo === "Enter") {
        saveRename();
        return true;
      }
      if (combo === "Escape" || combo === "Alt+Escape") {
        mode = "normal";
        target = null;
        return true;
      }
      return combo === "ArrowUp" || combo === "ArrowDown" || combo === "Tab" || combo === "Shift+Tab";
    }
    switch (combo) {
      case "Enter":
      case "Ctrl+Y":
        choose(filter?.chosen());
        return true;
      case "Ctrl+E":
        if (current && current.name !== NEW) openDialog("theme_editor", { name: current.name }, { grace: true });
        return true;
      case "Ctrl+R":
        startRename();
        return true;
      case "Ctrl+X":
        startDelete();
        return true;
      case "Ctrl+D":
        revert();
        return true;
      default:
        return false;
    }
  }

  // The row's field takes the focus while renaming.
  $effect(() => {
    const now = mode;
    tick().then(() => {
      const sheet = document.querySelector(`[data-sheet="${ID}"]`);
      const input = now === "rename" ? sheet?.querySelector(".rename input") : sheet?.querySelector("input");
      input?.focus();
      if (now === "rename") input?.select();
    });
  });

  onMount(() => {
    read();
    // Esc, or any other close, reverts an unapplied preview.
    return () => {
      previewSeq += 1;
      if (!applied) endPreview();
    };
  });
</script>

<Sheet id={ID} title="Themes" width="small" docked {help} {onKey}>
  <Field bind:value={query} icon="search" placeholder="Type to filter" label="Filter themes" />
  {#if mode === "rename"}
    <p class="ask">Rename this theme?</p>
  {:else if mode === "delete"}
    <p class="ask strong">Delete this theme?</p>
  {/if}
  <FilterList
    bind:this={filter}
    {items}
    filterText={(item) => item.label}
    groups={(item) => item.group}
    info={(item) => (item.name === chosenTheme() ? "current" : "")}
    {query}
    bind:selected
    onChoose={(item) => mode === "normal" && choose(item)}
    label="Themes"
    row={mode === "rename" ? renameRow : null}
  />
</Sheet>

{#snippet renameRow(item, index, state)}
  {#if state.selected}
    <div class="rename">
      <Field bind:value={renameText} prompt="> " label="New name" />
    </div>
  {:else}
    <span class="plain">{item.label}</span>
  {/if}
{/snippet}

<style>
  .ask {
    margin: calc(6 * var(--px)) 0 0;
    font: var(--t-small);
    color: var(--slate);
  }
  .ask.strong {
    color: var(--ink);
  }
  .rename {
    flex: 1;
    min-width: 0;
  }
  .plain {
    font: var(--t-item);
    color: var(--ink);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
