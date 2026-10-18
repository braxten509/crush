<script>
  // A one-pixel rule across a column. `marks` puts small + marks at the ends of a
  // section boundary: "both", or "start" for a rule that runs on into the divider.
  // `caret` lifts a small ^ out of the line at that many picture pixels from the
  // rule's left end, pointing at the heading above it.
  import PlusMark from "./PlusMark.svelte";

  let { marks = null, caret = null, class: extra = "" } = $props();
</script>

<div
  class="rule {extra}"
  class:marks-both={marks === "both"}
  class:marks-start={marks === "start"}
  class:has-caret={caret !== null}
  style:--caret={caret}
  aria-hidden="true"
>
  {#if marks}
    <PlusMark class="start" />
  {/if}
  {#if marks === "both"}
    <PlusMark class="end" />
  {/if}
  {#if caret !== null}
    <span class="line before"></span>
    <svg class="caret" viewBox="0 0 13 6" preserveAspectRatio="none" focusable="false">
      <path d="M0 5.5 6.5 0.5 13 5.5" fill="none" stroke="currentColor" stroke-width="1" vector-effect="non-scaling-stroke" />
    </svg>
    <span class="line after"></span>
  {:else}
    <span class="line"></span>
  {/if}
</div>

<style>
  .rule {
    position: relative;
    flex: none;
    height: 1px;
    color: var(--rule);
  }
  .line {
    position: absolute;
    top: 0;
    height: 1px;
    left: 0;
    right: 0;
    background: currentColor;
  }
  /* With + marks the line stops short of them: marks sit 11 picture pixels in from
     the left edge and 9 in from the right, and the line keeps 9 clear of each. */
  .marks-both .line,
  .marks-start .line {
    left: calc(20 * var(--px));
  }
  .marks-both .line {
    right: calc(19 * var(--px));
  }
  .rule :global(.plus.start) {
    left: calc(11 * var(--px));
    top: 0;
  }
  .rule :global(.plus.end) {
    right: calc(9 * var(--px));
    top: 0;
  }
  .has-caret .before {
    right: auto;
    width: calc((var(--caret) - 6) * var(--px));
  }
  .has-caret .after {
    left: calc((var(--caret) + 7) * var(--px));
  }
  .caret {
    position: absolute;
    left: calc((var(--caret) - 6.5) * var(--px));
    top: calc(-5 * var(--px));
    width: calc(13 * var(--px));
    height: calc(6 * var(--px));
    overflow: visible;
  }
</style>
