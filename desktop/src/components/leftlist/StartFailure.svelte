<script>
  // The list's bottom line when a start failed (desktop-design-5-6 2.21,
  // LIST-048): "Couldn't start a chat." (internal/ui/sessionhost/
  // host.go:322-329) or "Couldn't start the terminal." (terminal.go:24-30),
  // above the server rows, never in the strip, cleared by the next attempt.
  let { text } = $props();
</script>

<p class="start-failure" role="status">{text}</p>

<style>
  /* A warning, in the warning text color, at the list's text start. */
  .start-failure {
    margin: calc(14 * var(--px)) calc(20 * var(--px)) 0 calc(23 * var(--px));
    font: var(--t-small);
    color: var(--amber-ink);
  }
</style>
