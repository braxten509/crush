<script>
  // The composer: the typing field, the model picker and the send button.
  // Scaffold S3 moved this markup and CSS out of Conversation.svelte unchanged;
  // piece P3.7 draws the typing box for the chat {sessionId}.
  import { sample } from "../../lib/sample.js";
  import Icon from "../Icon.svelte";

  let { sessionId } = $props();

  const conversation = sample.conversation;
</script>

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

<style>
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
