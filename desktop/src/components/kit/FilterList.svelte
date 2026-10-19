<script>
  // A filtered list (desktop-design-5-6 2.1, DLG-010 to DLG-012). Placeholder
  // from scaffold S5 with the pinned props; KIT owns it (7.3).
  //   items, filterText(item), query, selected (bindable), infoBudget?,
  //   groups?, row (a snippet: row(item, index))
  let { items = [], filterText = (item) => String(item), query = "", selected = $bindable(0), infoBudget = null, groups = null, row } = $props();
</script>

<ul class="filter-list-placeholder">
  {#each items as item, i (i)}
    <li>{@render row?.(item, i)}</li>
  {/each}
</ul>
