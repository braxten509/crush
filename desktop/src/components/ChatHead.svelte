<script>
  // The chat's title and path. Scaffold S3 moved this markup and CSS out of
  // Conversation.svelte unchanged; piece P3.9 draws the shown chat from
  // {session} (P4.1 adds the branch path line in phase 4).
  import { sample } from "../lib/sample.js";

  let { session } = $props();

  const conversation = sample.conversation;
</script>

<header class="head">
  <h2 class="title">{conversation.title}</h2>
  <p class="path" aria-label="Path">{conversation.path.join(" / ")}</p>
</header>

<style>
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
</style>
