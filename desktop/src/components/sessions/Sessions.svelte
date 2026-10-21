<script>
  // The Sessions sheet (desktop-design-5-6 2.5, look 5.7; DLG-076 to DLG-088):
  // a port of internal/ui/dialog/sessions/sessions.go. Rows are this
  // workspace's saved chats from GET …/saved-sessions, newest first, with
  // "~/folder · 3 hours ago" in the info column (35 % budget). Enter, Tab and
  // Ctrl+Y open the selected chat; Ctrl+R renames it in its row, Ctrl+X asks
  // to delete it; both go through POST …/saved-sessions as the terminal's
  // CatalogChange. Under "Recent", the 5 newest chats of any folder; then,
  // under the folder's name, the rest of this folder's chats (owner,
  // 2026-10-20). A chat of another folder switches the window there in
  // place. Session events rename or remove rows in place.
  //   query? (the filter's first text: the left list's search box, START-021's id)
  import { onMount, tick, untrack } from "svelte";
  import Buttons from "../kit/Buttons.svelte";
  import Field from "../kit/Field.svelte";
  import FilterList from "../kit/FilterList.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { windowBusy } from "../../lib/busy.js";
  import { closeDialog } from "../../lib/dialogs.js";
  import { api } from "../../lib/engine/api.js";
  import { file_info } from "../../lib/engine/commands.js";
  import { onEvent } from "../../lib/engine/events.js";
  import { folderLabel } from "../../lib/leftlist/numbers.js";
  import { leftList } from "../../lib/leftlist/store.svelte.js";
  import { infoText, inProject } from "../../lib/sessions/match.js";
  import { isClosed, reopen } from "../../lib/stores/closed.svelte.js";
  import { conn } from "../../lib/stores/connection.svelte.js";
  import { openInFolder } from "../../lib/stores/openchats.svelte.js";
  import { busy, openChat, sessions } from "../../lib/stores/sessions.svelte.js";
  import { notify } from "../../lib/stores/ui.svelte.js";
  import { secondNow } from "../../lib/live/helpers.svelte.js";

  const ID = "sessions";
  const RECENT = 5;
  const BUSY = "Agent is busy, please wait..."; // sessions.go:196-198

  let { query: firstQuery = "" } = $props();

  let query = $state(untrack(() => firstQuery));
  let rows = $state([]);
  let recent = $state(new Set()); // the ids under "Recent"
  let selected = $state(-1);
  let mode = $state("normal"); // "normal" | "rename" | "delete" (sessions.go's sessionsMode)
  let target = null; // the chat id that entered rename or delete; the action is for it alone
  let lockedAt = -1; // the selection while a mode runs: clicks do not move it (2.5)
  let renameText = $state("");
  let loaded = $state(false);
  let failed = $state(false);
  let list = $state(null);

  const now = $derived(secondNow());
  const help = $derived(
    mode === "delete"
      ? [
          { key: "y", text: "delete" },
          { key: "n", text: "cancel" },
        ]
      : mode === "rename"
        ? [
            { key: "enter", text: "confirm" },
            { key: "esc", text: "cancel" },
          ]
        : [
            { key: "↑↓", text: "choose" },
            { key: "ctrl+r", text: "rename" },
            { key: "ctrl+x", text: "delete" },
            { key: "enter", text: "choose" },
            { key: "esc", text: "close" },
          ],
  );

  async function read() {
    try {
      const all = (await api.savedSessions(sessions.currentId || "")) ?? [];
      const newest = [...all].sort((a, b) => Number(b.UpdatedAt ?? 0) - Number(a.UpdatedAt ?? 0)).slice(0, RECENT);
      const top = new Set(newest.map((row) => row.ID));
      const here = all.filter((row) => !top.has(row.ID) && inProject(row, conn.project.path || conn.project.cwd));
      const keep = rows[selected]?.ID ?? sessions.currentId;
      recent = top;
      rows = [...newest, ...here];
      failed = false;
      const at = rows.findIndex((row) => row.ID === keep);
      selected = at >= 0 ? at : rows.length > 0 ? 0 : -1;
    } catch (error) {
      failed = rows.length === 0;
      notify("error", error?.message ?? String(error));
    } finally {
      loaded = true;
    }
  }

  const chosen = () => (selected >= 0 ? rows[selected] : null);

  // -- Opening a row (2.5; session_load.go:82-100) --

  async function open(row) {
    if (!row || mode !== "normal") return;
    if (inProject(row, conn.project.path || conn.project.cwd)) {
      closeDialog(ID);
      if (isClosed(row.ID)) await reopen(row.ID);
      await openChat(row.ID);
      return;
    }
    if (windowBusy()) {
      notify("warn", "Agent is busy, please wait before switching folders...");
      return;
    }
    let isFolder = false;
    try {
      const info = await file_info(row.Directory, "/");
      isFolder = info?.exists === true && info?.is_dir === true;
    } catch {
      isFolder = false;
    }
    if (!isFolder) {
      notify("warn", `That chat's folder is no longer available: ${row.Directory}`);
      return;
    }
    closeDialog(ID);
    try {
      await openInFolder(row.Directory, row.ID);
    } catch (error) {
      notify("error", error?.message ?? String(error));
    }
  }

  // -- Rename and delete (sessions.go:171-198, :420-477) --

  function startRename() {
    const row = chosen();
    if (!row) return;
    renameText = row.Title ?? "";
    enter("rename", row);
  }

  function enter(next, row) {
    target = row.ID;
    lockedAt = selected;
    mode = next;
  }

  function leave() {
    mode = "normal";
    target = null;
  }

  const targetRow = () => rows.find((row) => row.ID === target) ?? null;

  // A click during rename or delete does not change the row (2.5).
  $effect(() => {
    if (mode !== "normal" && selected !== lockedAt) {
      const at = rows.findIndex((row) => row.ID === target);
      if (at >= 0) selected = lockedAt = at;
    }
  });

  async function saveRename() {
    const row = targetRow();
    const title = renameText.trim();
    leave();
    if (!row || title === "" || title === row.Title) return;
    const at = rows.indexOf(row);
    rows[at] = { ...row, Title: title };
    try {
      await api.changeSavedSession({ DiscardEmpty: false, Session: { ...row, Title: title }, Remove: false });
    } catch (error) {
      notify("error", error?.message ?? String(error));
      read();
    }
  }

  function startDelete() {
    const row = chosen();
    if (!row) return;
    if (busy(row.ID)) {
      notify("warn", BUSY);
      return;
    }
    enter("delete", row);
  }

  async function confirmDelete() {
    const row = targetRow();
    leave();
    if (!row) return;
    const at = rows.indexOf(row);
    rows = rows.filter((r) => r !== row);
    selected = rows.length === 0 ? -1 : Math.min(at, rows.length - 1);
    try {
      await api.changeSavedSession({ DiscardEmpty: false, Session: row, Remove: true });
    } catch (error) {
      notify("error", error?.message ?? String(error));
      read();
    }
  }

  // -- Keys (sessions.go:160-235) --

  function onKey(event, combo) {
    if (mode === "delete") {
      if (combo === "y" || combo === "Shift+Y" || combo === "Enter") confirmDelete();
      else if (combo === "n" || combo === "Shift+N" || combo === "Escape" || combo === "Alt+Escape") leave();
      return true;
    }
    if (mode === "rename") {
      if (combo === "Enter") {
        saveRename();
        return true;
      }
      if (combo === "Escape" || combo === "Alt+Escape") {
        leave();
        return true;
      }
      // The row's field keeps every other key; the list does not move.
      return combo === "ArrowUp" || combo === "ArrowDown" || combo === "Tab" || combo === "Shift+Tab";
    }
    switch (combo) {
      case "Ctrl+P":
        list?.move(-1);
        return true;
      case "Ctrl+N":
        list?.move(1);
        return true;
      case "Enter":
      case "Tab":
      case "Ctrl+Y":
        open(chosen());
        return true;
      case "Ctrl+R":
        startRename();
        return true;
      case "Ctrl+X":
        startDelete();
        return true;
      default:
        return false;
    }
  }

  // The row's field takes the focus while renaming; the filter gets it back.
  $effect(() => {
    const now = mode;
    tick().then(() => {
      const sheet = document.querySelector(`[data-sheet="${ID}"]`);
      const target = now === "rename" ? sheet?.querySelector(".rename input") : sheet?.querySelector("input");
      target?.focus();
      if (now === "rename") target?.select();
    });
  });

  // A chat renamed or deleted elsewhere changes its row in place (2.5).
  onMount(() => {
    read();
    return onEvent("session", (kind, record) => {
      const at = rows.findIndex((row) => row.ID === record?.id);
      if (at < 0) return;
      if (kind === "deleted") {
        // The chat being renamed or deleted went away elsewhere: the question ends.
        if (record.id === target) leave();
        const keep = rows[selected]?.ID;
        rows = rows.filter((_, i) => i !== at);
        const again = rows.findIndex((row) => row.ID === keep);
        selected = again >= 0 ? again : rows.length === 0 ? -1 : Math.min(at, rows.length - 1);
        lockedAt = selected;
      } else if (kind === "updated" && typeof record.title === "string") {
        rows[at] = { ...rows[at], Title: record.title, UpdatedAt: record.updated_at ?? rows[at].UpdatedAt };
      }
    });
  });
</script>

<Sheet id={ID} title="Chats" width="standard" {help} {onKey} controls={mode === "normal" && selected >= 0 ? controls : null}>
  <Field bind:value={query} icon="search" placeholder="Search chats" label="Filter chats" />
  {#if mode === "rename"}
    <p class="ask">Rename this chat?</p>
    <div class="actions">
      <Buttons buttons={[{ id: "save", label: "Rename" }, { id: "cancel", label: "Cancel" }]} selected="save" onPress={(id) => (id === "save" ? saveRename() : leave())} />
    </div>
  {:else if mode === "delete"}
    <p class="ask strong">Delete this chat?</p>
    <div class="actions">
      <Buttons buttons={[{ id: "delete", label: "Delete", destructive: true }, { id: "cancel", label: "Cancel" }]} selected="cancel" onPress={(id) => (id === "delete" ? confirmDelete() : leave())} />
    </div>
  {/if}
  {#if !loaded}
    <p class="state">Loading chats…</p>
  {:else if failed}
    <p class="state">Couldn't read the saved chats. <button class="retry" type="button" onclick={read}>Try again</button></p>
  {:else if rows.length === 0}
    <p class="state">No saved chats yet</p>
  {/if}
  {#if loaded && rows.length > 0}
    <FilterList
      bind:this={list}
      items={rows}
      filterText={(row) => row.Title ?? ""}
      groups={(row) => (recent.has(row.ID) ? "Recent" : folderLabel(conn.project.path || conn.project.cwd))}
      info={(row) => infoText(row, leftList.home, now)}
      infoBudget={35}
      {query}
      bind:selected
      onChoose={(row) => open(row)}
      label="Sessions"
      row={mode === "rename" ? renameRow : null}
    />
  {/if}
</Sheet>

<!-- Rename and Delete to click, beside their keys (owner, 2026-10-20). -->
{#snippet controls()}
  <button type="button" onclick={startRename}>Rename</button>
  <button type="button" onclick={startDelete}>Delete</button>
{/snippet}

{#snippet renameRow(item, index, state)}
  {#if state.selected}
    <div class="rename">
      <Field bind:value={renameText} prompt="> " label="New title" />
    </div>
  {:else}
    <span class="plain">{item.Title}</span>
  {/if}
{/snippet}

<style>
  .actions {
    margin-top: calc(12 * var(--px));
  }
  /* Loading, empty and failed each say so (owner, 2026-10-20). */
  .state {
    margin: calc(20 * var(--px)) 0 0;
    font: var(--t-body-small);
    color: var(--slate);
  }
  .retry {
    font: inherit;
    color: var(--link);
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }
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
