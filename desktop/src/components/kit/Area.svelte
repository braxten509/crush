<script>
  // A multiline editor (desktop-design-5-6 2.1, DLG-018, DLG-019).
  // Placeholder from scaffold S5 with the pinned props; KIT owns it (7.3).
  //   value (bindable), minLines, maxLines, reserved?: string[]
  let { value = $bindable(""), minLines = 1, maxLines = 10, reserved = [] } = $props();
</script>

<textarea class="area-placeholder" rows={minLines} bind:value></textarea>
