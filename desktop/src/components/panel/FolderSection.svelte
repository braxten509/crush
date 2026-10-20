<script>
  // FOLDER (owner, 2026-10-19): the project this window shows, its name with
  // the folder mark, then where it is (home written "~") and the Git branch.
  import Icon from "../Icon.svelte";
  import { short } from "../../lib/leftlist/picker.js";
  import { leftList } from "../../lib/leftlist/store.svelte.js";
  import { conn } from "../../lib/stores/connection.svelte.js";
  import { ws } from "../../lib/stores/workspace.svelte.js";
  import { baseName, pathLine } from "../start/startmodel.js";

  const path = $derived(conn.project.path || conn.project.cwd || "");
</script>

<section class="panel-section" data-section="folder" aria-label="Folder">
  <h2 class="label">Folder</h2>
  {#if path}
    <p class="name" title={path}><Icon name="folder" size={14} /><span class="text">{baseName(path)}</span></p>
    <p class="where">{pathLine(short(path, leftList.home), ws.branch ? `branch ${ws.branch}` : "")}</p>
  {:else}
    <p class="where">None</p>
  {/if}
</section>

<style>
  .label {
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
  }
  .name {
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    margin-top: calc(8 * var(--px));
    font: var(--t-small);
    color: var(--ink);
  }
  .text,
  .where {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .where {
    font: var(--t-small);
    color: var(--slate);
  }
</style>
