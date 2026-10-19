<script>
  // The line over a message (CHAT-015; the picture's byline): the author in
  // --t-author capitals, then the time in --t-stamp slate, or in its place a
  // word such as "sending…" (4.4).
  // Placed by UserItem, PendingItem and ReplyItem.
  let { author, time = "", note = "" } = $props();
</script>

<p class="byline">
  <span class="author">{author}</span>
  {#if time}<time>{time}</time>{:else if note}<span class="note">{note}</span>{/if}
</p>

<style>
  .byline {
    display: flex;
    align-items: baseline;
    height: calc(24 * var(--px));
    gap: calc(20 * var(--px));
    margin-bottom: calc(5 * var(--px));
    white-space: nowrap;
  }
  .author {
    font: var(--t-author);
    text-transform: uppercase;
    letter-spacing: 0.01em;
  }
  time,
  .note {
    font: var(--t-stamp);
    color: var(--slate);
  }
</style>
