<script>
  // A notice in the chat (CHAT-008 to CHAT-011; desktop-design 4.4): a loaded
  // skill, a task's result, the answers to a question form or a system
  // notice, as one line in --t-small slate after a calc(16 * var(--px)) drawn
  // icon, cut with "…", aligned with replies, no box. The name (or the
  // answer's state) is in ink at weight 600.
  // Placed by Timeline.
  import Icon from "../Icon.svelte";
  import { noticeParts } from "../../lib/timeline.js";

  let { item } = $props();

  const parts = $derived(noticeParts(item));
</script>

<p class="notice" data-notice={item.notice} data-state={item.state}>
  <span class="icon {parts.tone}"><Icon name={parts.icon} size={16} /></span>
  <span class="line">{parts.before}{#if parts.strong}<b>{parts.strong}</b>{/if}{parts.after}</span>
</p>

<style>
  .notice {
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    min-width: 0;
    font: var(--t-small);
    color: var(--slate);
  }
  .icon {
    flex: none;
    display: grid;
    place-items: center;
  }
  .ink {
    color: var(--ink);
  }
  .muted {
    color: var(--slate);
  }
  .error {
    color: var(--error-ink);
  }
  .line {
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  b {
    font-weight: 600;
    color: var(--ink);
  }
</style>
