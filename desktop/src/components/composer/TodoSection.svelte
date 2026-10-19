<script>
  // The tray's to-do section (SIDE-031, SIDE-032, SIDE-034; a port of
  // internal/ui/model/pills.go:47-85, 287-308): "TO-DO 1/4"; closed, the step
  // in progress; the ctrl+t hint at the right. Open, the list: done, in
  // progress, pending. The activity mark runs only while the agent works on
  // a step. A click on the header line opens or closes the list.
  import Icon from "../Icon.svelte";
  import ActivityMark from "../timeline/ActivityMark.svelte";
  import { progress, todoHeader, todoList } from "../../lib/composer/trayrows.js";
  import { busy, sessions } from "../../lib/stores/sessions.svelte.js";
  import { todos, toggle } from "../../lib/stores/todos.svelte.js";

  const ICON = { completed: "check", in_progress: "dot", pending: "circle" };

  const list = $derived(sessions.byId[sessions.currentId]?.todos ?? []);
  const header = $derived(todoHeader(list, todos.open));
  const working = $derived(Boolean(sessions.currentId) && busy(sessions.currentId) && progress(list).active !== null);
  const hintWord = $derived(header.hint.replace(/^ctrl\+t\s*/, ""));
</script>

<button class="head" type="button" data-tray-row aria-expanded={todos.open} onclick={() => toggle()}>
  <span class="name">{header.label}</span>
  <span class="count">{header.count}</span>
  {#if header.step}
    <span class="step">
      {#if working}<span class="activity" data-activity><ActivityMark size="small" /></span>{/if}
      <span class="step-text">{header.step}</span>
    </span>
  {/if}
  <span class="hint"><span class="key">ctrl+t</span> {hintWord}</span>
</button>
{#if todos.open}
  <ul class="list">
    {#each todoList(list) as item, at (at)}
      <li class="item {item.status}" data-tray-row data-status={item.status}>
        <span class="mark">
          {#if item.status === "in_progress" && working}
            <span class="activity" data-activity><ActivityMark size="small" /></span>
          {:else}
            <Icon name={ICON[item.status]} size={16} />
          {/if}
        </span>
        <span class="text">{item.text}</span>
      </li>
    {/each}
  </ul>
{/if}

<style>
  .head {
    display: flex;
    align-items: baseline;
    gap: calc(10 * var(--px));
    width: 100%;
    min-height: calc(28 * var(--px));
    padding: calc(2 * var(--px)) 0;
    text-align: left;
    color: var(--ink);
  }
  .name {
    flex: none;
    font: var(--t-label);
    letter-spacing: var(--track-label);
  }
  .count {
    flex: none;
    font: var(--t-label);
    font-variant-numeric: tabular-nums;
  }
  .step {
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    min-width: 0;
    align-self: center;
  }
  .step-text {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font: var(--t-small);
    color: var(--slate);
  }
  .hint {
    flex: none;
    margin-left: auto;
    font: var(--t-small);
    color: var(--slate);
    white-space: nowrap;
  }
  .key {
    color: var(--ink);
  }
  .activity {
    flex: none;
    display: flex;
    align-items: center;
  }
  .list {
    margin: 0;
    padding: 0 0 calc(4 * var(--px));
    list-style: none;
  }
  .item {
    display: flex;
    align-items: center;
    gap: calc(10 * var(--px));
    min-height: calc(28 * var(--px));
    font: var(--t-small);
    color: var(--ink);
  }
  .item.completed {
    color: var(--slate);
  }
  .item.completed .mark {
    color: var(--ink);
  }
  .item.pending .mark {
    color: var(--slate);
  }
  .mark {
    flex: none;
    display: grid;
    place-items: center;
    width: calc(16 * var(--px));
    height: calc(16 * var(--px));
  }
  .text {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
