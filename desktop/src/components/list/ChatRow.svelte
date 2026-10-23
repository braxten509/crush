<script>
  // One chat in the left list ({row} from leftlist/openlist.listedRows): the
  // title (its folder is the heading it sits under, owner 2026-10-21), and
  // on the right the time or what the chat is doing
  // (LIST-012: "Working…", "Waiting for permission", "Waiting for your
  // answer", "Background"). Every row is one line (owner, 2026-10-22, pick
  // C): the state takes the time's place. A hidden copy of the time holds a
  // column of the row's width, so a long title ends before the time instead
  // of under it. Rows deeper than the top use
  // the built branch look (phase 4 nests branches, 4.8).
  //
  // Every row reserves a close slot at its right end (4.8, review finding
  // 13): the × shows in it while the row is hovered, focused or selected,
  // and nothing else on the row moves. A row with branches has its chevron
  // just left of that slot, and its time moves over to make room.
  // The × is the row button's sibling (a button cannot hold a button).
  //
  // A row with none of phase 3's states draws the first registered phase 6
  // state (lib/rowstates.js: LST's unread, ready and unknown,
  // desktop-design-5-6 2.20) in the same slot: its icon and text.
  import Icon from "../Icon.svelte";
  import { rowState } from "../../lib/rowstates.js";
  import { closeListed, openInFolder } from "../../lib/stores/openchats.svelte.js";
  import { hideHelperChat } from "../../lib/stores/helperchats.svelte.js";
  import { focusField } from "../../lib/stores/composer.svelte.js";
  import { openChat, sessions } from "../../lib/stores/sessions.svelte.js";

  let { row } = $props();
  let rowButton = $state(null);

  // A row of another folder (row.folder) opens in that folder: the window
  // switches to it first. The × closes the chat for every app (the open
  // chats shared with the terminal).
  // The empty new chat (row.draft) is the landing page: a click shows it, the
  // × takes the row away.
  function open() {
    if (row.draft) focusField();
    else if (row.folder) openInFolder(row.folder.dir, row.id).catch((error) => console.error("opening a chat failed", error));
    else openChat(row.id);
  }

  function close(event) {
    event.stopPropagation();
    if (row.draft) {
      sessions.draft = false;
      return;
    }
    // A sub-agent's row only hides; the sub-agent keeps working.
    if (row.helper) {
      hideHelperChat(row.id);
      return;
    }
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
    class:has-branches={row.branches > 0}
    type="button"
    aria-current={row.selected ? "page" : undefined}
    aria-expanded={row.branches > 0 ? row.expanded : undefined}
    onclick={open}
  >
    <span class="title">{row.title}</span>
    <span class="line2">
      {#if row.helper}<span class="helper-mark">Subagent</span>{/if}
      {#if row.state}
        <span class="state {row.stateKind}">
          <Icon name={STATE_ICONS[row.stateKind] ?? "working"} />
          <span>{row.state}</span>
        </span>
      {:else if extra}
        <span class="state registered">
          <Icon name={extra.icon} />
          <span>{extra.text}</span>
          {#if row.time}<span>{row.time}</span>{/if}
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
  /* 2 screen px between rows, so two bands never touch (owner, 2026-10-22:
     1-4 px). */
  :global(.chat) + .chat {
    margin-top: 2px;
  }
  :global(.chat.expanded) + .chat,
  .chat.branch,
  :global(.chat.branch) + .chat:not(.branch) {
    border-top: 0;
  }

  /* Columns: the title, what sits on its right (the sub-agent mark and the
     state), the time's room, and the 48 picture px the time keeps from the
     row's right edge (the close slot). One line (owner, 2026-10-22); the
     title keeps at least half the row, so a long state is cut instead. */
  .row {
    --end: calc(48 * var(--px));
    position: relative;
    display: grid;
    grid-template-columns: minmax(50%, 1fr) minmax(0, auto) auto var(--end);
    align-items: center;
    box-sizing: border-box;
    width: 100%;
    height: calc(44 * var(--px));
    padding: 0 0 0 calc(23 * var(--px));
    text-align: left;
  }
  .row.has-branches {
    --end: calc(74 * var(--px));
  }
  .title {
    grid-column: 1;
    grid-row: 1;
    padding-right: calc(16 * var(--px));
    font: var(--t-item);
    text-box: trim-both cap alphabetic;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .line2 {
    grid-column: 2;
    grid-row: 1;
    display: flex;
    align-items: center;
    gap: calc(12 * var(--px));
    min-width: 0;
    overflow: hidden;
  }
  .has-state .line2 {
    padding-right: calc(4 * var(--px));
  }
  /* A sub-agent's chat (owner, 2026-10-21), in the panel headings' type. */
  .helper-mark {
    flex: none;
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
    color: var(--slate);
  }
  .state {
    display: flex;
    flex: 0 1 auto;
    min-width: 0;
    align-items: center;
    gap: calc(8 * var(--px));
    font: var(--t-small);
    color: var(--slate);
    white-space: nowrap;
  }
  .state > span {
    text-box: trim-both cap alphabetic;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  /* Keep accents and descenders inside the clipping area without changing
     the capital-height center used beside the icons. */
  .title,
  .state > span {
    padding-block: 0.3em;
    margin-block: -0.3em;
  }
  .state :global(svg) {
    flex: none;
  }
  .time-room {
    grid-column: 3;
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
    right: var(--end);
    top: 50%;
    transform: translateY(-50%);
    font: var(--t-small);
    text-box: trim-both cap alphabetic;
    color: var(--slate);
    white-space: nowrap;
  }
  .row :global(.chevron) {
    position: absolute;
    right: calc(45 * var(--px));
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
    height: calc(44 * var(--px) + 2px);
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
    padding-left: calc((64 + 28 * (var(--depth) - 1)) * var(--px));
  }
  .branch .title {
    font: var(--t-branch);
  }
  /* Over the row's band, so the open or hovered branch keeps its line
     (owner, 2026-10-21: it vanished while the dash stayed). */
  .branch::before {
    content: "";
    position: absolute;
    z-index: 1;
    left: calc((35 + 28 * (var(--depth) - 1)) * var(--px));
    /* Up through the 2px between rows, so the line stays whole. */
    top: -2px;
    bottom: 0;
    width: max(1px, calc(1.5 * var(--px)));
    background: var(--figure);
  }
  .branch::after {
    content: "";
    position: absolute;
    left: calc((35 + 28 * (var(--depth) - 1)) * var(--px));
    top: 50%;
    width: calc(16 * var(--px));
    height: max(1px, calc(1.5 * var(--px)));
    background: var(--figure);
  }

  .row:hover .title {
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }

  /* The close slot: the × (12) centred 25.5 from the row's right edge, on
     the row's middle line. Its hit area is as big as the row allows (owner,
     2026-10-20: small controls get a bigger click area): 36 wide and the
     row's full height, clear of the chevron (right 45 to 58). */
  .close {
    position: absolute;
    top: 0;
    bottom: 0;
    right: calc(7.5 * var(--px));
    display: grid;
    place-items: center;
    width: calc(36 * var(--px));
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
