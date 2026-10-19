<script>
  // The shared sheet frame (desktop-design-5-6 2.1, look 5.1). Placeholder
  // from scaffold S5 with the pinned props; KIT owns it (7.3).
  //   id, title, width: "small" | "standard" | "wide", maxHeight?, controls?
  //   (a snippet beside the title), help: [{key, text, disabled?}], onKey?
  //   (the front sheet's keys), docked?, children
  let { id, title, width = "standard", maxHeight = null, controls = null, help = [], onKey = null, docked = false, children } = $props();
</script>

<div class="sheet-placeholder" role="dialog" aria-label={title} data-sheet={id} data-width={width} data-docked={docked ? "true" : undefined}>
  {@render children?.()}
</div>
