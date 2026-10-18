<script>
  // Small drawn icons, sized in picture pixels like everything else. Each is a
  // few strokes in the current colour; none carries meaning on its own, so they
  // are hidden from screen readers and their text sits next to them. `size`
  // (picture pixels) scales an icon whose longer side is not that size, for
  // example a 16 px icon drawn at 12.
  let { name, class: extra = "", size = null } = $props();

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
    check: { w: 16, h: 16 },
    cross: { w: 16, h: 16 },
    dot: { w: 16, h: 16 },
    gear: { w: 16, h: 16 },
    diamond: { w: 16, h: 16 },
    info: { w: 16, h: 16 },
    update: { w: 16, h: 16 },
    close: { w: 16, h: 16 },
    image: { w: 16, h: 16 },
    file: { w: 16, h: 16 },
    text: { w: 16, h: 16 },
    skill: { w: 16, h: 16 },
    "half-circle": { w: 16, h: 16 },
    circle: { w: 16, h: 16 },
    offline: { w: 16, h: 16 },
  };
  const box = $derived(icons[name]);
  const scale = $derived(size ? size / Math.max(box.w, box.h) : 1);
</script>

<svg
  class="icon {extra}"
  style:--w={box.w * scale}
  style:--h={box.h * scale}
  viewBox="0 0 {box.w} {box.h}"
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
  {:else if name === "check"}
    <path d="M2.5 8.5l3.6 3.6L13.5 4" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" />
  {:else if name === "cross"}
    <path d="M4 4l8 8M12 4l-8 8" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" />
  {:else if name === "dot"}
    <circle cx="8" cy="8" r="3" fill="currentColor" />
  {:else if name === "gear"}
    <path d="M6.1 3.05L6.75 .91h2.5l.65 2.14 1.44.83 2.18-.51 1.25 2.17-1.54 1.63v1.66l1.54 1.63-1.25 2.17-2.18-.51-1.44.83-.65 2.14h-2.5l-.65-2.14-1.44-.83-2.18.51-1.25-2.17 1.54-1.63V7.17L1.23 5.54l1.25-2.17 2.18.51z" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" />
    <circle cx="8" cy="8" r="2.2" fill="none" stroke="currentColor" stroke-width="1.6" />
  {:else if name === "diamond"}
    <path d="M8 1.5 14.5 8 8 14.5 1.5 8z" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" />
  {:else if name === "info"}
    <circle cx="8" cy="8" r="6.7" fill="none" stroke="currentColor" stroke-width="1.6" />
    <path d="M8 7.3v4.4" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" />
    <circle cx="8" cy="4.7" r="1" fill="currentColor" />
  {:else if name === "update"}
    <path d="M13.6 8A5.6 5.6 0 1 1 11.96 4.04" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
    <path d="M11.96 .84v3.2H8.76" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
  {:else if name === "close"}
    <path d="M2.5 2.5l11 11M13.5 2.5l-11 11" fill="none" stroke="currentColor" stroke-width="1.6" />
  {:else if name === "image"}
    <path d="M1.5 2.5h13v11h-13z" fill="none" stroke="currentColor" stroke-width="1.6" />
    <path d="M1.5 11.5l4-4 3 3 2-2 4 4" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" />
    <circle cx="10.6" cy="5.9" r="1.3" fill="currentColor" />
  {:else if name === "file"}
    <path d="M3.5 1.5h6l3.5 3.5v9.5h-9.5z M9.5 1.5V5h3.5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" />
  {:else if name === "text"}
    <path d="M2 4h12M2 8h12M2 12h8" fill="none" stroke="currentColor" stroke-width="1.6" />
  {:else if name === "skill"}
    <path d="M8 2.5l6 11H2z" fill="currentColor" />
  {:else if name === "half-circle"}
    <circle cx="8" cy="8" r="6.2" fill="none" stroke="currentColor" stroke-width="1.6" />
    <path d="M8 1.8a6.2 6.2 0 0 0 0 12.4z" fill="currentColor" />
  {:else if name === "circle"}
    <circle cx="8" cy="8" r="6.2" fill="none" stroke="currentColor" stroke-width="1.6" />
  {:else if name === "offline"}
    <circle cx="8" cy="8" r="6.2" fill="none" stroke="currentColor" stroke-width="1.6" />
    <path d="M3.6 3.6l8.8 8.8" fill="none" stroke="currentColor" stroke-width="1.6" />
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
