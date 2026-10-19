<script>
  // SKILLS (SIDE-011): the active and built-in skills minus the disabled ones,
  // sorted by name, one row each (rows.js, a port of skills.go:36-143).
  import PanelSection from "./PanelSection.svelte";
  import StatusIcon from "./StatusIcon.svelte";

  let { list, label = "Skills" } = $props();
</script>

<PanelSection {label} name="skills" rows={list.rows} more={list.more > 0 ? `…and ${list.more} more` : ""}>
  {#snippet row(item)}
    <StatusIcon status={item.status} />
    <span class="name">{item.name}</span>
  {/snippet}
</PanelSection>

<style>
  .name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
