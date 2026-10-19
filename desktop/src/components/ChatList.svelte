<script>
  // The left column: the wordmark, New chat, the search, and the project's
  // chats by day, newest first (sessions.order). The open chat sits on a band
  // with a vermilion edge; a row says what its chat is doing (LIST-012) in the
  // time's place. The search filters by title (Ctrl+K focuses it). Phase 4
  // hangs branches under their chats (listmodel.js is ready for it) and adds
  // the closed count above the end rule. The header keeps its size and the
  // rows scroll under it, so nothing is squeezed in a short window.
  import { onMount } from "svelte";
  import { buildRows } from "../lib/listmodel.js";
  import { probeSection } from "../lib/probe.js";
  import { isClosed } from "../lib/stores/closed.svelte.js";
  import { focusField } from "../lib/stores/composer.svelte.js";
  import { busy, sessions } from "../lib/stores/sessions.svelte.js";
  import { setFocus } from "../lib/stores/ui.svelte.js";
  import { ws } from "../lib/stores/workspace.svelte.js";
  import Icon from "./Icon.svelte";
  import ChatRow from "./list/ChatRow.svelte";
  import { setSearchField } from "./list/listfocus.js";
  import ListFooter from "./list/ListFooter.svelte";
  import { startNewChat } from "./list/newchat.js";
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

  // Day groups for drawing: each day's label and the rows under it.
  const groups = $derived.by(() => {
    const out = [];
    for (const item of built.items) {
      if (item.kind === "day") out.push({ key: `${out.length}:${item.label}`, label: item.label, rows: [] });
      else if (out.length) out[out.length - 1].rows.push(item);
    }
    return out;
  });

  const searchHint = $derived(start.platform === "macos" ? "⌘K" : "Ctrl K");

  // The list's own keys while it has the focus: ↑ ↓ move between the rows
  // (↓ from the search goes to the first), Enter in the search opens the
  // first match, Esc clears the search, then goes back to the composer.
  function rowButtons() {
    return [...(nav?.querySelectorAll(".scroll .row") ?? [])];
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
      buttons[0]?.click();
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
      rows: built.rows.map((r) => ({ id: r.id, title: r.title, depth: r.depth, selected: r.selected, closed: r.closed, state: r.state, state_kind: r.stateKind, time: r.time, agent: r.agent })),
      closed_count: built.closedCount,
      search_hint: searchHint,
      query,
      empty: built.empty,
      days: groups.map((g) => g.label),
    }),
    () => ({
      rows: built.rows.map((r) => {
        const li = nav?.querySelector(`.scroll [data-id="${CSS.escape(r.id)}"]`);
        return {
          id: r.id,
          rect: rectOf(li),
          title: rectOf(li?.querySelector(".title")),
          title_text: textRect(li?.querySelector(".title")),
          time: rectOf(li?.querySelector(".time")),
          state: rectOf(li?.querySelector(".state")),
        };
      }),
      column: rectOf(nav),
      new_chat: rectOf(nav?.querySelector(".new-chat")),
      search: rectOf(nav?.querySelector(".search")),
      scroll: rectOf(nav?.querySelector(".scroll")),
      empty: rectOf(nav?.querySelector(".empty")),
      align: listMarks(),
    }),
  );

  // The text's own box, not its element's (a cut title ends at its "…").
  function textRect(el) {
    if (!el) return null;
    const range = document.createRange();
    range.selectNodeContents(el);
    const r = range.getBoundingClientRect();
    return r.width ? { x: r.x, y: r.y, w: Math.min(r.width, el.clientWidth), h: r.height } : null;
  }
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<nav class="chats" aria-label="Chats" bind:this={nav} onfocusin={() => setFocus("list")} onkeydown={onKey}>
  <h1 class="wordmark"><span class="crush">Crush</span> <span class="ai">AI</span></h1>

  <button class="new-chat" type="button" onclick={startNewChat}>
    <Icon name="plus" />
    <span class="label">New chat</span>
  </button>

  <label class="search">
    <Icon name="search" />
    <input type="search" placeholder="Search chats…" aria-label="Search chats" bind:this={field} bind:value={query} />
    <kbd>{searchHint}</kbd>
  </label>

  <Rule marks="both" class="under-search" />

  <div class="scroll">
    {#if built.empty}
      <p class="empty">{built.empty}</p>
    {/if}
    {#each groups as group (group.key)}
      <section class="group" aria-label={group.label}>
        <h2 class="day">{group.label}</h2>
        <ul class="list">
          {#each group.rows as row (row.id)}
            <ChatRow {row} />
          {/each}
        </ul>
      </section>
    {/each}

    <ListFooter count={built.closedCount} />
    {#if !built.empty}<Rule marks="both" class="end-of-list" />{/if}
  </div>
</nav>

<style>
  /* Vertical positions follow the approved picture at 12/12 (column top = 0). */
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
  .new-chat,
  .search,
  .chats :global(.rule.under-search) {
    flex: none;
  }

  .wordmark {
    margin: calc(27 * var(--px)) 0 0 calc(20 * var(--px));
    font: 700 calc(32 * var(--px)) / calc(40 * var(--px)) var(--face-brand);
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

  .new-chat {
    display: flex;
    align-items: center;
    box-sizing: border-box;
    height: calc(52 * var(--px));
    margin: calc(22 * var(--px)) calc(20 * var(--px)) 0;
    padding-left: calc(16 * var(--px));
    border: 1px solid var(--rule-control);
    background: var(--paper-raised);
    color: var(--ink);
  }
  .new-chat .label {
    /* Spectral's capitals sit high in their line box; one picture pixel down
       centres them on the plus (measured in the real window, WebKitGTK). */
    position: relative;
    top: calc(1 * var(--px));
    margin-left: calc(13 * var(--px));
    font: var(--t-button);
  }
  .new-chat:hover .label {
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
    color: var(--ink);
    cursor: text;
  }
  .search input {
    flex: 1;
    min-width: 0;
    height: calc(26 * var(--px));
    margin-left: calc(12 * var(--px));
    padding: 0;
    border: 0;
    outline: 0;
    background: none;
    font: var(--t-field);
    appearance: none;
  }
  .search input::placeholder {
    color: var(--slate);
    opacity: 1;
  }
  .search input::-webkit-search-cancel-button {
    appearance: none;
  }
  .search:focus-within {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }
  .search kbd {
    flex: none;
    margin-left: calc(8 * var(--px));
    font: var(--t-small);
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
</style>
