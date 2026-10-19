<script>
  // A sheet's help footer (desktop-design-5-6 2.1, DLG-009). Placeholder from
  // scaffold S5 with the pinned props; KIT owns it (7.3).
  //   pairs: [{key, text, disabled?}]
  let { pairs = [] } = $props();
</script>

<p class="help-placeholder">
  {#each pairs.filter((pair) => !pair.disabled) as pair, i (i)}{#if i > 0} · {/if}{pair.key} {pair.text}{/each}
</p>
