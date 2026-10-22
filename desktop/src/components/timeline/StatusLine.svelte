<script>
  // A live status (desktop-design 4.10; CHAT-019 to CHAT-022, CHAT-029,
  // CHAT-085): the activity mark, the words in --t-small slate, " · 12s" in
  // --t-stamp once 10 s have passed (one shared 1 s clock, lib/timefmt.js),
  // then the thought peek in italic slate, all on one line cut with "…".
  // Thinking itself is never drawn (CHAT-023). A tool group's live status
  // (its key starts "status:group:") uses the small mark (CHAT-104).
  // Placed by Timeline; ToolGroup (P3.6) for a working group's live status.
  import { clockNow, elapsedSince } from "../../lib/timefmt.js";
  import ActivityMark from "./ActivityMark.svelte";

  let { item } = $props();

  const size = $derived(item.key?.startsWith("status:group:") ? "small" : "line");
  const elapsed = $derived(elapsedSince(item.startedAt, clockNow()));
</script>

<p class="status {size}" data-words={item.words}>
  <ActivityMark {size} /><span class="words">{item.words}</span>{#if elapsed}<span class="elapsed">{" · "}{elapsed}</span>{/if}{#if item.thought}<span class="thought">{" · "}{item.thought}</span>{/if}
</p>

<style>
  .status {
    min-width: 0;
    font: var(--t-small);
    color: var(--slate);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .status :global(.mark) {
    margin-right: calc(10 * var(--px));
  }
  .status.small :global(.mark) {
    margin-right: calc(8 * var(--px));
  }
  .elapsed {
    font: var(--t-stamp);
  }
  .thought {
    font-style: italic;
  }
</style>
