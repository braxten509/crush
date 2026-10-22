<script module>
  // The probe's menu section (desktop-design 1.9): the entries the open menu
  // draws, and their rects when a driver asks.
  import { probeSection } from "../../lib/probe.js";
  import { ui } from "../../lib/stores/ui.svelte.js";

  let drawn = []; // the open menu's entries: {label, disabled, hint}

  const rectOf = (el) => {
    const r = el.getBoundingClientRect();
    return { x: r.x, y: r.y, w: r.width, h: r.height };
  };

  probeSection(
    "menu",
    () => ({ open: Boolean(ui.menu), items: ui.menu ? drawn : [] }),
    () => {
      const box = document.querySelector("[data-menu]");
      return {
        box: box ? rectOf(box) : null,
        items: [...document.querySelectorAll("[data-menu-item]")].map((el) => ({ label: el.dataset.menuItem, rect: rectOf(el) })),
      };
    },
  );
</script>

<script>
  // The timeline's right-click menu (desktop-design 4.2): our entries only
  // (lib/menu.js, registered by lib/plugins/timeline.js and later phases), at
  // the pointer and flipped to stay inside the window. ↑/↓ move between the
  // entries that are not disabled, Enter runs one, Esc closes; a press outside
  // it, a scroll gesture, the window losing focus or a run closes it (the
  // demo's core.js:337-343). A disabled entry shows its hint under it.
  // Placed by OverlayHost while ui.menu is set: {x, y, ctx} (Timeline).
  import { unfold } from "../../lib/motion.js";
  import { onMount } from "svelte";
  import { bindKey } from "../../lib/keys.js";
  import { menuItems } from "../../lib/menu.js";
  import { closeMenu } from "../../lib/stores/ui.svelte.js";

  let { menu } = $props();

  const entries = $derived(menuItems(menu.ctx ?? {}));
  let box = $state(null);
  let active = $state(-1);
  let place = $state(null); // {x, y} once measured

  $effect(() => {
    drawn = entries.map((e) => ({ label: e.label, disabled: e.disabled, hint: e.disabled ? e.hint : "" }));
  });

  // At the pointer; flipped to the other side of it where the menu would
  // leave the window.
  $effect(() => {
    const { x, y } = menu;
    entries;
    if (!box) return;
    const w = box.offsetWidth;
    const h = box.offsetHeight;
    const left = x + w > window.innerWidth ? Math.max(0, x - w) : x;
    const top = y + h > window.innerHeight ? Math.max(0, y - h) : y;
    place = { x: left, y: top };
  });

  function step(dir) {
    const n = entries.length;
    for (let k = 1; k <= n; k++) {
      const i = (((active < 0 ? (dir > 0 ? -1 : 0) : active) + dir * k) % n + n) % n;
      if (!entries[i].disabled) {
        active = i;
        box?.querySelectorAll("[data-menu-item]")[i]?.focus({ preventScroll: true });
        return true;
      }
    }
    return true;
  }

  function run(i) {
    const entry = entries[i];
    if (!entry || entry.disabled) return false;
    closeMenu();
    entry.run();
    return true;
  }

  onMount(() => {
    const before = document.activeElement;
    box.focus({ preventScroll: true });
    const unbind = [
      bindKey("menu", "ArrowDown", () => step(1)),
      bindKey("menu", "ArrowUp", () => step(-1)),
      bindKey("menu", "Enter", () => (active >= 0 ? run(active) : true)),
      bindKey("menu", "Escape", () => {
        closeMenu();
        return true;
      }),
    ];
    const outside = (event) => {
      if (!box?.contains(event.target)) closeMenu();
    };
    window.addEventListener("mousedown", outside, true);
    window.addEventListener("wheel", closeMenu, { capture: true, passive: true });
    window.addEventListener("blur", closeMenu);
    return () => {
      for (const fn of unbind) fn();
      window.removeEventListener("mousedown", outside, true);
      window.removeEventListener("wheel", closeMenu, { capture: true });
      window.removeEventListener("blur", closeMenu);
      drawn = [];
      // Focus goes back where it was, unless a run moved it on.
      const now = document.activeElement;
      if ((!now || now === document.body || !now.isConnected || box?.contains(now)) && before?.isConnected) before.focus({ preventScroll: true });
    };
  });
</script>

<div
  class="menu"
  role="menu"
  tabindex="-1"
  aria-label="Message actions"
  data-menu
  bind:this={box}
  style:left="{(place ?? menu).x}px"
  style:top="{(place ?? menu).y}px"
  style:visibility={place ? null : "hidden"}
  transition:unfold|global={{ from: "top" }}
>
  {#each entries as entry, i (entry.id)}
    <button
      class="entry"
      class:active={i === active}
      type="button"
      role="menuitem"
      tabindex="-1"
      disabled={entry.disabled}
      data-menu-item={entry.label}
      onmouseenter={() => {
        if (!entry.disabled) active = i;
      }}
      onclick={() => run(i)}
    >
      <span class="label">{entry.label}</span>
      {#if entry.disabled && entry.hint}<span class="hint">{entry.hint}</span>{/if}
    </button>
  {/each}
</div>

<style>
  /* Square, no shadow: paper-raised in a control hairline (4.2). */
  .menu {
    position: fixed;
    z-index: 30;
    box-sizing: border-box;
    min-width: calc(260 * var(--px));
    max-width: calc(360 * var(--px));
    padding: calc(6 * var(--px)) 0;
    background: var(--paper-raised);
    border: 1px solid var(--rule-control);
    outline: none;
  }
  /* 36 tall: a 26 line between 5 above and 5 below. */
  .entry {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    box-sizing: border-box;
    width: 100%;
    min-height: calc(36 * var(--px));
    padding: calc(5 * var(--px)) calc(16 * var(--px));
    border: 0;
    background: none;
    font: var(--t-body-small);
    color: var(--ink);
    text-align: left;
    cursor: default;
  }
  .entry:focus-visible {
    outline: none;
  }
  /* The pointer's row: the pick band with the vermilion edge, as the chat
     list's selected row (owner, 2026-10-21). */
  .entry:not(:disabled):hover,
  .entry.active {
    background: var(--pick);
    box-shadow: inset calc(4 * var(--px)) 0 0 var(--vermilion);
  }
  .entry:disabled {
    color: var(--mark);
  }
  .label,
  .hint {
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .hint {
    font: var(--t-small);
    font-style: italic;
    color: var(--mark);
  }
</style>
