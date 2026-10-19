<script>
  // A secure entry field (desktop-design-5-6 2.7 "Field"; DLG-191 to
  // DLG-196, DLG-199): KIT's masked Field (bullets, the terminal's editing
  // keys, `[` and `]` typed as text) with the status line under it. The value
  // is bound to the form and lives nowhere else. A paste goes in as it is
  // (validation strips one trailing newline), so a value with a line break
  // or a control character gets the engine's error rather than being changed
  // (DLG-193, DLG-195); the form drops pastes while a sheet covers it
  // (DLG-200). A field the engine saved or can no longer take shows only its
  // status.
  //   value (bindable), status (the status text), locked?, error?
  import { tick } from "svelte";
  import Field from "../kit/Field.svelte";

  let { value = $bindable(""), status = "", locked = false, error = "" } = $props();

  async function onpaste(event) {
    // Before Field's own paste, which would turn line breaks into spaces.
    event.preventDefault();
    event.stopPropagation();
    const input = event.target;
    if (locked || input?.tagName !== "INPUT") return;
    // As it is: the one trailing newline the engine allows is stripped once,
    // at validation and submit (lib/ask/secure.js).
    const text = event.clipboardData?.getData("text/plain") ?? "";
    const start = input.selectionStart ?? value.length;
    const end = input.selectionEnd ?? value.length;
    value = value.slice(0, start) + text + value.slice(end);
    await tick();
    input.setSelectionRange(start + text.length, start + text.length);
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="secure-field" onpastecapture={onpaste} onmousedowncapture={(event) => event.target.tagName === "INPUT" && event.preventDefault()}>
  {#if locked}
    <p class="locked">{status}</p>
  {:else}
    <Field bind:value masked placeholder="Enter secret…" label="Secret value" {error} />
    <p class="status">{status}</p>
  {/if}
</div>

<style>
  p {
    margin: calc(6 * var(--px)) 0 0;
    font: var(--t-small);
    color: var(--slate);
  }
  .locked {
    margin: 0;
    color: var(--ink);
  }
</style>
