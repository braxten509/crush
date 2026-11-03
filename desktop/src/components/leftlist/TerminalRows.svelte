<script>
  // One folder's terminal tabs (desktop-design-5-6 2.20, 2.25, look 5.17;
  // LIST-001, LIST-009, LIST-010, LIST-045, LIST-055), drawn in the folder
  // after its chats (owner, 2026-11-03: no separate terminals part). A row has
  // the terminal mark and its title; the shown tab sits on the band with the
  // vermilion edge, and its "×" asks to close it (TERM's close box). A click
  // shows the tab; pressing and dragging moves it among its folder's
  // terminals (the pointer is captured until the release). A double-click or
  // right-click renames it in place: Enter or leaving the box keeps the name,
  // Esc drops it, an empty name goes back to the shell's title, and the keys
  // go back to the shown terminal (owner, 2026-11-03). The shown row stays in view.
  import { askCloseTerminal, renameTerminal, showTerminal } from "../../lib/terminal/tabs.svelte.js";
  import { showRow, shownTerminal, startDrag } from "../../lib/leftlist/numbers.js";
  import { leftList } from "../../lib/leftlist/store.svelte.js";
  import Icon from "../Icon.svelte";

  let { tabs = [] } = $props();
  let list = $state(null);
  let editing = $state(""); // the tab being renamed
  let draft = $state("");

  const shown = $derived(shownTerminal());

  function titleOf(tab) {
    return tab?.title || "Terminal";
  }

  $effect(() => {
    if (!shown || !list) return;
    queueMicrotask(() => list?.querySelector(`[data-id="${CSS.escape(shown)}"]`)?.scrollIntoView({ block: "nearest" }));
  });

  function press(event, tab) {
    if (event.button !== 0 || event.target?.closest?.(".close, .rename")) return;
    showRow({ kind: "terminal", id: tab.id });
    startDrag(event, tab.id, list, () => [...(list?.querySelectorAll("li.terminal") ?? [])]);
  }

  function rename(tab) {
    editing = tab.id;
    draft = tab.name || titleOf(tab);
  }

  // The drag holds the pointer on the list, so a double-click lands on the
  // list itself: the row under the pointer is the one meant.
  function dblclick(event) {
    const id = document.elementFromPoint(event.clientX, event.clientY)?.closest?.("li.terminal")?.dataset.id;
    const tab = tabs.find((t) => t.id === id);
    if (tab) rename(tab);
  }

  function menu(event, tab) {
    event.preventDefault();
    rename(tab);
  }

  // Done by Enter or Esc: the keys go back to the shown terminal (a click
  // elsewhere keeps the name and leaves the focus where it went).
  function stop(id, refocus = true) {
    editing = "";
    if (refocus && id === shown) showTerminal(id);
  }

  function keep(refocus = true) {
    if (!editing) return;
    const id = editing;
    renameTerminal(id, draft);
    stop(id, refocus);
  }

  function onRenameKey(event) {
    event.stopPropagation();
    if (event.key === "Enter") {
      event.preventDefault();
      keep();
    } else if (event.key === "Escape") {
      event.preventDefault();
      stop(editing);
    }
  }

  function focusSelect(node) {
    node.focus();
    node.select();
  }

  function close(event, tab) {
    event.stopPropagation();
    askCloseTerminal(tab.id);
  }
</script>

<ul class="terminals" aria-label="Terminals" bind:this={list} ondblclick={dblclick}>
  {#each tabs as tab (tab.id)}
    <li class="terminal" class:selected={tab.id === shown} class:dragging={leftList.dragging === tab.id} data-id={tab.id}>
      {#if editing === tab.id}
        <div class="row editing">
          <span class="title">
            <Icon name="terminal" size={18} />
            <input
              class="rename"
              type="text"
              aria-label="Terminal name"
              placeholder="Terminal"
              maxlength="80"
              bind:value={draft}
              use:focusSelect
              onkeydown={onRenameKey}
              onblur={() => keep(false)}
            />
          </span>
        </div>
      {:else}
        <button
          class="row"
          type="button"
          title="Double-click to rename"
          aria-current={tab.id === shown ? "page" : undefined}
          onclick={() => showRow({ kind: "terminal", id: tab.id })}
          onpointerdown={(event) => press(event, tab)}
          oncontextmenu={(event) => menu(event, tab)}
        >
          <span class="title"><Icon name="terminal" size={18} /><span class="name">{titleOf(tab)}</span></span>
        </button>
      {/if}
      <button class="close" type="button" aria-label="Close {titleOf(tab)}" onclick={(event) => close(event, tab)}>
        <Icon name="close" size={12} />
      </button>
    </li>
  {/each}
</ul>

<style>
  /* Straight under the folder's chats; under its line, 3 px down as the
     chats are (ChatList.svelte .list). */
  :global(.folder) + .terminals {
    margin-top: 3px;
  }
  /* 2 screen px after the last chat and between rows, as between chats
     (ChatRow.svelte), so two bands never touch. */
  :global(.list) + .terminals,
  .terminal + .terminal {
    margin-top: 2px;
  }
  .close:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }
  .terminal {
    position: relative;
  }

  /* The chat row's geometry (list/ChatRow.svelte): one line 44 high, text
     from 23. */
  .row {
    position: relative;
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    column-gap: calc(16 * var(--px));
    box-sizing: border-box;
    width: 100%;
    height: calc(44 * var(--px));
    padding: 0 calc(48 * var(--px)) 0 calc(23 * var(--px));
    text-align: left;
    touch-action: none;
  }
  .title {
    display: flex;
    align-items: center;
    gap: calc(10 * var(--px));
    min-width: 0;
    color: var(--slate);
  }
  .name {
    min-width: 0;
    font: var(--t-item);
    color: var(--ink);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  /* The name box sits where the name was, on the same line. */
  .rename {
    flex: 1 1 auto;
    min-width: 0;
    height: calc(30 * var(--px));
    padding: 0 calc(6 * var(--px));
    margin-left: calc(-7 * var(--px));
    box-sizing: border-box;
    border: 1px solid var(--rule-control);
    background: var(--paper-raised);
    font: var(--t-item);
    color: var(--ink);
    outline: none;
  }
  .rename:focus-visible {
    border-color: var(--vermilion);
  }
  .row:hover .name {
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }
  .dragging .row {
    cursor: grabbing;
  }

  /* The shown tab, as the open chat (ChatRow.svelte): a band between faint
     lines, with the vermilion edge. */
  .selected .row {
    background: var(--band);
  }
  .selected .row::before {
    content: "";
    position: absolute;
    inset: 0;
    border-top: 1px solid var(--rule-faint);
    border-bottom: 1px solid var(--rule-faint);
    pointer-events: none;
  }
  .selected .row::after {
    content: "";
    position: absolute;
    left: calc(3 * var(--px));
    top: 0;
    bottom: 0;
    width: calc(4 * var(--px));
    background: var(--vermilion);
    /* Holds still while the list and the strip slide (motion.js slidePage). */
    transform: translateX(calc(-1 * var(--slide-shift, 0px)));
  }

  /* The close slot, where the chat row keeps its own (ChatRow.svelte .close). */
  .close {
    position: absolute;
    top: 0;
    bottom: 0;
    right: calc(11.5 * var(--px));
    display: grid;
    place-items: center;
    width: calc(28 * var(--px));
    padding: 0;
    color: var(--slate);
    visibility: hidden;
  }
  .terminal:hover .close,
  .terminal:focus-within .close,
  .selected .close {
    visibility: visible;
  }
  .close:hover {
    color: var(--ink);
  }
</style>
