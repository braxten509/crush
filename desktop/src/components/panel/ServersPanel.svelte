<script>
  // SERVERS (owner, 2026-10-19: moved here from the bottom of the left list):
  // the ports the project's background jobs listen on (lib/leftlist/
  // servers.js, read every second by plugins/leftlist.js). A server's name
  // opens http://localhost:<port>; a program with several ports is one row
  // that opens to list them. The stop cross keeps its place and shows on
  // hover; it stops that server's job (a program's: all its ports).
  import Icon from "../Icon.svelte";
  import { foldOf, rowTexts, serverRows, stopServer, useRow, visibleServers } from "../../lib/leftlist/servers.js";
  import { leftList } from "../../lib/leftlist/store.svelte.js";

  const rows = $derived(serverRows(visibleServers(), leftList.openFolds));
  const key = (row) => (row.ports > 0 ? `fold:${foldOf(row.server)}` : `${row.child ? "child" : "port"}:${row.server.port}`);
</script>

<!-- Shown only while it has something in it (owner, 2026-10-20). -->
{#if rows.length > 0}
<section class="panel-section" data-section="servers" aria-label="Servers">
  <h2 class="label">Servers</h2>
  <ul class="rows">
    {#each rows as row (key(row))}
      {@const texts = rowTexts(row)}
      <li class="line row" class:child={row.child}>
        <button class="open" type="button" tabindex="-1" onclick={() => useRow(row)}>
          {#if row.ports > 0}
            <span class="fold" class:turned={leftList.openFolds[foldOf(row.server)]}><Icon name="disclosure" size={10} /></span>
          {:else}
            <Icon name="arrow-up-right" size={12} />
          {/if}
          <span class="name">{texts.label}</span>
          {#if texts.right}<span class="port">{texts.right}</span>{/if}
        </button>
        <button class="stop" type="button" tabindex="-1" title="Stop" aria-label="Stop {texts.label}" onclick={() => stopServer(row.server)}><Icon name="cross" size={12} /></button>
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
  /* A list with no lines between rows, as Background (owner, 2026-10-20). */
  .rows {
    margin-top: calc(8 * var(--px));
  }
  .line {
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    min-height: calc(28 * var(--px));
    font: var(--t-small);
  }
  .child {
    padding-left: calc(18 * var(--px));
  }
  .port {
    color: var(--slate);
  }
  .open {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    text-align: left;
  }
  .open:hover .name {
    text-decoration: underline;
  }
  .name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .port {
    flex: none;
    font-variant-numeric: tabular-nums;
  }
  .fold {
    display: grid;
    place-items: center;
  }
  .fold.turned {
    transform: rotate(90deg);
  }
  .stop {
    flex: none;
    display: grid;
    place-items: center;
    width: calc(20 * var(--px));
    height: calc(20 * var(--px));
    color: var(--slate);
    visibility: hidden;
  }
  .row:hover .stop {
    visibility: visible;
  }
  .stop:hover {
    color: var(--ink);
  }
</style>
