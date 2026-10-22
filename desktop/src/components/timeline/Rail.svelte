<script>
  // The vermilion rail of the shown chat (desktop-design 2.1). It runs from
  // the column's top to the top edge of the composer block, measured from
  // the layout: the end follows `.rule.above-composer`'s bottom edge (the
  // composer block starts there), so a taller composer (chips queued, a slot
  // form) never lets the rail run into it. One rail for the column, whichever
  // chat's timeline shows (TimelineStack keeps the others drawn, hidden).
  import { onMount } from "svelte";

  let { column, shown } = $props();

  let rail = $state(null);
  let railBottom = $state(null); // CSS px from the column's bottom to the composer block's top

  function measureRail() {
    const rule = column?.querySelector(":scope > .rule.above-composer");
    if (!column || !rule) {
      railBottom = null;
      return;
    }
    const bottom = column.clientHeight - (rule.offsetTop + rule.offsetHeight);
    if (bottom !== railBottom) railBottom = bottom;
  }

  onMount(() => {
    const sizes = new ResizeObserver(measureRail);
    const watch = () => {
      sizes.disconnect();
      sizes.observe(column);
      for (const part of column.children) if (part !== rail) sizes.observe(part);
    };
    const parts = new MutationObserver(() => {
      watch();
      measureRail();
    });
    parts.observe(column, { childList: true });
    watch();
    measureRail();
    return () => {
      sizes.disconnect();
      parts.disconnect();
    };
  });
</script>

<!-- Only under an open chat: a start screen covers the rail, and rounding
     left its last pixel row showing above the composer (owner, 2026-10-20). -->
{#if shown && railBottom !== null}
  <div class="rail" bind:this={rail} style:bottom="{railBottom}px" aria-hidden="true"></div>
{/if}

<style>
  /* The rail runs from the column's top (chats have no head since
     2026-10-20) to the top of the composer block; no dot at its top (owner,
     2026-10-20). */
  .rail {
    position: absolute;
    left: calc(30 * var(--px));
    top: 0;
    width: calc(3 * var(--px));
    background: var(--vermilion);
    pointer-events: none;
  }
</style>
