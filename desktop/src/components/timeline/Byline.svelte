<script>
  // The line over a message (CHAT-015; the picture's byline): the author in
  // --t-author capitals, then the time in --t-stamp slate, or in its place a
  // word such as "sending…" (4.4). Over your messages (`end`, on the right)
  // it is mirrored: the time first, the author at the edge (owner,
  // 2026-10-21); the words still read author first.
  // Placed by UserItem, PendingItem and ReplyItem.
  let { author, time = "", note = "", end = false } = $props();
</script>

<p class="byline" class:end>
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
  .byline.end {
    flex-direction: row-reverse;
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
