<script>
  // FOLDER (owner, 2026-10-19): the project this window shows, its name with
  // the folder mark and a caret, then where it is (home written "~") and the
  // Git branch. The name opens the folder list (FolderPicker; owner,
  // 2026-10-20): picking another folder switches this window to it.
  import Icon from "../Icon.svelte";
  import { short } from "../../lib/leftlist/picker.js";
  import { leftList, openPicker } from "../../lib/leftlist/store.svelte.js";
  import { conn } from "../../lib/stores/connection.svelte.js";
  import { ws } from "../../lib/stores/workspace.svelte.js";
  import { baseName, pathLine } from "../start/startmodel.js";

  const path = $derived(conn.project.path || conn.project.cwd || "");
</script>

<section class="panel-section" data-section="folder" aria-label="Folder">
  <h2 class="label">Folder</h2>
  {#if path}
    <button class="name" type="button" tabindex="-1" title="Switch to another folder" data-probe="folder-picker" onclick={() => openPicker("session")}>
      <Icon name="folder" size={14} /><span class="text">{baseName(path)}</span><Icon name="caret" size={10} />
    </button>
    <p class="where" title={path}>{pathLine(short(path, leftList.home), ws.branch ? `branch ${ws.branch}` : "")}</p>
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
    max-width: 100%;
    margin-top: calc(8 * var(--px));
    font: var(--t-small);
    color: var(--ink);
    text-align: left;
  }
  .name:hover .text {
    text-decoration: underline;
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
