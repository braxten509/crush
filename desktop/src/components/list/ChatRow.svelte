<script>
  // One chat in the left list ({row} from leftlist/openlist.listedRows): the
  // title (its folder is the heading it sits under, owner 2026-10-21), and
  // on the right the time or what the chat is doing
  // (LIST-012: "Working…", "Waiting for permission", "Waiting for your
  // answer", "Background"). The time keeps the picture's place on the row's
  // middle line; a hidden copy of it holds a column of the row's width, so a
  // long title ends before the time instead of under it. The state sits on the
  // second line. Rows deeper than the top use
  // the built branch look (phase 4 nests branches, 4.8).
  //
  // Every row reserves a close slot on the title's first line, centred on the
  // chevron's column (4.8, review finding 13): the × shows in it while the
  // row is hovered, focused or selected, and nothing else on the row moves.
  // The × is the row button's sibling (a button cannot hold a button).
  //
  // A row with none of phase 3's states draws the first registered phase 6
  // state (lib/rowstates.js: LST's unread, ready and unknown,
  // desktop-design-5-6 2.20) in the same slot: its icon and text.
  import Icon from "../Icon.svelte";
  import { rowState } from "../../lib/rowstates.js";
  import { closeListed, openInFolder } from "../../lib/stores/openchats.svelte.js";
  import { openChat } from "../../lib/stores/sessions.svelte.js";

  let { row } = $props();
  let rowButton = $state(null);

  // A row of another folder (row.folder) opens in that folder: the window
  // switches to it first. The × closes the chat for every app (the open
  // chats shared with the terminal).
  function open() {
    if (row.folder) openInFolder(row.folder.dir, row.id).catch((error) => console.error("opening a chat failed", error));
    else openChat(row.id);
  }

  function close(event) {
    event.stopPropagation();
    closeListed(row.id).catch((error) => console.error("closing a chat failed", error));
  }

  // ↑ and ↓ on the × move from its row, as from the row itself (the list's keys).
  function closeKey(event) {
    if (event.key === "ArrowUp" || event.key === "ArrowDown") rowButton?.focus();
  }

  const STATE_ICONS = { working: "working", waiting: "clock", background: "gear" };

  const extra = $derived(row.state ? null : rowState(row.id));
</script>

<li
  class="chat"
  class:selected={row.selected}
  class:expanded={row.expanded}
  class:branch={row.depth > 0}
  data-id={row.id}
  style:--depth={Math.min(row.depth, 6)}
>
  <button
    bind:this={rowButton}
    class="row"
    class:has-state={row.state !== "" || extra !== null}
    type="button"
    aria-current={row.selected ? "page" : undefined}
    aria-expanded={row.branches > 0 ? row.expanded : undefined}
    onclick={open}
  >
    <span class="title">{row.title}</span>
    <span class="line2">
      {#if row.state}
        <span class="state {row.stateKind}">
          <Icon name={STATE_ICONS[row.stateKind] ?? "working"} />
          <span>{row.state}</span>
        </span>
      {:else if extra}
        <span class="state registered">
          <Icon name={extra.icon} />
          <span>{extra.text}</span>
        </span>
      {/if}
    </span>
    {#if !row.state && !extra && row.time}
      <span class="time-room" aria-hidden="true">{row.time}</span>
      <span class="time">{row.time}</span>
    {/if}
    {#if row.branches > 0}
      <Icon name="chevron" class="chevron" />
    {/if}
  </button>
  <button class="close" type="button" aria-label="Close {row.title}" onclick={close} onkeydown={closeKey}>
    <Icon name="close" size={12} />
  </button>
</li>

<style>
  .chat {
    position: relative;
  }
  :global(.chat) + .chat {
    border-top: 1px solid var(--rule);
  }
  :global(.chat.expanded) + .chat,
  .chat.branch,
  :global(.chat.branch) + .chat:not(.branch) {
    border-top: 0;
  }

  /* Columns: the text, the time's room, and the 48 picture px the time keeps
     from the row's right edge (with the chevron in it). Rows: the title line
     and the state line. */
  .row {
    position: relative;
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto calc(48 * var(--px));
    grid-template-rows: calc(28 * var(--px)) auto;
    align-content: start;
    box-sizing: border-box;
    width: 100%;
    height: calc(81 * var(--px));
    padding: calc(12 * var(--px)) 0 0 calc(23 * var(--px));
    text-align: left;
  }
  .title {
    grid-column: 1;
    grid-row: 1;
    padding-right: calc(32 * var(--px));
    font: var(--t-item);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .line2 {
    grid-column: 1;
    grid-row: 2;
    display: flex;
    align-items: center;
    gap: calc(12 * var(--px));
    min-width: 0;
    margin-top: calc(5 * var(--px));
    padding-right: calc(32 * var(--px));
  }
  .has-state .line2 {
    grid-column: 1 / -1;
    padding-right: calc(24 * var(--px));
  }
  .state {
    display: flex;
    flex: none;
    align-items: center;
    gap: calc(8 * var(--px));
    margin-left: auto;
    font: var(--t-small);
    color: var(--slate);
    white-space: nowrap;
  }
  .time-room {
    grid-column: 2;
    grid-row: 1;
    height: 0;
    overflow: hidden;
    visibility: hidden;
    font: var(--t-small);
    white-space: nowrap;
  }
  /* The time and the chevron sit on the row's middle line. */
  .time {
    position: absolute;
    right: calc(48 * var(--px));
    top: 50%;
    transform: translateY(-50%);
    font: var(--t-small);
    color: var(--slate);
    white-space: nowrap;
  }
  .row :global(.chevron) {
    position: absolute;
    right: calc(19 * var(--px));
    top: 50%;
    transform: translateY(-50%);
    color: var(--ink);
  }

  /* The open chat: a band between faint lines, with a vermilion edge. The
     lines are drawn inside the row's own box, so the open chat keeps a plain
     row's height and nothing on it or below it moves when the choice moves;
     the edge is drawn after them, over their ends. */
  /* The band reaches 1px past each end, its faint lines over the lines
     between rows (owner, 2026-10-20); the negative margins keep every row
     where it was. */
  .selected .row {
    margin: -1px 0;
    height: calc(81 * var(--px) + 2px);
    padding-top: calc(12 * var(--px) + 1px);
    background: var(--band);
  }
  .selected.branch .row {
    height: calc(72 * var(--px) + 2px);
    padding-top: calc(13 * var(--px) + 1px);
  }
  .selected .row::before {
    content: "";
    position: absolute;
    inset: 0;
    border-top: 1px solid var(--rule-faint);
    border-bottom: 1px solid var(--rule-faint);
    pointer-events: none;
  }
  /* The band's full height, so it ends with the band's lines. */
  .selected .row::after {
    content: "";
    position: absolute;
    z-index: 1;
    /* Flush with the column's line (owner, 2026-10-20: no gap at its side). */
    left: 0;
    top: 0;
    bottom: 0;
    width: calc(4 * var(--px));
    background: var(--vermilion);
    /* Holds still while the list and the strip slide (motion.js slidePage). */
    transform: translateX(calc(-1 * var(--slide-shift, 0px)));
  }

  /* Branch rows (phase 4, 4.8): the built branch look, indented by depth. */
  .branch .row {
    height: calc(72 * var(--px));
    padding: calc(13 * var(--px)) 0 0 calc((64 + 28 * (var(--depth) - 1)) * var(--px));
    grid-template-rows: calc(26 * var(--px)) auto;
  }
  .branch .title {
    font: var(--t-branch);
  }
  .branch .line2 {
    margin-top: 0;
  }
  .branch .time {
    top: calc(25 * var(--px));
    transform: none;
  }
  /* Over the row's band, so the open or hovered branch keeps its line
     (owner, 2026-10-21: it vanished while the dash stayed). */
  .branch::before {
    content: "";
    position: absolute;
    z-index: 1;
    left: calc((35 + 28 * (var(--depth) - 1)) * var(--px));
    top: 0;
    bottom: 0;
    width: max(1px, calc(1.5 * var(--px)));
    background: var(--figure);
  }
  .branch::after {
    content: "";
    position: absolute;
    left: calc((35 + 28 * (var(--depth) - 1)) * var(--px));
    top: calc(34 * var(--px));
    width: calc(16 * var(--px));
    height: max(1px, calc(1.5 * var(--px)));
    background: var(--figure);
  }

  .row:hover .title {
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }

  /* The close slot: the × (12) centred on the chevron's column (right 19 +
     13 / 2 = 25.5 from the row's right edge) and on the title's first line
     (26 from the row's top on chat and branch rows alike, selected or not).
     Its hit area is as big as the row allows (owner, 2026-10-20: small
     controls get a bigger click area): 36 wide, from the row's top to just
     above the chevron (from 32.5 on a branch row), so a click there stays
     the row's. */
  .close {
    position: absolute;
    top: 0;
    right: calc(7.5 * var(--px));
    display: grid;
    justify-items: center;
    align-items: end;
    width: calc(36 * var(--px));
    height: calc(32 * var(--px));
    padding: 0;
    color: var(--slate);
    visibility: hidden;
  }
  .chat:hover .close,
  .chat:focus-within .close,
  .selected .close {
    visibility: visible;
  }
  .close:hover {
    color: var(--ink);
  }
  .close:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(-2 * var(--px));
  }
</style>
