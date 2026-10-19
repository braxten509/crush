<script>
  // FILES (SIDE-008): the shown chat's changed files, newest change first,
  // each path relative to the project and trimmed to its last two folders,
  // with "+N" and "−N" right-aligned as in the tool rows (4.6, 4.13);
  // unchanged files are left out (lib/modified.js, a port of
  // session.go:151-299). ws.files follows `file` events, so the rows do too.
  import PanelSection from "./PanelSection.svelte";

  let { list, label = "Files" } = $props();
</script>

<PanelSection {label} name="files" rows={list.rows} more={list.more}>
  {#snippet row(item)}
    <span class="path" title={item.fullPath}>{item.path}</span>
    <span class="counts">
      <span class="add">+{item.additions}</span>
      <span class="del">−{item.deletions}</span>
    </span>
  {/snippet}
</PanelSection>

<style>
  .path {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .counts {
    display: flex;
    gap: calc(8 * var(--px));
    margin-left: auto;
    flex: none;
  }
  .add {
    color: var(--diff-add-ink);
  }
  .del {
    color: var(--error-ink);
  }
</style>
