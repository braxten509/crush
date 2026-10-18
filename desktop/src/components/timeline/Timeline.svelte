<script>
  // The timeline of messages and folded tool runs on a vermilion rail with
  // lettered keys. Scaffold S3 moved this markup and CSS out of
  // Conversation.svelte unchanged; piece P3.5 draws the chat {sessionId}.
  import { sample } from "../../lib/sample.js";
  import ToolGroup from "../tools/ToolGroup.svelte";

  let { sessionId } = $props();

  const conversation = sample.conversation;
  const authorLabel = (author) => (author === "you" ? "You" : author === "claude" ? "Claude" : author);
</script>

<div class="rail" aria-hidden="true"></div>

<ol class="timeline" aria-label="Messages">
  {#each conversation.timeline as item (item.id)}
    {#if item.kind === "message"}
      <li class="message {item.author}">
        <p class="byline">
          <span class="author">{authorLabel(item.author)}</span>
          <time>{item.time}</time>
        </p>
        <div class="body" class:boxed={item.author === "you"}>
          {#if item.key}<span class="key" aria-label="Key {item.key}">{item.key}</span>{/if}
          <p>
            {#each item.parts as part, i (i)}
              {#if part.href}<a href={part.href}>{part.text}</a>{:else}{part.text}{/if}
            {/each}
          </p>
        </div>
      </li>
    {:else if item.kind === "tools"}
      <li class="tools">
        {#if item.key}<span class="key" aria-label="Key {item.key}">{item.key}</span>{/if}
        <ToolGroup {item} />
      </li>
    {/if}
  {/each}
</ol>

<style>
  /* The rail runs from the head's rule to the composer's rule. */
  .rail {
    position: absolute;
    left: calc(30 * var(--px));
    top: calc(121 * var(--px));
    bottom: calc(98 * var(--px));
    width: calc(3 * var(--px));
    background: var(--vermilion);
    pointer-events: none;
  }
  .rail::before {
    content: "";
    position: absolute;
    left: calc(-2 * var(--px));
    top: calc(1 * var(--px) + 1px);
    width: calc(7 * var(--px));
    height: calc(7 * var(--px));
    border-radius: 50%;
    background: var(--vermilion);
  }

  .timeline {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    scrollbar-width: none;
    padding: calc(24 * var(--px)) calc(23 * var(--px)) calc(24 * var(--px)) calc(66 * var(--px));
  }
  /* The rhythm between entries: 40 by default, more air after your own message,
     and a tool run sits a little closer to the reply that ran it. */
  .timeline > li + li {
    margin-top: calc(40 * var(--px));
  }
  .timeline > .you + li {
    margin-top: calc(44 * var(--px));
  }
  .timeline > li + .tools {
    margin-top: calc(37 * var(--px));
  }
  .timeline > .tools + li {
    margin-top: calc(39 * var(--px));
  }

  .message {
    position: relative;
  }
  .byline {
    display: flex;
    align-items: baseline;
    height: calc(24 * var(--px));
    gap: calc(20 * var(--px));
    margin-bottom: calc(5 * var(--px));
  }
  .author {
    font: var(--t-author);
    text-transform: uppercase;
    letter-spacing: 0.01em;
  }
  .byline time {
    font: var(--t-stamp);
    color: var(--slate);
  }
  .body {
    position: relative;
    font: var(--t-reading);
  }
  .claude .body {
    margin-top: calc(13 * var(--px));
    max-width: calc(540 * var(--px));
  }
  .boxed {
    border: 1px solid var(--rule-faint);
    padding: calc(17 * var(--px)) calc(12 * var(--px)) calc(12 * var(--px));
  }
  .body a {
    color: var(--link);
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }

  /* A key on the rail marks where the chat branched or tools ran. */
  .key {
    position: absolute;
    left: calc(-48 * var(--px));
    top: 0;
    box-sizing: border-box;
    width: calc(27 * var(--px));
    height: calc(29 * var(--px));
    border: calc(2 * var(--px)) solid var(--vermilion);
    background: var(--vermilion-wash);
    color: var(--vermilion-ink);
    font: var(--t-key);
    text-align: center;
    display: grid;
    place-items: center;
  }

  .tools {
    position: relative;
  }
  .tools .key {
    top: calc(1 * var(--px));
  }
</style>
