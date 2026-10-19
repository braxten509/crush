<script>
  // Where a start screen draws: the band between the head's rule and the
  // composer's rule, where the timeline is (desktop-design 2.1; Conversation
  // places the start screens in the timeline's place). It lies over the
  // timeline on the paper, so nothing the timeline draws without a chat shows
  // through, and it leaves the notice line's band free while a notice shows
  // (SIDE-028: the notice sits over the timeline's bottom, on the composer
  // rule). The band is measured from the two rules, which Conversation.svelte
  // names under-head and above-composer, whenever any part of the column
  // changes size or a part comes or goes.
  import { onMount } from "svelte";
  import { ui } from "../../lib/stores/ui.svelte.js";

  let { kind, label, children } = $props();

  let layer = $state(null);
  let band = $state(null); // {top, bottom} in CSS px within the conversation column

  function measure() {
    const column = layer?.parentElement;
    const head = column?.querySelector(":scope > .rule.under-head");
    const foot = column?.querySelector(":scope > .rule.above-composer");
    if (!head || !foot) {
      band = null;
      return;
    }
    const top = head.offsetTop + head.offsetHeight;
    const bottom = column.clientHeight - foot.offsetTop;
    if (band?.top !== top || band?.bottom !== bottom) band = { top, bottom };
  }

  onMount(() => {
    const column = layer.parentElement;
    const sizes = new ResizeObserver(measure);
    const watch = () => {
      sizes.disconnect();
      sizes.observe(column);
      for (const part of column.children) if (part !== layer) sizes.observe(part);
    };
    const parts = new MutationObserver(() => {
      watch();
      measure();
    });
    parts.observe(column, { childList: true });
    watch();
    measure();
    return () => {
      sizes.disconnect();
      parts.disconnect();
    };
  });
</script>

<section
  class="start-layer {kind}"
  class:notice={ui.notice !== null}
  bind:this={layer}
  style:--band-top={band ? `${band.top}px` : null}
  style:--band-bottom={band ? `${band.bottom}px` : null}
  aria-label={label}
>
  {@render children?.()}
</section>

<style>
  .start-layer {
    /* Until measured: the built head and composer heights plus their rules. */
    --band-top: calc(121 * var(--px) + 1px);
    --band-bottom: calc(98 * var(--px) + 1px);
    position: absolute;
    left: 0;
    right: 0;
    top: var(--band-top);
    bottom: var(--band-bottom);
    z-index: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    background: var(--paper);
  }
  .start-layer.notice {
    bottom: calc(var(--band-bottom) + 40 * var(--px));
  }
</style>
