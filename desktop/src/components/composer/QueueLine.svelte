<script>
  // The queue on Complete's row while no chips take it (owner, 2026-10-21,
  // pick A): "QUEUE 2", the next prompt, "waiting · sends next", "+1 more"
  // and Interrupt, on one line. With chips on the row the queue stays in the
  // tray (QueueSection.svelte).
  import { queueRows } from "../../lib/composer/trayrows.js";
  import { visibleItems } from "../../lib/stores/queue.svelte.js";
  import { interruptAndSend } from "../../lib/stores/composer.svelte.js";
  import { sessions } from "../../lib/stores/sessions.svelte.js";

  const rows = $derived(queueRows(visibleItems(sessions.currentId)));
  const first = $derived(rows[0]);
</script>

{#if first}
  <div class="queue-line" data-probe="queue-line">
    <span class="name">Queue</span>
    <span class="count">{rows.length}</span>
    <span class="text" title={first.text}>{first.text}</span>
    {#if first.tag}<span class="tag">{first.tag}</span>{/if}
    {#if rows.length > 1}<span class="more">+{rows.length - 1} more</span>{/if}
    <button class="interrupt" type="button" onclick={() => interruptAndSend()}>Interrupt</button>
  </div>
{/if}

<style>
  .queue-line {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: baseline;
    gap: calc(10 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }
  .name,
  .count {
    flex: none;
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
    color: var(--ink);
  }
  .count {
    font-variant-numeric: tabular-nums;
  }
  .text {
    min-width: 0;
    margin-left: calc(2 * var(--px));
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tag,
  .more {
    flex: none;
    white-space: nowrap;
  }
  .interrupt {
    flex: none;
    margin-left: auto;
    font: var(--t-small);
    color: var(--ink);
  }
  .interrupt:hover {
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }
  .interrupt:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }
</style>
