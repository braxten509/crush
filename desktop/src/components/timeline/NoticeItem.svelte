<script>
  // A notice in the chat (CHAT-008 to CHAT-011; desktop-design 4.4): a loaded
  // skill, a task's result, the answers to a question form or a system
  // notice, as one line in --t-small slate after a calc(16 * var(--px)) drawn
  // icon, cut with "…", aligned with replies, no box. The name (or the
  // answer's state) is in ink at weight 600.
  // Placed by Timeline.
  // The answers line opens your answers in a pop-up (owner, 2026-10-20).
  import Icon from "../Icon.svelte";
  import { answersView } from "../../lib/answers.js";
  import { closeDialog, openDialog } from "../../lib/dialogs.js";
  import { chatOf, messages } from "../../lib/stores/messages.svelte.js";
  import { noticeParts, textOf } from "../../lib/timeline.js";

  let { item } = $props();

  const parts = $derived(noticeParts(item));

  // The line's message and those before it, newest first (where the
  // questions were asked).
  function showAnswers() {
    const chat = messages.bySession[item.sessionId] ?? messages.bySession[chatOf(item.messageId)];
    const at = chat ? chat.ids.indexOf(item.messageId) : -1;
    const message = at < 0 ? null : chat.byId.get(item.messageId);
    const earlier = at < 0 ? [] : chat.ids.slice(0, at).reverse().map((id) => chat.byId.get(id));
    closeDialog("answers");
    openDialog("answers", { answers: answersView(message ? textOf(message) : "", earlier), state: item.state });
  }
</script>

{#if item.notice === "answers"}
  <button type="button" class="notice open" data-notice={item.notice} data-state={item.state} data-answers-open onclick={showAnswers}>
    <span class="icon {parts.tone}"><Icon name={parts.icon} size={16} /></span>
    <span class="line">{parts.before}{#if parts.strong}<b>{parts.strong}</b>{/if}{parts.after}</span>
  </button>
{:else}
  <p class="notice" data-notice={item.notice} data-state={item.state}>
    <span class="icon {parts.tone}"><Icon name={parts.icon} size={16} /></span>
    <span class="line">{parts.before}{#if parts.strong}<b>{parts.strong}</b>{/if}{parts.after}</span>
  </p>
{/if}

<style>
  .notice {
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    min-width: 0;
    font: var(--t-small);
    color: var(--slate);
  }
  .open {
    width: 100%;
    margin: 0;
    padding: 0;
    border: 0;
    background: none;
    text-align: left;
    cursor: pointer;
  }
  .open:hover .line {
    text-decoration: underline;
    text-underline-offset: calc(3 * var(--px));
  }
  .open:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
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
