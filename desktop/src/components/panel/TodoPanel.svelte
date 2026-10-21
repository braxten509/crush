<script>
  // TO-DO (owner, 2026-10-19: moved here from the tray above the typing box):
  // "TO-DO 2/4", then every step: done, in progress, pending
  // (trayrows.js todoList); the step in progress shows the busy dots, in step
  // with every other busy mark (owner, 2026-10-20 and 2026-10-21); "None"
  // without steps.
  import Icon from "../Icon.svelte";
  import ActivityMark from "../timeline/ActivityMark.svelte";
  import { todoHeader, todoList } from "../../lib/composer/trayrows.js";
  import { sessions } from "../../lib/stores/sessions.svelte.js";

  const ICON = { completed: "check", pending: "circle" };

  const list = $derived(sessions.byId[sessions.currentId]?.todos ?? []);
  const header = $derived(todoHeader(list, true));
</script>

<!-- Shown only while it has something in it (owner, 2026-10-20). -->
{#if list.length > 0}
<section class="panel-section" data-section="todos" aria-label="To-do">
  <h2 class="label">{header.label}<span class="count">{header.count}</span></h2>
  <ul class="rows">
    {#each todoList(list) as item, at (at)}
      <li class="line {item.status}" data-status={item.status}>
        <span class="mark">
          {#if item.status === "in_progress"}
            <ActivityMark size="small" />
          {:else}
            <Icon name={ICON[item.status]} size={14} />
          {/if}
        </span>
        <span class="text">{item.text}</span>
      </li>
    {/each}
  </ul>
</section>
{/if}

<style>
  .label {
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
  }
  .count {
    margin-left: calc(8 * var(--px));
    font-variant-numeric: tabular-nums;
  }
  /* One step per line, under the heading. */
  .rows {
    margin-top: calc(8 * var(--px));
  }
  .line {
    display: flex;
    align-items: flex-start;
    gap: calc(8 * var(--px));
    min-height: calc(24 * var(--px));
    font: var(--t-small);
  }
  .completed .text {
    color: var(--slate);
  }
  .completed .text {
    text-decoration: line-through;
  }
  .mark {
    flex: none;
    display: grid;
    place-items: center;
    width: calc(16 * var(--px));
    height: calc(24 * var(--px));
  }
  .text {
    min-width: 0;
    overflow-wrap: anywhere;
  }
</style>
