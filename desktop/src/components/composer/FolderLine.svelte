<script>
  // The folder line above the typing box on a new chat, also while the
  // folder asks to be initialized (owner, 2026-10-19, look 1): the folder
  // mark, the folder's name, where it is (home written "~") and a caret. A
  // click opens the folder list (FolderPicker); picking another folder
  // switches this window to it.
  import Icon from "../Icon.svelte";
  import { short } from "../../lib/leftlist/picker.js";
  import { leftList, openPicker } from "../../lib/leftlist/store.svelte.js";
  import { conn } from "../../lib/stores/connection.svelte.js";
  import { baseName } from "../start/startmodel.js";

  const path = $derived(conn.project.path || conn.project.cwd || "");
  const parent = $derived(short(path.slice(0, Math.max(path.lastIndexOf("/"), 1)), leftList.home));
</script>

<button class="folder-line" type="button" tabindex="-1" data-probe="folder-line" title="Start in another folder" onclick={() => openPicker("session")}>
  <Icon name="folder" size={14} />
  <span class="name">{baseName(path)}</span>
  <span class="where">{parent}</span>
  <Icon name="caret" size={10} />
</button>

<style>
  .folder-line {
    align-self: flex-start;
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    min-height: calc(28 * var(--px));
    margin-bottom: calc(6 * var(--px));
    font: var(--t-small);
    color: var(--ink);
  }
  .where {
    color: var(--slate);
  }
  .folder-line:hover .name {
    text-decoration: underline;
  }
</style>
