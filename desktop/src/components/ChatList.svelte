<script>
  // The left column: the wordmark, New chat, the search, and the project's
  // chats by day, newest first (sessions.order). The open chat sits on a band
  // with a vermilion edge; a row says what its chat is doing (LIST-012) in the
  // time's place. The search filters by title (Ctrl+K focuses it). Phase 4
  // hangs branches under their chats (listmodel.js is ready for it) and adds
  // the closed count above the end rule. The header keeps its size and the
  // rows scroll under it, so nothing is squeezed in a short window.
  //
  // Phases 5 and 6 (desktop-design-5-6 7.1, 2.5): registered list sections
  // draw under the search's rule ("header"), after the chat rows ("rows") and
  // above the end rule ("footer"). The search hands off to the Sessions
  // sheet: a typed character, Enter or a click in it opens the sheet with the
  // box's text in its filter and empties the box. While no Sessions sheet is
  // registered (SES), the box filters the list as before.
  import { onMount } from "svelte";
  import { hasDialog, openDialog } from "../lib/dialogs.js";
  import { slidePage } from "../lib/motion.js";
  import { settingsOpen } from "../lib/settings/page.svelte.js";
  import { listSections } from "../lib/listsections.js";
  import { buildRows } from "../lib/listmodel.js";
  import { probeSection } from "../lib/probe.js";
  import { folderGroups, folderName } from "../lib/leftlist/folders.js";
  import { isClosed, isClosedIn } from "../lib/stores/closed.svelte.js";
  import { conn, inFolder } from "../lib/stores/connection.svelte.js";
  import { folders } from "../lib/stores/folders.svelte.js";
  import { branchesOf, parentOf } from "../lib/stores/links.svelte.js";
  import { focusField } from "../lib/stores/composer.svelte.js";
  import { busy, sessions } from "../lib/stores/sessions.svelte.js";
  import { setFocus } from "../lib/stores/ui.svelte.js";
  import { ws } from "../lib/stores/workspace.svelte.js";
  import Icon from "./Icon.svelte";
  import ChatRow from "./list/ChatRow.svelte";
  import { searchFocused, setSearchField } from "./list/listfocus.js";
  import ListFooter from "./list/ListFooter.svelte";
  import { newChat, newTerminal } from "../lib/leftlist/store.svelte.js";
  import { listRoom } from "./panel/panel.svelte.js";
  import { listMarks } from "./start/marks.js";
  import { start } from "./start/start.svelte.js";
  import { providerName } from "./start/startmodel.js";
  import Rule from "./Rule.svelte";

  let query = $state("");
  let field = $state(null);
  let nav = $state(null);

  // The day groups and times move on as the clock does.
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

  // The row's agent line names the chat's provider (configured name, else the
  // catalog's, else its id).
  const agentOf = (s) => {
    const id = s?.context_budget?.provider;
    return id ? providerName(id, start.providerNames, ws.providers) : "";
  };

  const built = $derived(
    buildRows(
      sessions.order.map((id) => sessions.byId[id]),
      {
        parentOf,
        isClosed,
        selectedId: sessions.currentId,
        busy,
        query,
        now,
        timeOf: (s) => Math.max(s?.updated_at ?? 0, sessions.activity[s?.id] ?? 0),
        agentOf,
      },
    ),
  );

  // Only the newest chats show (owner, 2026-10-19), with their branches and
  // always the open chat; "All chats" opens the Sessions sheet for the rest.
  // A search shows every match.
  const SHOWN_CHATS = 6;

  // The groups for drawing (owner, 2026-10-20, B): this folder's chats under
  // its name, then every other folder's chats not closed, under theirs.
  const view = $derived.by(() => {
    const here = { key: "folder:here", label: conn.project.name || folderName(conn.project.cwd), path: conn.project.path || conn.project.cwd, rows: [] };
    let top = 0;
    let hidden = 0;
    let keep = true;
    for (const item of built.items) {
      if (item.kind === "day") continue;
      if (item.depth === 0) {
        top++;
        keep = query !== "" || top <= SHOWN_CHATS || item.selected;
        if (!keep) hidden++;
      }
      if (keep || item.selected) here.rows.push(item);
    }
    const others = folderGroups(folders.catalog, {
      here: inFolder,
      isClosedIn,
      isBranch: (dataDir, id) => branchesOf(dataDir).includes(id),
      agentOf: (provider) => (provider ? providerName(provider, start.providerNames, ws.providers) : ""),
      now,
    });
    return { groups: [here, ...others].filter((g) => g.rows.length > 0), hidden };
  });
  const groups = $derived(view.groups);
  const empty = $derived(groups.length === 0 ? built.empty : "");

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
      rows: built.rows.map((r) => ({ id: r.id, title: r.title, depth: r.depth, selected: r.selected, state: r.state, state_kind: r.stateKind, time: r.time, agent: r.agent })),
      closed_count: built.closedCount,
      search_hint: searchHint,
      search_focused: searchFocused(),
      query,
      empty: built.empty,
      days: groups.map((g) => g.label),
      folders: groups.map((g) => ({ label: g.label, path: g.path, rows: g.rows.map((r) => r.id) })),
    }),
    () => ({
      rows: built.rows.map((r) => {
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
    {#each groups as group (group.key)}
      <section class="group" aria-label={group.label} data-folder={group.path}>
        <h2 class="day" title={group.path}>{group.label}</h2>
        <ul class="list">
          {#each group.rows as row (row.id)}
            <ChatRow {row} />
          {/each}
        </ul>
      </section>
    {/each}

    {#each listSections("rows") as entry (entry.id)}
      <entry.component />
    {/each}

    {#if view.hidden > 0}
      <button class="all-chats" type="button" onclick={() => openDialog("sessions")}>All chats <span class="more">({view.hidden} more)</span></button>
    {/if}
    <ListFooter count={built.closedCount} />
    {#each listSections("footer") as entry (entry.id)}
      <entry.component />
    {/each}
    {#if !empty}<Rule marks="both" class="end-of-list" />{/if}
  </div>

  <!-- Settings without the Ctrl+P menu (owner, 2026-10-19). -->
  <button class="settings" type="button" onclick={() => openDialog("settings")}>
    <Icon name="gear" size={16} />
    <span class="label">Settings</span>
  </button>
</nav>

<style>
  /* Vertical positions follow the approved picture at 12/12 (column top = 0). */
  /* The list slides in from the left, back from Settings or the strip, and
     rolls away over the strip as it collapses (lib/motion.js; Settings'
     own list covers it going, so then it leaves at once). */
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

  /* "All chats (N more)" under the shown rows opens the Sessions sheet. */
  .all-chats {
    display: block;
    margin: calc(10 * var(--px)) calc(20 * var(--px)) calc(4 * var(--px));
    padding: 0;
    border: 0;
    background: none;
    font: var(--t-body-small);
    color: var(--ink);
    text-align: left;
    cursor: pointer;
  }
  .all-chats .more {
    color: var(--slate);
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
  .all-chats:hover,
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

  .day,
  .empty {
    margin: calc(14 * var(--px)) 0 0 calc(20 * var(--px));
  }
  .day {
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
  }
  .empty {
    font: var(--t-small);
    color: var(--slate);
  }

  .list {
    margin-top: calc(13 * var(--px));
  }

  .chats :global(.rule.end-of-list) {
    margin-top: 0;
  }
  /* Right under the open chat's band the list's end rule would be a second
     line beneath its own; it goes clear there (owner, 2026-10-20). */
  .chats :global(.group:has(.chat.selected:last-child) + .rule.end-of-list) {
    visibility: hidden;
  }
</style>
