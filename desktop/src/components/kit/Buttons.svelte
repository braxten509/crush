<script>
  // A row of sheet buttons (desktop-design-5-6 2.1, DLG-020). Placeholder
  // from scaffold S5 with the pinned props; KIT owns it (7.3).
  //   buttons: [{id, label, key?, destructive?}], selected (bindable), onPress(id), stack?
  let { buttons = [], selected = $bindable(null), onPress = () => {}, stack = false } = $props();
</script>

<div class="buttons-placeholder">
  {#each buttons as button (button.id)}
    <button type="button" onclick={() => onPress(button.id)}>{button.label}</button>
  {/each}
</div>
