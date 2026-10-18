<script>
  // Placeholder from scaffold S3 (desktop-design 5.1); piece P3.5 replaces it.
  // It draws the selection bar, the key letter and the focus around its item,
  // which comes as its child.
  // Placed by Timeline.
  let { item, selected, focused, children } = $props();
</script>

{@render children?.()}
