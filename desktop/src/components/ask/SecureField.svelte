<script>
  // A secure entry field (desktop-design-5-6 2.7 "Field"; DLG-191 to
  // DLG-196, DLG-199): KIT's masked Field (bullets, the terminal's editing
  // keys, `[` and `]` typed as text) with the status line under it. The value
  // is bound to the form and lives nowhere else. While a sheet covers the
  // window a paste is dropped (DLG-200). A saved field takes no typing.
  //   value (bindable), status (the status text), saved?, error?
  import Field from "../kit/Field.svelte";
  import { dialogs } from "../../lib/dialogs.js";

  let { value = $bindable(""), status = "", saved = false, error = "" } = $props();

  function onpaste(event) {
    if (dialogs.length > 0 || saved) {
      event.preventDefault();
      event.stopPropagation();
    }
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="secure-field" onpastecapture={onpaste} onmousedowncapture={(event) => event.target.tagName === "INPUT" && event.preventDefault()}>
  {#if saved}
    <p class="saved">Saved</p>
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
  .saved {
    margin: 0;
    color: var(--ink);
  }
</style>
