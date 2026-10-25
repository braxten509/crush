<script>
  // The Open Project sheet (desktop-design-5-6 2.5, look 5.7; DLG-113 to
  // DLG-118): a port of internal/ui/dialog/projects.go. Rows are the saved
  // projects of projects.json (read each time the sheet opens), short paths,
  // "current" on this window's project, filtered by path. Enter/Ctrl+Y opens
  // one: this window switches to it in place (switchFolder, no reload);
  // Ctrl+X stops saving one at once. With no saved project the sheet closes
  // at once with "no saved projects yet, use Save Project first"
  // (projects.go:66-73), drawing nothing meanwhile.
  import Field from "../kit/Field.svelte";
  import FilterList from "../kit/FilterList.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { closeDialog } from "../../lib/dialogs.js";
  import { projectRemove, projectsList } from "../../lib/engine/commands.js";
  import { clean, short } from "../../lib/leftlist/picker.js";
  import { leftList } from "../../lib/leftlist/store.svelte.js";
  import { conn, switchFolder } from "../../lib/stores/connection.svelte.js";
  import { notify } from "../../lib/stores/ui.svelte.js";

  const ID = "projects";

  let rows = $state(null);
  let query = $state("");
  let selected = $state(0);
  let list = $state(null);

  const here = $derived(clean(conn.project.path || conn.project.cwd || ""));
  const isCurrent = (row) => clean(row.path) === here;
  const titleOf = (row) => short(row.path, leftList.home);

  const help = [
    { key: "↑/↓", text: "choose" },
    { key: "enter", text: "open" },
    { key: "ctrl+x", text: "remove" },
    { key: "esc", text: "close" },
  ];

  // dialog_actions.go:406-420.
  async function open(row) {
    if (!row) return;
    if (isCurrent(row)) {
      closeDialog(ID);
      notify("info", "Already in this project");
      return;
    }
    closeDialog(ID);
    // In place, with no reload (owner, 2026-10-20: the folder is per chat).
    // A failed switch shows its own notice, with Try again.
    await switchFolder(row.path);
  }

  // projects.go:136-150: the row goes at once, the folder stays.
  async function remove(row) {
    if (!row) return;
    const at = rows.indexOf(row);
    rows = rows.filter((r) => r !== row);
    selected = rows.length === 0 ? -1 : Math.min(at, rows.length - 1);
    try {
      await projectRemove(row.path);
      notify("info", `Removed saved project ${titleOf(row)}`);
    } catch (error) {
      notify("error", error?.message ?? String(error));
    }
  }

  projectsList()
    .then((all) => {
      const saved = (all?.projects ?? []).filter((row) => row?.saved === true && row.path);
      if (saved.length === 0) {
        closeDialog(ID);
        notify("info", "no saved projects yet, use Save Project first");
        return;
      }
      rows = saved;
      const at = saved.findIndex(isCurrent);
      selected = at >= 0 ? at : 0;
    })
    .catch((error) => {
      closeDialog(ID);
      notify("error", error?.message ?? String(error));
    });

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
        open(list?.chosen());
        return true;
      case "Ctrl+X":
        remove(list?.chosen());
        return true;
      default:
        return false;
    }
  }
</script>

{#if rows}
<Sheet id={ID} title="Open project" width="small" {help} {onKey}>
  <Field bind:value={query} icon="search" placeholder="Type to filter" label="Filter projects" />
  <FilterList
    bind:this={list}
    items={rows}
    filterText={(row) => row.path}
    title={titleOf}
    info={(row) => (isCurrent(row) ? "current" : "")}
    {query}
    bind:selected
    onChoose={(row) => open(row)}
    label="Projects"
  />
</Sheet>
{/if}
