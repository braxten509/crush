<script>
  // The left column: the wordmark, New chat and New terminal, the search,
  // and the open chats shared with the terminal's session list (owner,
  // 2026-10-20: "the two should be in sync"), with this window's terminal
  // tabs after them (owner, 2026-11-03), under their folders (owner,
  // 2026-10-21, pick A: the path in quiet italic, a plain "+" on its right
  // for a new chat there). A folder stays after its last chat closes; then
  // an × beside the "+" takes it off (lib/stores/listfolders.svelte.js). The open chat sits on a band with a vermilion edge; a row
  // of this folder says what its chat is doing (LIST-012) in the time's
  // place, and its open branches hang under it. Older chats are found with
  // the search (the Sessions sheet); there is no "All chats" line. The header
  // keeps its size and the rows scroll under it.
  //
  // Phases 5 and 6 (desktop-design-5-6 7.1, 2.5): registered list sections
  // draw under the search's rule ("header"), after the chat rows ("rows") and
  // last ("footer"); no rule ends the list (owner, 2026-10-24). The search hands off to the Sessions
  // sheet: a typed character, Enter or a click in it opens the sheet with the
  // box's text in its filter.
  import { onMount } from "svelte";
  import { hasDialog, openDialog } from "../lib/dialogs.js";
  import { slidePage, unfold } from "../lib/motion.js";
  import { settingsOpen } from "../lib/settings/page.svelte.js";
  import { listSections } from "../lib/listsections.js";
  import { probeSection } from "../lib/probe.js";
  import { chatGroups } from "../lib/leftlist/numbers.js";
  import { dropFolder } from "../lib/stores/listfolders.svelte.js";
  import { focusField } from "../lib/stores/composer.svelte.js";
  import { sessions } from "../lib/stores/sessions.svelte.js";
  import { setFocus } from "../lib/stores/ui.svelte.js";
  import { bindKey } from "../lib/keys.js";
  import Icon from "./Icon.svelte";
  import ChatRow from "./list/ChatRow.svelte";
  import TerminalRows from "./leftlist/TerminalRows.svelte";
  import TodoRows from "./leftlist/TodoRows.svelte";
  import { startTodo, todosIn } from "../lib/stores/listtodos.svelte.js";
  import { searchFocused, setSearchField } from "./list/listfocus.js";
  import { leftList, newChat, newTerminal, startIn } from "../lib/leftlist/store.svelte.js";
  import { clean } from "../lib/leftlist/picker.js";
  import { listRoom } from "./panel/panel.svelte.js";
  import { listMarks } from "./start/marks.js";
  import { start } from "./start/start.svelte.js";
  import Rule from "./Rule.svelte";

  let query = $state("");
  let field = $state(null);
  let nav = $state(null);

  // The times move on as the clock does.
  let now = $state(Date.now());
  onMount(() => {
    const timer = setInterval(() => (now = Date.now()), 60_000);
    return () => clearInterval(timer);
  });

  $effect(() => {
    setSearchField(field);
    return () => setSearchField(null);
  });

  // The open chat's row stays in view when the chat changes.
  $effect(() => {
    const id = sessions.currentId;
    if (!id || !nav) return;
    queueMicrotask(() => nav?.querySelector(`.scroll [data-id="${CSS.escape(id)}"]`)?.scrollIntoView({ block: "nearest" }));
  });

  // A folder's "+" opens a small menu right under it, its right edge on the
  // "+" (owner, 2026-11-05): New Chat, Open Chat (the chat search showing only
  // that folder's chats, owner 2026-11-09), New Terminal, New Todo, all in that folder.
  // ↑/↓ move, Enter picks, Esc, Tab, a press outside, a scroll or the
  // window losing focus closes it.
  let plus = $state(null); // {dir, button} while open
  let plusBox = $state(null);
  let plusPlace = $state(null); // {x, y} once measured

  function togglePlus(event, dir) {
    const button = event.currentTarget;
    plus = plus?.dir === dir ? null : { dir, button };
  }

  function closePlus({ refocus = false } = {}) {
    const button = plus?.button;
    plus = null;
    if (refocus && button?.isConnected) button.focus({ preventScroll: true });
  }

  function pickPlus(what) {
    const dir = plus?.dir;
    closePlus();
    if (!dir) return;
    // No project's todos stay under "" (its dir is home for chats and terminals).
    if (what === "todo") startTodo(dir === home ? "" : dir);
    else if (what === "open") openDialog("sessions", { folder: dir });
    else startIn(what, dir);
  }

  $effect(() => {
    if (!plus || !plusBox) {
      plusPlace = null;
      return;
    }
    const r = plus.button.getBoundingClientRect();
    const gap = 2;
    const w = plusBox.offsetWidth;
    const h = plusBox.offsetHeight;
    const below = r.bottom + gap;
    plusPlace = {
      x: Math.max(0, r.right - w),
      y: below + h > window.innerHeight ? Math.max(0, r.top - gap - h) : below,
    };
    plusBox.querySelector("button")?.focus({ preventScroll: true });
    const outside = (event) => {
      if (!plusBox?.contains(event.target) && !plus?.button.contains(event.target)) closePlus();
    };
    const away = () => closePlus();
    window.addEventListener("mousedown", outside, true);
    window.addEventListener("wheel", away, { capture: true, passive: true });
    window.addEventListener("blur", away);
    window.addEventListener("resize", away);
    // Through the app's keys (lib/keys.js), which run before the page's own:
    // the "overlay" scope is asked first while no sheet is open.
    const unbind = [
      bindKey("overlay", "ArrowDown", () => plusStep(1)),
      bindKey("overlay", "ArrowUp", () => plusStep(-1)),
      bindKey("overlay", "Enter", plusEnter),
      bindKey("overlay", "Escape", () => (closePlus({ refocus: true }), true)),
      bindKey("overlay", "Tab", () => (closePlus(), false)),
      bindKey("overlay", "Shift+Tab", () => (closePlus(), false)),
    ];
    return () => {
      for (const fn of unbind) fn();
      window.removeEventListener("mousedown", outside, true);
      window.removeEventListener("wheel", away, { capture: true });
      window.removeEventListener("blur", away);
      window.removeEventListener("resize", away);
    };
  });

  function plusItems() {
    return [...(plusBox?.querySelectorAll("button") ?? [])];
  }

  function plusStep(dir) {
    const items = plusItems();
    const at = items.indexOf(document.activeElement);
    items[(at + dir + items.length) % items.length]?.focus({ preventScroll: true });
    return true;
  }

  function plusEnter() {
    const item = plusItems().find((b) => b === document.activeElement);
    if (!item) return false;
    item.click();
    return true;
  }

  // No project is the home folder (owner, 2026-11-09): what runs in ~ shows
  // under No project, never as its own "~" folder, and its "+" starts there.
  const home = $derived(leftList.home ? clean(leftList.home) : "");
  const allGroups = $derived(chatGroups(now));
  const homeGroup = $derived(home ? allGroups.find((group) => clean(group.dir) === home) : null);
  const groups = $derived(allGroups.filter((group) => group !== homeGroup));
  // Drawn order: the folders, then No project.
  const rows = $derived([...groups, ...(homeGroup ? [homeGroup] : [])].flatMap((group) => group.rows));
  const empty = $derived(allGroups.length === 0 ? "No open chats" : "");
  // Every heading, for a todo's "Move to" (TodoRows.svelte).
  const folderTargets = $derived(groups.map((group) => ({ dir: group.dir, label: group.label })));

  const searchHint = $derived(start.platform === "macos" ? "⌘K" : "Ctrl K");

  // The list's own keys while it has the focus: ↑ ↓ move between the rows
  // (↓ from the search goes to the first), Enter in the search opens the
  // first match, Esc clears the search, then goes back to the composer.
  function rowButtons() {
    return [...(nav?.querySelectorAll(".scroll .row") ?? [])];
  }

  // True when the Sessions sheet took the box's text.
  function handOff() {
    openDialog("sessions", { query });
    if (!hasDialog("sessions")) return false;
    query = "";
    return true;
  }

  function onSearchKey(event) {
    if (event.ctrlKey || event.altKey || event.metaKey || event.key.length !== 1 || event.key === " ") return;
    event.preventDefault();
    query = event.key;
    handOff();
  }

  function onKey(event) {
    if (event.ctrlKey || event.altKey || event.metaKey) return;
    const buttons = rowButtons();
    const at = buttons.indexOf(document.activeElement);
    const inSearch = document.activeElement === field;
    if (event.key === "ArrowDown") {
      const next = inSearch ? buttons[0] : buttons[at + 1];
      if (next) next.focus();
      if (next || at >= 0) event.preventDefault();
    } else if (event.key === "ArrowUp") {
      if (at > 0) buttons[at - 1].focus();
      else if (at === 0) field?.focus();
      if (at >= 0) event.preventDefault();
    } else if (event.key === "Enter" && inSearch) {
      if (!handOff()) buttons[0]?.click();
      event.preventDefault();
    } else if (event.key === "Escape") {
      if (inSearch && query) query = "";
      else focusField();
      event.preventDefault();
    }
  }

  function rectOf(el) {
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return { x: r.x, y: r.y, w: r.width, h: r.height };
  }

  // 1.9: the rows as drawn. state is the words the row shows ("" for none).
  probeSection(
    "list",
    () => ({
      rows: rows.map((r) => ({ id: r.id, title: r.title, depth: r.depth, selected: r.selected, state: r.state, state_kind: r.stateKind, time: r.time, agent: r.agent, folder: r.agent })),
      search_hint: searchHint,
      search_focused: searchFocused(),
      query,
      empty,
      days: [],
    }),
    () => ({
      rows: rows.map((r) => {
        const li = nav?.querySelector(`.scroll [data-id="${CSS.escape(r.id)}"]`);
        return {
          id: r.id,
          rect: rectOf(li),
          band: rectOf(li?.querySelector(".row")), // the button: its background is the open chat's band
          title: rectOf(li?.querySelector(".title")),
          title_text: textRect(li?.querySelector(".title")),
          time: rectOf(li?.querySelector(".time")),
          state: rectOf(li?.querySelector(".state")),
        };
      }),
      column: rectOf(nav),
      new_chat: rectOf(nav?.querySelector(".new-chat")),
      new_terminal: rectOf(nav?.querySelector(".new-terminal")),
      search: rectOf(nav?.querySelector(".search")),
      scroll: rectOf(nav?.querySelector(".scroll")),
      empty: rectOf(nav?.querySelector(".empty")),
      align: listMarks(),
    }),
  );

  // The text as drawn, not its element's box: a cut title ends at its "…",
  // the content box's right edge (its right padding stays empty).
  function textRect(el) {
    if (!el) return null;
    const range = document.createRange();
    range.selectNodeContents(el);
    const r = range.getBoundingClientRect();
    if (!r.width) return null;
    const box = el.getBoundingClientRect();
    const contentRight = box.left + el.clientLeft + el.clientWidth - (parseFloat(getComputedStyle(el).paddingRight) || 0);
    return { x: r.x, y: r.y, w: Math.min(r.right, contentRight) - r.x, h: r.height };
  }

  // The list width New chat and New terminal need with their words whole
  // (owner, 2026-10-20): each half's content with as much room after the
  // words as before the icon (owner, 2026-10-20: the right side was
  // tighter), twice, plus the gap and what the list keeps around the row.
  // Measured again on a zoom change.
  let newRow = $state(null);
  $effect(() => {
    if (!newRow) return;
    const measure = () => {
      const halves = [...newRow.children];
      if (halves.length === 0) return;
      const need = (b) => {
        const style = getComputedStyle(b);
        const parts = [...b.children].reduce((sum, c) => sum + c.getBoundingClientRect().width + parseFloat(getComputedStyle(c).marginLeft), 0);
        return parts + 2 * parseFloat(style.paddingLeft) + 2 * parseFloat(style.borderLeftWidth) + 2;
      };
      const gap = parseFloat(getComputedStyle(newRow).columnGap) || 0;
      const row = Math.max(...halves.map(need)) * halves.length + gap * (halves.length - 1);
      const column = newRow.closest(".chats-column") ?? newRow.parentElement;
      const around = column.getBoundingClientRect().width - newRow.getBoundingClientRect().width;
      const unit = parseFloat(getComputedStyle(document.documentElement).fontSize) / 16 || 1;
      listRoom.width = Math.ceil((row + around) / unit);
    };
    measure();
    document.fonts?.ready.then(measure);
    const observer = new ResizeObserver(measure);
    observer.observe(newRow);
    return () => observer.disconnect();
  });
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<nav class="chats" aria-label="Chats" bind:this={nav} transition:slidePage|global={{ dir: -1, overlay: true, off: settingsOpen() }} onfocusin={() => setFocus("list")} onkeydown={onKey}>
  <h1 class="wordmark"><span class="crush">Crush</span> <span class="ai">AI</span></h1>

  <!-- New chat and New terminal, two equal halves of one row (owner, 2026-10-20, A). -->
  <div class="new-row" bind:this={newRow}>
    <button class="new-chat" type="button" onclick={newChat}>
      <Icon name="plus" />
      <span class="label">New chat</span>
    </button>
    <button class="new-chat new-terminal" type="button" title="New terminal (Alt+T)" onclick={newTerminal}>
      <Icon name="terminal" />
      <span class="label">New terminal</span>
    </button>
  </div>

  <!-- A button, not a box (owner, 2026-10-20): it opens the Sessions sheet,
       where the search text goes; a letter typed on it starts that search. -->
  <button class="search" type="button" aria-label="Search chats" bind:this={field} onclick={handOff} onkeydown={onSearchKey}>
    <Icon name="search" />
    <span class="placeholder">Search chats…</span>
    <kbd>{searchHint}</kbd>
  </button>

  <Rule marks="both" class="under-search" />

  {#each listSections("header") as entry (entry.id)}
    <entry.component />
  {/each}

  <div class="scroll">
    {#if empty}
      <p class="empty">{empty}</p>
    {/if}
    {#each groups as group (group.dir)}
      <section class="group" aria-label={group.label} data-folder={group.dir}>
        <div class="folder">
          <Icon name="folder-filled" size={12} class="folder-icon" />
          <span class="folder-name" title={group.dir}>{group.label}</span>
          {#if group.rows.length === 0 && group.terminals.length === 0 && todosIn(group.dir).length === 0}
            <button class="folder-button folder-drop" type="button" data-folder-drop={group.dir} aria-label="Remove {group.label}" title="Remove folder" onclick={() => dropFolder(group.dir)}>
              <Icon name="close" size={12} frame={30} />
            </button>
          {/if}
          <button class="folder-button folder-new" class:open={plus?.dir === group.dir} type="button" aria-label="New in {group.label}" aria-haspopup="menu" aria-expanded={plus?.dir === group.dir} onclick={(event) => togglePlus(event, group.dir)}>
            <Icon name="plus" frame={30} />
          </button>
        </div>
        {#if group.rows.length > 0}
          <ul class="list">
            {#each group.rows as row (row.id)}
              <ChatRow {row} />
            {/each}
          </ul>
        {:else if group.terminals.length === 0 && todosIn(group.dir).length === 0}
          <p class="folder-empty">No open chats</p>
        {/if}
        {#if group.terminals.length > 0}
          <TerminalRows tabs={group.terminals} />
        {/if}
        <TodoRows dir={group.dir} folders={folderTargets} />
      </section>
    {/each}

    <!-- No project, always last of the folders: todos with no folder and the
         chats and terminals of the home folder (owner, 2026-11-09). Its "+"
         is a folder's menu on ~ (no home known yet: New Todo at once). -->
    <section class="group" aria-label="No project" data-folder="">
      <div class="folder">
        <Icon name="folder-filled" size={12} class="folder-icon" />
        <span class="folder-name" title={leftList.home}>No project</span>
        {#if home}
          <button class="folder-button folder-new" class:open={plus?.dir === home} type="button" aria-label="New in No project" aria-haspopup="menu" aria-expanded={plus?.dir === home} onclick={(event) => togglePlus(event, home)}>
            <Icon name="plus" frame={30} />
          </button>
        {:else}
          <button class="folder-button folder-new" type="button" aria-label="New todo with no project" title="New todo" onclick={() => startTodo("")}>
            <Icon name="plus" frame={30} />
          </button>
        {/if}
      </div>
      {#if homeGroup?.rows.length > 0}
        <ul class="list">
          {#each homeGroup.rows as row (row.id)}
            <ChatRow {row} />
          {/each}
        </ul>
      {/if}
      {#if homeGroup?.terminals.length > 0}
        <TerminalRows tabs={homeGroup.terminals} />
      {/if}
      <TodoRows dir="" folders={folderTargets} />
      {#if homeGroup && todosIn(homeGroup.dir).length > 0}
        <TodoRows dir={homeGroup.dir} folders={folderTargets} />
      {/if}
    </section>

    {#each listSections("rows") as entry (entry.id)}
      <entry.component />
    {/each}

    {#each listSections("footer") as entry (entry.id)}
      <entry.component />
    {/each}
  </div>

  {#if plus}
    <!-- svelte-ignore a11y_interactive_supports_focus -->
    <div
      class="plus-menu"
      role="menu"
      aria-label="New in folder"
      data-plus-menu
      bind:this={plusBox}
      style:left="{plusPlace?.x ?? 0}px"
      style:top="{plusPlace?.y ?? 0}px"
      style:visibility={plusPlace ? null : "hidden"}
      transition:unfold={{ from: "top" }}
    >
      <button class="plus-entry" type="button" role="menuitem" onclick={() => pickPlus("session")}>New Chat</button>
      <button class="plus-entry" type="button" role="menuitem" onclick={() => pickPlus("open")}>Open Chat</button>
      <button class="plus-entry" type="button" role="menuitem" onclick={() => pickPlus("terminal")}>New Terminal</button>
      <button class="plus-entry" type="button" role="menuitem" onclick={() => pickPlus("todo")}>New Todo</button>
    </div>
  {/if}

  <!-- Settings without the Ctrl+P menu (owner, 2026-10-19). -->
  <button class="settings" type="button" data-testid="open-settings" onclick={() => openDialog("settings")}>
    <Icon name="gear" size={16} />
    <span class="label">Settings</span>
  </button>
</nav>

<style>
  /* Vertical positions follow the approved picture at 12/12 (column top = 0). */
  /* The list slides in from the left, back from Settings or the strip, and
     rolls away over the strip as it collapses (lib/motion.js; Settings'
     own list covers it going, so then it leaves at once). Both ways it keeps
     its open width while the column glides (App.svelte). */
  .chats {
    position: relative;
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    overflow: hidden;
  }
  /* The header keeps its size; only the rows give way (and scroll). */
  .wordmark,
  .new-row,
  .search,
  .chats :global(.rule.under-search) {
    flex: none;
  }

  .wordmark {
    margin: calc(27 * var(--px)) 0 0 calc(20 * var(--px));
    font: var(--t-wordmark);
    color: var(--ink);
    white-space: nowrap;
  }
  .wordmark .crush {
    text-transform: uppercase;
  }
  .wordmark .ai {
    margin-left: calc(14 * var(--px));
    font-weight: 400;
    color: var(--slate);
  }

  .new-row {
    display: flex;
    gap: calc(8 * var(--px));
    margin: calc(22 * var(--px)) calc(20 * var(--px)) 0;
  }
  .new-chat {
    flex: 1 1 0;
    min-width: 0;
    display: flex;
    align-items: center;
    box-sizing: border-box;
    height: calc(52 * var(--px));
    padding-left: calc(16 * var(--px));
    white-space: nowrap;
    border: 1px solid var(--rule-control);
    background: var(--paper-raised);
    color: var(--ink);
  }
  .new-chat .label {
    /* At the small button size the capitals already centre on the plus
       (measured in the real window, 2026-10-20); the old 1 px nudge is gone. */
    margin-left: calc(13 * var(--px));
    font: var(--t-button);
  }
  .new-chat:hover .label {
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }

  /* Settings sits under the scrolling rows, at the column's foot. */
  .settings {
    flex: none;
    display: flex;
    align-items: center;
    gap: calc(12 * var(--px));
    box-sizing: border-box;
    height: calc(52 * var(--px));
    margin: 0 calc(20 * var(--px));
    padding: 0;
    border: 0;
    border-top: 1px solid var(--rule-faint);
    background: none;
    font: var(--t-body-small);
    color: var(--ink);
    cursor: pointer;
  }
  .settings:hover .label {
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }

  .search {
    display: flex;
    align-items: center;
    box-sizing: border-box;
    height: calc(50 * var(--px));
    margin: calc(17 * var(--px)) calc(20 * var(--px)) 0;
    padding: 0 calc(12 * var(--px)) 0 calc(11 * var(--px));
    border: 1px solid var(--rule-control);
    background: none;
    color: var(--ink);
    cursor: pointer;
  }
  .search .placeholder {
    flex: 1;
    min-width: 0;
    margin-left: calc(12 * var(--px));
    font: var(--t-field);
    line-height: calc(26 * var(--px));
    color: var(--slate);
    text-align: left;
  }
  .search:hover {
    background: var(--control-hover);
  }
  .search:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }
  .search kbd {
    flex: none;
    margin-left: calc(8 * var(--px));
    font: var(--t-mono);
    color: var(--slate);
    white-space: nowrap;
  }

  .chats :global(.rule.under-search) {
    margin-top: calc(25 * var(--px));
  }

  .scroll {
    flex: 1 1 auto;
    min-height: 0;
    overflow-x: hidden;
    overflow-y: auto;
    scrollbar-width: none;
  }

  .empty {
    margin: calc(14 * var(--px)) 0 0 calc(20 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }


  /* A folder: its path in quiet italic, the "+" (and an empty folder's ×)
     on the right, plain marks with no box (owner, 2026-10-21). Kept tight
     (owner, 2026-10-22: the list took too much room), with a blank row
     between folders and 3 screen px between the line and its chats, so the "+" box
     and the open chat's band never touch (owner, 2026-10-22). */
  .group + .group {
    margin-top: calc(22 * var(--px));
  }
  .list {
    margin-top: 3px;
  }
  .folder {
    display: flex;
    align-items: center;
    gap: calc(4 * var(--px));
    height: calc(32 * var(--px));
    margin-top: calc(4 * var(--px));
    padding: 0 calc(12 * var(--px)) 0 calc(23 * var(--px));
  }
  .folder :global(.folder-icon) {
    flex: none;
    margin-right: calc(2 * var(--px));
    color: var(--slate);
  }
  .folder-name {
    flex: 1 1 auto;
    min-width: 0;
    font: var(--t-meta);
    font-style: italic;
    color: var(--slate);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .folder-button {
    flex: none;
    display: grid;
    place-items: center;
    width: calc(30 * var(--px));
    height: calc(30 * var(--px));
    padding: 0;
    border: 0;
    background: none;
    color: var(--ink);
    cursor: pointer;
  }
  .folder-button:hover {
    background: var(--control-hover);
  }
  .folder-button:focus-visible {
    outline: 1px solid var(--vermilion);
  }
  .folder-button.open {
    background: var(--control-hover);
  }
  /* The "+" menu: the right-click menu's paper and hairline (ContextMenu),
     sized to its words. */
  .plus-menu {
    position: fixed;
    z-index: 30;
    display: flex;
    flex-direction: column;
    box-sizing: border-box;
    padding: calc(6 * var(--px)) 0;
    background: var(--paper-raised);
    border: 1px solid var(--rule-control);
  }
  .plus-entry {
    box-sizing: border-box;
    min-height: calc(36 * var(--px));
    padding: calc(5 * var(--px)) calc(16 * var(--px));
    border: 0;
    background: none;
    font: var(--t-body-small);
    color: var(--ink);
    text-align: left;
    white-space: nowrap;
    cursor: default;
  }
  .plus-entry:hover,
  .plus-entry:focus-visible {
    background: var(--pick);
    outline: none;
  }
  /* The × shows while the folder's line is pointed at or holds the focus. */
  .folder-drop {
    color: var(--slate);
    opacity: 0;
  }
  .folder:hover .folder-drop,
  .folder:focus-within .folder-drop {
    opacity: 1;
  }
  .folder-empty {
    margin: 0 0 calc(4 * var(--px)) calc(23 * var(--px));
    font: var(--t-small);
    font-style: italic;
    color: var(--slate);
  }
</style>
