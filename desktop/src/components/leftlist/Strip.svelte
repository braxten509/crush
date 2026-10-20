<script>
  // The narrow strip (desktop-design-5-6 2.20, look 5.17; LIST-002, LIST-044;
  // the terminal's drawStrip, internal/ui/sessionhost/draw.go:471-500), shown
  // instead of the list while leftList.strip. Top to bottom: "›" (the whole
  // top row opens the list), "+" (a new chat in this folder), ">_"
  // (a new terminal in this folder), then one cell per row with its state mark and its
  // number (its Alt+number, at most two digits); the shown row's cell has the
  // vermilion edge, and a rule parts the chats from the terminal tabs. No
  // notice line and no server rows (servers.go:96-99). A terminal tab's cell
  // drags like its row (LIST-009). Mounting it starts the page-width watch.
  import { onMount } from "svelte";
  import { isShown, listRows, showRow, startDrag } from "../../lib/leftlist/numbers.js";
  import { MARKS, chatState } from "../../lib/leftlist/states.js";
  import { leftList, newChat, newTerminal, toggleList, watchWidth } from "../../lib/leftlist/store.svelte.js";
  import Icon from "../Icon.svelte";

  let nav = $state(null);
  let cells = $state(null);

  onMount(() => watchWidth(nav));

  const rows = $derived(
    listRows().map((row) => ({
      ...row,
      key: `${row.kind}:${row.id}`,
      state: row.kind === "terminal" ? "terminal" : chatState(row.id),
      shown: isShown(row),
    })),
  );
  const firstTerminal = $derived(rows.findIndex((row) => row.kind === "terminal"));

  // The shown row's cell stays in view when the shown row changes.
  $effect(() => {
    const shown = rows.find((row) => row.shown);
    if (!shown || !cells) return;
    queueMicrotask(() => cells?.querySelector(`[data-row="${CSS.escape(shown.key)}"]`)?.scrollIntoView({ block: "nearest" }));
  });

  function press(event, row) {
    if (row.kind !== "terminal") return;
    showRow(row);
    startDrag(event, row.id, cells, () => [...(cells?.querySelectorAll(".cell.terminal") ?? [])]);
  }
</script>

<nav class="strip" aria-label="Chats" bind:this={nav}>
  <button class="expand" type="button" aria-label="Open the list" title="Open the list (Alt+S)" onclick={toggleList}>
    <Icon name="chevron" class="right" />
  </button>
  <button class="tool new-session" type="button" aria-label="New chat" title="New chat (Alt+N)" onclick={newChat}>
    <Icon name="plus" size={18} />
  </button>
  <button class="tool new-terminal" type="button" aria-label="New terminal" title="New terminal (Alt+T)" onclick={newTerminal}>
    <Icon name="terminal" size={18} />
  </button>
  <div class="cells" bind:this={cells}>
    {#each rows as row, i (row.key)}
      {#if i === firstTerminal && i > 0}<span class="group-rule" aria-hidden="true"></span>{/if}
      <button
        class="cell {row.state}"
        class:shown={row.shown}
        class:dragging={row.kind === "terminal" && leftList.dragging === row.id}
        type="button"
        data-row={row.key}
        data-number={row.number}
        aria-label="{row.title}, {row.number}"
        aria-current={row.shown ? "page" : undefined}
        onclick={() => showRow(row)}
        onpointerdown={(event) => press(event, row)}
      >
        <Icon name={MARKS[row.state]} size={18} />
        <span class="number">{String(row.number).slice(0, 2)}</span>
      </button>
    {/each}
  </div>
</nav>

<style>
  /* 64 wide at 12/12, at the column's left; the column keeps its own width
     (App.svelte's grid). The "›" cell is 44 tall like the cells below and
     centered on the open list's wordmark row (27 to 67), so its hover fill
     and focus outline sit around the arrow (owner, 2026-10-20: the arrow sat
     low in a tall box); the "+" lines up with the list's New chat button
     (from 89). */
  .strip {
    align-self: flex-start;
    display: flex;
    flex-direction: column;
    box-sizing: border-box;
    width: calc(64 * var(--px));
    height: 100%;
    min-height: 0;
    overflow: hidden;
  }
  .expand {
    flex: none;
    display: grid;
    place-items: center;
    width: 100%;
    height: calc(44 * var(--px));
    margin-top: calc(25 * var(--px));
    padding: 0;
    color: var(--slate);
  }
  .expand :global(.right) {
    transform: rotate(-90deg);
  }
  .tool {
    flex: none;
    display: grid;
    place-items: center;
    width: 100%;
    height: calc(44 * var(--px));
    padding: 0;
    color: var(--slate);
  }
  .new-session {
    margin-top: calc(20 * var(--px));
  }
  .expand:hover,
  .tool:hover {
    color: var(--ink);
    background: var(--control-hover);
  }
  .expand:focus-visible,
  .tool:focus-visible,
  .cell:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(-2 * var(--px));
  }

  .cells {
    flex: 1 1 auto;
    min-height: 0;
    margin-top: calc(24 * var(--px));
    overflow-x: hidden;
    overflow-y: auto;
    scrollbar-width: none;
  }
  .group-rule {
    display: block;
    height: 1px;
    margin: 0 calc(12 * var(--px));
    background: var(--rule);
  }

  /* One cell per row: the mark, then the number. */
  .cell {
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: calc(6 * var(--px));
    width: 100%;
    height: calc(44 * var(--px));
    padding: 0 0 0 calc(4 * var(--px));
    color: var(--slate);
  }
  .cell:hover {
    background: var(--control-hover);
  }
  .number {
    min-width: calc(16 * var(--px));
    font: var(--t-mono);
    color: var(--slate);
    text-align: left;
  }
  .cell.shown {
    background: var(--band);
  }
  .cell.shown .number {
    color: var(--ink);
  }
  .cell.shown::after {
    content: "";
    position: absolute;
    left: calc(3 * var(--px));
    top: 0;
    bottom: 0;
    width: calc(4 * var(--px));
    background: var(--vermilion);
  }
  .cell.terminal {
    touch-action: none;
  }
  .cell.dragging {
    cursor: grabbing;
  }

  /* The marks (look 5.2). */
  .cell.waiting,
  .cell.terminal {
    color: var(--slate);
  }
  .cell.background {
    --icon-ring: var(--mark);
    --icon-arc: var(--mark);
  }
  .cell.unread {
    color: var(--ink);
  }
  .cell.ready {
    color: var(--rule-control);
  }
  .cell.unknown {
    color: var(--mark);
  }
</style>
