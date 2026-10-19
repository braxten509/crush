<script>
  // Above the list's end rule while chats are closed (4.8, 3.3): "N closed ·
  // links reopen them". Phase 4 closes chats (P4.1); until then N stays 0 and
  // nothing shows.
  let { count = 0 } = $props();
</script>

{#if count > 0}
  <p class="footer">{count} closed · links reopen them</p>
{/if}

<style>
  .footer {
    margin: calc(14 * var(--px)) calc(20 * var(--px)) calc(14 * var(--px));
    font: var(--t-small);
    color: var(--slate);
  }
</style>
