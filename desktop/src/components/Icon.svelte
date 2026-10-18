<script>
  // Small drawn icons, sized in picture pixels like everything else. Each is a
  // few strokes in the current colour; none carries meaning on its own, so they
  // are hidden from screen readers and their text sits next to them.
  let { name, class: extra = "" } = $props();

  const icons = {
    plus: { w: 20, h: 20 },
    search: { w: 20, h: 20 },
    working: { w: 18, h: 18 },
    clock: { w: 20, h: 20 },
    warning: { w: 25, h: 24 },
    send: { w: 31, h: 25 },
    chevron: { w: 13, h: 7 },
    disclosure: { w: 8, h: 14 },
    caret: { w: 12, h: 7 },
    arrowhead: { w: 9, h: 13 },
  };
  const size = $derived(icons[name]);
</script>

<svg
  class="icon {extra}"
  style:--w={size.w}
  style:--h={size.h}
  viewBox="0 0 {size.w} {size.h}"
  aria-hidden="true"
  focusable="false"
>
  {#if name === "plus"}
    <path d="M10 1v18M1 10h18" fill="none" stroke="currentColor" stroke-width="1.75" />
  {:else if name === "search"}
    <circle cx="7.75" cy="7.75" r="6.75" fill="none" stroke="currentColor" stroke-width="1.75" />
    <path d="M12.6 12.6 19 19" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" />
  {:else if name === "working"}
    <circle cx="9" cy="9" r="7.6" fill="none" stroke="currentColor" stroke-width="1.6" />
  {:else if name === "clock"}
    <circle cx="10" cy="10" r="8.6" fill="none" stroke="currentColor" stroke-width="1.6" />
    <path d="M10 5.2V10l3.4 2.3" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
  {:else if name === "warning"}
    <path d="M10.9 1.6a1.8 1.8 0 0 1 3.2 0l10.5 19.6a1.8 1.8 0 0 1-1.6 2.6H2a1.8 1.8 0 0 1-1.6-2.6z" fill="var(--amber)" />
    <path d="M12.5 8.4v7" fill="none" stroke="var(--paper)" stroke-width="2.4" stroke-linecap="round" />
    <circle cx="12.5" cy="19.4" r="1.35" fill="var(--paper)" />
  {:else if name === "send"}
    <path d="M29.6 1.4 1.6 9.3l10.8 4.4 4.8 10.1z M29.6 1.4 12.4 13.7" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round" stroke-linecap="round" />
  {:else if name === "chevron"}
    <path d="M1 1l5.5 5 5.5-5" fill="none" stroke="currentColor" stroke-width="1.9" />
  {:else if name === "disclosure"}
    <path d="M0 0l8 7-8 7z" fill="currentColor" />
  {:else if name === "caret"}
    <path d="M0 .5h12L6 6.5z" fill="currentColor" />
  {:else if name === "arrowhead"}
    <path d="M0 0l9 6.5L0 13z" fill="currentColor" />
  {/if}
</svg>

<style>
  .icon {
    display: block;
    flex: none;
    width: calc(var(--w) * var(--px));
    height: calc(var(--h) * var(--px));
    overflow: visible;
  }
</style>
