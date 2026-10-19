<script>
  // A reply (CHAT-014 to CHAT-018, CHAT-031 to CHAT-034): the byline (the
  // agent's name and the time), then the stored Markdown through the safe
  // renderer (MarkdownView, P3.4). A plan sits in a square box: 1px
  // --rule-control with + marks at its corners, "PLAN" in small caps set into
  // the top rule, padding calc(17 * var(--px)) calc(20 * var(--px)), no fill;
  // while it streams in plan mode the box has no bottom rule (CHAT-032).
  // The byline heads the item, never the plan's inner text. One MarkdownView
  // stays in place when the box opens or closes, so its renderer keeps its
  // stable prefix.
  // Placed by Timeline.
  import PlusMark from "../PlusMark.svelte";
  import MarkdownView from "../render/MarkdownView.svelte";
  import { clockTime } from "../../lib/timefmt.js";
  import Byline from "./Byline.svelte";

  let { item } = $props();

  const boxed = $derived(item.plan === "open" || item.plan === "closed");
  const open = $derived(item.plan === "open");
</script>

<div class="reply">
  <Byline author={item.author} time={clockTime(item.time)} />
  <div class="body" class:plan={boxed} class:open data-plan={item.plan}>
    {#if boxed}
      <span class="label">Plan</span>
      <PlusMark class="corner top-left" />
      <PlusMark class="corner top-right" />
      {#if !open}
        <PlusMark class="corner bottom-left" />
        <PlusMark class="corner bottom-right" />
      {/if}
    {/if}
    <MarkdownView text={item.text} messageId={item.messageId} sessionId={item.sessionId} finished={item.finished} plan={item.plan} />
  </div>
</div>

<style>
  /* The reply is the container its plan box measures (CHAT-034, below). */
  .reply {
    container: reply / inline-size;
  }
  .body {
    position: relative;
    margin-top: calc(13 * var(--px));
  }

  .plan {
    border: 1px solid var(--rule-control);
    padding: calc(17 * var(--px)) calc(20 * var(--px));
  }
  .plan.open {
    border-bottom: 0;
  }
  /* The label interrupts the top rule; the paper behind it hides the line. */
  .label {
    position: absolute;
    left: calc(14 * var(--px));
    top: calc(-11 * var(--px));
    padding: 0 calc(6 * var(--px));
    background: var(--paper);
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
    color: var(--ink);
    user-select: none;
  }
  .body :global(.plus.corner) {
    z-index: 1;
  }
  .body :global(.plus.top-left) {
    left: -1px;
    top: -1px;
  }
  .body :global(.plus.top-right) {
    right: -1px;
    top: -1px;
  }
  .body :global(.plus.bottom-left) {
    left: -1px;
    bottom: -1px;
  }
  .body :global(.plus.bottom-right) {
    right: -1px;
    bottom: -1px;
  }

  /* Plan headings (CHAT-034): H1 and H2 as in any reply, H3 to H5 indented,
     inline code at weight 600. */
  .body :global(.md-plan > :is(h3, h4, h5)) {
    padding-left: calc(16 * var(--px));
  }
  .body :global(.md-plan .md-icode) {
    font-weight: 600;
  }

  /* The terminal drops the box's padding when its frame no longer fits the
     width (assistant_render.go:70-78); 2.625rem is the frame, 42 picture px. */
  @container reply (max-width: 2.625rem) {
    .plan {
      padding-left: 0;
      padding-right: 0;
    }
  }
</style>
