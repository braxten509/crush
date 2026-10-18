<script>
  // The middle column: the chat's title and path, the timeline of messages and
  // folded tool runs on a vermilion rail with lettered keys, and the composer.
  import Icon from "./Icon.svelte";
  import Rule from "./Rule.svelte";

  let { conversation } = $props();

  const authorLabel = (author) => (author === "you" ? "You" : author === "claude" ? "Claude" : author);
</script>

<main class="conversation" aria-label={conversation.title}>
  <header class="head">
    <h2 class="title">{conversation.title}</h2>
    <p class="path" aria-label="Path">{conversation.path.join(" / ")}</p>
  </header>

  <Rule marks="both" class="under-head" />

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
          <button class="tool-row" type="button" aria-expanded="false">
            <Icon name="disclosure" />
            <span class="line"><span class="count">Ran {item.count} tools</span><span class="dot" aria-hidden="true"></span><span class="summary">{item.summary}</span></span>
          </button>
        </li>
      {/if}
    {/each}
  </ol>

  <Rule marks="both" class="above-composer" />

  <form class="composer" onsubmit={(event) => event.preventDefault()}>
    <div class="field">
      <textarea rows="1" placeholder="Ask {conversation.agent}…" aria-label="Message {conversation.agent}"></textarea>
      <span class="divider" aria-hidden="true"></span>
      <button class="model" type="button" aria-label="Model: {conversation.model}">
        <span>{conversation.model}</span>
        <Icon name="caret" />
      </button>
    </div>
    <button class="send" type="submit" aria-label="Send">
      <Icon name="send" />
    </button>
  </form>
</main>

<style>
  /* Vertical positions follow the approved picture at 12/12 (column top = 0). */
  .conversation {
    position: relative;
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
  }

  .head {
    padding: calc(17 * var(--px)) calc(30 * var(--px)) 0;
    height: calc(121 * var(--px));
    box-sizing: border-box;
    flex: none;
  }
  .title {
    font: var(--t-title);
    letter-spacing: -0.02em;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .path {
    margin-top: calc(11 * var(--px));
    font: var(--t-path);
    letter-spacing: var(--track-path);
    text-transform: uppercase;
    color: var(--slate);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .conversation :global(.rule.under-head),
  .conversation :global(.rule.above-composer) {
    flex: none;
    z-index: 1;
  }

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
  .tool-row {
    display: flex;
    align-items: center;
    box-sizing: border-box;
    width: 100%;
    height: calc(44 * var(--px));
    padding: 0 calc(12 * var(--px)) 0 calc(21 * var(--px));
    border: 1px solid var(--rule-control);
    font: var(--t-body-small);
  }
  /* One line of text, so the dot can sit on the letters' middle and a long
     summary ends in an ellipsis. */
  .tool-row .line {
    flex: 1;
    min-width: 0;
    margin-left: calc(15 * var(--px));
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    text-align: left;
  }
  .tool-row .count {
    letter-spacing: -0.01em;
  }
  .tools .key {
    top: calc(1 * var(--px));
  }
  /* vertical-align: middle centres the dot half an x-height above the
     baseline, read from the font itself, in every renderer and at every zoom. */
  .tool-row .dot {
    display: inline-block;
    vertical-align: middle;
    width: calc(4 * var(--px));
    height: calc(4 * var(--px));
    margin: 0 calc(10 * var(--px)) 0 calc(9 * var(--px));
    border-radius: 50%;
    background: var(--ink);
  }
  .tool-row .summary {
    color: var(--slate);
  }

  .composer {
    flex: none;
    display: flex;
    gap: calc(6 * var(--px));
    height: calc(98 * var(--px));
    box-sizing: border-box;
    padding: calc(15 * var(--px)) calc(20 * var(--px)) calc(25 * var(--px));
  }
  .field {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: flex-start;
    border: 1px solid var(--rule-control);
  }
  .field textarea {
    flex: 1;
    min-width: 0;
    height: calc(26 * var(--px));
    margin: calc(16 * var(--px)) 0 0 calc(14 * var(--px));
    padding: 0;
    border: 0;
    outline: 0;
    background: none;
    resize: none;
    overflow: hidden;
    font: var(--t-composer);
    color: var(--ink);
  }
  .field textarea::placeholder {
    color: var(--slate);
    opacity: 1;
  }
  .field:focus-within {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }
  .divider {
    align-self: stretch;
    width: 1px;
    margin: calc(7 * var(--px)) 0;
    background: var(--rule);
  }
  .model {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: calc(14 * var(--px));
    min-width: calc(163 * var(--px));
    box-sizing: border-box;
    align-self: stretch;
    padding: 0 calc(13 * var(--px)) 0 calc(14 * var(--px));
    font: var(--t-small);
    color: var(--ink);
  }
  .send {
    flex: none;
    width: calc(55 * var(--px));
    display: grid;
    place-items: center;
    background: var(--vermilion-fill);
    color: var(--paper);
    border-radius: calc(2 * var(--px));
  }
  .send :global(.icon) {
    /* A paper plane reads centred a little right of its box. */
    transform: translateX(calc(3 * var(--px)));
  }
</style>
