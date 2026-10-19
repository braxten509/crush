<script>
  // One chat in the left list ({row} from listmodel.buildRows): the title,
  // the agent under it, and on the right the time or what the chat is doing
  // (LIST-012: "Working…", "Waiting for permission", "Waiting for your
  // answer", "Background"). The time keeps the picture's place on the row's
  // middle line; a hidden copy of it holds a column of the row's width, so a
  // long title ends before the time instead of under it. The state shares the
  // agent's line and the agent gives way to it. Rows deeper than the top use
  // the built branch look (phase 4 nests branches, 4.8).
  import Icon from "../Icon.svelte";
  import { openChat } from "../../lib/stores/sessions.svelte.js";

  let { row } = $props();

  const STATE_ICONS = { working: "working", waiting: "clock", background: "gear" };
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
    class="row"
    class:has-state={row.state !== ""}
    type="button"
    aria-current={row.selected ? "page" : undefined}
    aria-expanded={row.branches > 0 ? row.expanded : undefined}
    onclick={() => openChat(row.id)}
  >
    <span class="title">{row.title}</span>
    <span class="line2">
      {#if row.agent}<span class="agent">{row.agent}</span>{/if}
      {#if row.state}
        <span class="state {row.stateKind}">
          <Icon name={STATE_ICONS[row.stateKind] ?? "working"} />
          <span>{row.state}</span>
        </span>
      {/if}
    </span>
    {#if !row.state && row.time}
      <span class="time-room" aria-hidden="true">{row.time}</span>
      <span class="time">{row.time}</span>
    {/if}
    {#if row.branches > 0}
      <Icon name="chevron" class="chevron" />
    {/if}
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
     and the agent line. */
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
  .agent {
    flex: 0 1 auto;
    min-width: 0;
    font: var(--t-meta);
    color: var(--slate);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
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

  /* The open chat: a band between faint lines, with a vermilion edge. */
  .selected .row {
    background: var(--band);
    border-top: 1px solid var(--rule-faint);
    border-bottom: 1px solid var(--rule-faint);
    height: calc(77 * var(--px) + 2px);
  }
  .selected .line2 {
    margin-top: calc(3 * var(--px));
  }
  .selected .row::before {
    content: "";
    position: absolute;
    left: calc(3 * var(--px));
    top: -1px;
    bottom: -1px;
    width: calc(4 * var(--px));
    background: var(--vermilion);
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
  .branch .agent {
    font: var(--t-agent-mono);
    line-height: calc(20 * var(--px));
  }
  .branch .time {
    top: calc(25 * var(--px));
    transform: none;
  }
  .branch::before {
    content: "";
    position: absolute;
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
</style>
