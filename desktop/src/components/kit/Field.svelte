<script>
  // A one-line field (desktop-design-5-6 2.1, DLG-013 to DLG-017).
  // Placeholder from scaffold S5 with the pinned props; KIT owns it (7.3).
  //   value (bindable), placeholder?, prompt?, masked?, error?, reserved?: string[]
  let { value = $bindable(""), placeholder = "", prompt = "", masked = false, error = "", reserved = [] } = $props();
</script>

<input class="field-placeholder" type={masked ? "password" : "text"} {placeholder} bind:value />
