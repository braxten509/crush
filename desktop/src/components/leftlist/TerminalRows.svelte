<script>
  // This window's terminal tabs in the open list (desktop-design-5-6 2.20,
  // 2.25, look 5.17; LIST-001, LIST-009, LIST-010, LIST-045, LIST-055): the
  // list section "terminals" after the chats (internal/ui/sessionhost/
  // draw.go:18-23), under the group heading "Terminals [<n>]" (draw.go:
  // 450-469) with a "+" for a new terminal. A row has the terminal mark,
  // "Terminal" and, below, its folder's name; the shown tab sits on the band
  // with the vermilion edge, and its "×" asks to close it (TERM's close box).
  // A click shows the tab; pressing and dragging moves it within the group
  // (the pointer is captured until the release). The shown row stays in view.
  import { askCloseTerminal } from "../../lib/terminal/tabs.svelte.js";
  import { orderedTerminals, showRow, shownTerminal, startDrag } from "../../lib/leftlist/numbers.js";
  import { baseOf, clean, short } from "../../lib/leftlist/picker.js";
  import { leftList, newTerminal } from "../../lib/leftlist/store.svelte.js";
  import Icon from "../Icon.svelte";

  let list = $state(null);

  const tabs = $derived(orderedTerminals());
  const shown = $derived(shownTerminal());

  // The folder's name, home as "~" (draw.go:397-400).
  function folderOf(tab) {
    return tab?.cwd ? baseOf(short(clean(tab.cwd), leftList.home)) : "";
  }

  function titleOf(tab) {
    return tab?.title || "Terminal";
  }

  $effect(() => {
    if (!shown || !list) return;
    queueMicrotask(() => list?.querySelector(`[data-id="${CSS.escape(shown)}"]`)?.scrollIntoView({ block: "nearest" }));
  });

  function press(event, tab) {
    if (event.button !== 0 || event.target?.closest?.(".close")) return;
    showRow({ kind: "terminal", id: tab.id });
    startDrag(event, tab.id, list, () => [...(list?.querySelectorAll("li.terminal") ?? [])]);
  }

  function close(event, tab) {
    event.stopPropagation();
    askCloseTerminal(tab.id);
  }
</script>

<section class="terminals" aria-label="Terminals">
  <h2 class="group">
    <span class="label">Terminals [{tabs.length}]</span>
    <button class="add" type="button" aria-label="New terminal" title="New terminal (Alt+T)" onclick={newTerminal}>
      <Icon name="plus" />
    </button>
  </h2>
  <ul class="list" bind:this={list}>
    {#each tabs as tab (tab.id)}
      <li class="terminal" class:selected={tab.id === shown} class:dragging={leftList.dragging === tab.id} data-id={tab.id}>
        <button
          class="row"
          type="button"
          aria-current={tab.id === shown ? "page" : undefined}
          onclick={() => showRow({ kind: "terminal", id: tab.id })}
          onpointerdown={(event) => press(event, tab)}
        >
          <span class="title"><Icon name="terminal" size={18} /><span class="name">{titleOf(tab)}</span></span>
          <span class="folder"><Icon name="folder-filled" size={12} /><span class="path">{folderOf(tab)}</span></span>
        </button>
        <button class="close" type="button" aria-label="Close {titleOf(tab)}" onclick={(event) => close(event, tab)}>
          <Icon name="close" size={12} />
        </button>
      </li>
    {/each}
  </ul>
</section>

<style>
  /* The day heading's style and place (ChatList.svelte .day), with the "+"
     20 x 20 at the list's right edge minus 19, on the heading's centre line. */
  .group {
    position: relative;
    display: flex;
    align-items: center;
    margin: calc(14 * var(--px)) 0 0 calc(20 * var(--px));
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
  }
  .add {
    position: absolute;
    right: calc(19 * var(--px));
    top: 50%;
    transform: translateY(-50%);
    display: grid;
    place-items: center;
    width: calc(20 * var(--px));
    height: calc(20 * var(--px));
    padding: 0;
    color: var(--slate);
  }
  .add:hover {
    color: var(--ink);
  }
  .add:focus-visible,
  .close:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }

  .list {
    margin-top: calc(13 * var(--px));
  }
  .terminal {
    position: relative;
  }
  .terminal + .terminal {
    border-top: 1px solid var(--rule);
  }

  /* The chat row's geometry (list/ChatRow.svelte): 81 high, text from 23. */
  .row {
    position: relative;
    display: grid;
    grid-template-rows: calc(28 * var(--px)) auto;
    align-content: start;
    box-sizing: border-box;
    width: 100%;
    height: calc(81 * var(--px));
    padding: calc(12 * var(--px)) calc(48 * var(--px)) 0 calc(23 * var(--px));
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
  .folder {
    display: flex;
    align-items: center;
    gap: calc(6 * var(--px));
    min-width: 0;
    margin-top: calc(5 * var(--px));
    font: var(--t-meta);
    color: var(--slate);
  }
  .path {
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
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
    top: calc(20 * var(--px));
    right: calc(11.5 * var(--px));
    display: grid;
    place-items: center;
    width: calc(28 * var(--px));
    height: calc(12 * var(--px));
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
