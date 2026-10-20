<script>
  // TO-DO (owner, 2026-10-19: moved here from the tray above the typing box):
  // "TO-DO 2/4", then every step: done, in progress, pending
  // (trayrows.js todoList). The activity mark runs only while the agent works
  // on a step; "None" without steps.
  import Icon from "../Icon.svelte";
  import ActivityMark from "../timeline/ActivityMark.svelte";
  import { progress, todoHeader, todoList } from "../../lib/composer/trayrows.js";
  import { busy, sessions } from "../../lib/stores/sessions.svelte.js";

  const ICON = { completed: "check", in_progress: "dot", pending: "circle" };

  const list = $derived(sessions.byId[sessions.currentId]?.todos ?? []);
  const header = $derived(todoHeader(list, true));
  const working = $derived(Boolean(sessions.currentId) && busy(sessions.currentId) && progress(list).active !== null);
</script>

<section class="panel-section" class:empty={list.length === 0} data-section="todos" aria-label="To-do">
  <h2 class="label">{header.label}{#if list.length > 0}<span class="count">{header.count}</span>{/if}</h2>
  {#if list.length === 0}
    <p class="visually-hidden">None</p>
  {:else}
    <ul class="rows">
      {#each todoList(list) as item, at (at)}
        <li class="line {item.status}" data-status={item.status}>
          <span class="mark">
            {#if item.status === "in_progress" && working}
              <ActivityMark size="small" />
            {:else}
              <Icon name={ICON[item.status]} size={14} />
            {/if}
          </span>
          <span class="text">{item.text}</span>
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  .label {
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
  }
  /* An empty section steps back (owner, 2026-10-20): a quieter heading and
     no "None" line; the heading keeps its place. */
  .empty .label {
    color: var(--mark);
  }
  .count {
    margin-left: calc(8 * var(--px));
    font-variant-numeric: tabular-nums;
  }
  .rows,
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
  .in_progress .text {
    font-weight: 600;
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
