<script>
  // The tray's queue (TYPE-044, TYPE-045, TYPE-050; a port of
  // internal/ui/model/pills.go:87-120): "QUEUE 2" and Interrupt, then one slate
  // line per queued prompt, the first tagged "waiting · sends next".
  import { queueRows } from "../../lib/composer/trayrows.js";
  import { visibleItems } from "../../lib/stores/queue.svelte.js";
  import { interruptAndSend } from "../../lib/stores/composer.svelte.js";
  import { sessions } from "../../lib/stores/sessions.svelte.js";

  const rows = $derived(queueRows(visibleItems(sessions.currentId)));
</script>

<div class="head" data-tray-row>
  <span class="name">Queue</span>
  <span class="count">{rows.length}</span>
  <button class="interrupt" type="button" onclick={() => interruptAndSend()}>Interrupt</button>
</div>
<ul class="list">
  {#each rows as row, at (at)}
    <li class="item" data-tray-row>
      <span class="text">{row.text}</span>
      {#if row.tag}<span class="tag">{row.tag}</span>{/if}
    </li>
  {/each}
</ul>

<style>
  .head {
    display: flex;
    align-items: baseline;
    gap: calc(10 * var(--px));
    min-height: calc(28 * var(--px));
    padding: calc(2 * var(--px)) 0;
    color: var(--ink);
  }
  .name,
  .count {
    flex: none;
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
  }
  .count {
    font-variant-numeric: tabular-nums;
  }
  .interrupt {
    margin-left: auto;
    font: var(--t-small);
    color: var(--ink);
  }
  .interrupt:hover {
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }
  .list {
    margin: 0;
    padding: 0 0 calc(4 * var(--px));
    list-style: none;
  }
  .item {
    display: flex;
    align-items: baseline;
    gap: calc(12 * var(--px));
    min-height: calc(28 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }
  .text {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tag {
    flex: none;
    white-space: nowrap;
  }
</style>
