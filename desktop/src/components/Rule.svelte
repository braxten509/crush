<script>
  // A one-pixel rule across a column. `marks` keeps clear ends on a section
  // boundary, where + marks were until 2026-10-20 (owner: none now): "both",
  // or "start" for a rule that runs on into the divider.
  // `caret` lifts a small ^ out of the line at that many picture pixels from the
  // rule's left end, pointing at the heading above it.
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
  /* With clear ends the line starts 20 picture pixels in from the left edge and
     stops 19 short of the right. */
  .marks-both .line,
  .marks-start .line {
    left: calc(20 * var(--px));
  }
  .marks-both .line {
    right: calc(19 * var(--px));
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
