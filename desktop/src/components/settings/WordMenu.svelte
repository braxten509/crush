<script>
  // A word of a fallback block's sentence that opens a menu under it
  // (Settings > Fallback order, owner 2026-10-21, look C). The word sits in
  // the sentence's own flow with a dotted underline; a click, Enter, Space or
  // ↓ opens the menu the page draws (children(done)). ↑/↓ move between its
  // items, Esc closes it and gives the word focus back, a press outside or
  // focus leaving closes it. It unrolls from the word and rolls back up
  // (lib/motion.js unfold). The page holds which one is open (open,
  // ontoggle), so only one is; done() closes it after a pick.
  import { tick } from "svelte";
  import { bindKey } from "../../lib/keys.js";
  import { unfold } from "../../lib/motion.js";

  let { text, label, open = false, ontoggle, children } = $props();

  const GAP = 4; // CSS px between the word and its menu
  const MARGIN = 8; // CSS px the menu keeps from the window's edges

  let root = $state(null);
  let word = $state(null);
  let menu = $state(null);
  let place = $state({ left: 0, top: 0, up: false });

  // Under the word's last line, inside the window; above its first line
  // when there is no room below. Relative to the block, which holds it.
  function measure() {
    if (!word || !menu) return;
    const base = menu.offsetParent?.getBoundingClientRect() ?? { left: 0, top: 0 };
    const lines = word.getClientRects();
    const first = lines[0] ?? word.getBoundingClientRect();
    const last = lines[lines.length - 1] ?? first;
    const width = menu.offsetWidth;
    const height = menu.offsetHeight;
    const left = Math.max(MARGIN, Math.min(last.left, window.innerWidth - MARGIN - width));
    const up = last.bottom + GAP + height > window.innerHeight - MARGIN && first.top - GAP - height >= MARGIN;
    place = { left: left - base.left, top: (up ? first.top - GAP - height : last.bottom + GAP) - base.top, up };
  }

  const items = () => (menu ? [...menu.querySelectorAll("[data-item]")] : []);

  function close(focusWord) {
    ontoggle(false);
    if (focusWord) word?.focus({ preventScroll: true });
  }

  // After a pick: the word keeps the focus when the menu had it.
  function done() {
    close(Boolean(menu?.contains(document.activeElement)));
  }

  $effect(() => {
    if (!open) return;
    tick().then(() => {
      measure();
      const list = items();
      (list.find((el) => el.getAttribute("aria-checked") === "true") ?? list[0])?.focus({ preventScroll: true });
    });
    // Esc is the window's key router's first (Settings closes on it), so the
    // open menu takes it in the scope asked before the page's.
    const unbind = bindKey("overlay", "Escape", () => {
      close(true);
      return true;
    });
    const away = (event) => {
      if (!root?.contains(event.target)) ontoggle(false);
    };
    document.addEventListener("pointerdown", away, true);
    window.addEventListener("resize", measure);
    return () => {
      unbind();
      document.removeEventListener("pointerdown", away, true);
      window.removeEventListener("resize", measure);
    };
  });

  function wordKey(event) {
    if (event.target !== word) return;
    if (event.key === "Enter" || event.key === " " || (event.key === "ArrowDown" && !open)) {
      event.preventDefault();
      ontoggle(!open);
    }
  }

  function menuKey(event) {
    // A number field keeps its own ↑/↓ (they step its value).
    if (event.target.tagName === "INPUT" && (event.key === "ArrowUp" || event.key === "ArrowDown")) return;
    const list = items();
    if (!list.length) return;
    const at = list.indexOf(document.activeElement);
    let next = -1;
    if (event.key === "ArrowDown") next = (at + 1) % list.length;
    else if (event.key === "ArrowUp") next = (at - 1 + list.length) % list.length;
    else if (event.key === "Home") next = 0;
    else if (event.key === "End") next = list.length - 1;
    if (next < 0) return;
    event.preventDefault();
    list[next].focus({ preventScroll: true });
  }

  function leave(event) {
    if (open && !root?.contains(event.relatedTarget)) ontoggle(false);
  }
</script>

<!-- No whitespace between the tags: the word sits inside a sentence. -->
<span class="word-menu" bind:this={root} onfocusout={leave}><span
    bind:this={word}
    class="word"
    class:open
    role="button"
    tabindex="0"
    aria-haspopup="menu"
    aria-expanded={open}
    aria-label="{label}: {text}"
    onclick={() => ontoggle(!open)}
    onkeydown={wordKey}>{text}</span
  >{#if open}<span
      class="menu"
      role="menu"
      tabindex="-1"
      aria-label={label}
      data-probe="word-menu"
      bind:this={menu}
      style:left="{place.left}px"
      style:top="{place.top}px"
      transition:unfold={{ from: place.up ? "bottom" : "top" }}
      onkeydown={menuKey}
      onmousedown={(event) => {
        // A press on the menu's own ground keeps the focus where it is.
        if (!event.target.closest("[data-item]")) event.preventDefault();
      }}>{@render children(done)}</span
    >{/if}</span
>

<style>
  .word {
    text-decoration: underline dotted var(--slate);
    text-decoration-thickness: 1px;
    text-underline-offset: calc(5 * var(--px));
    cursor: pointer;
    border-radius: 0;
  }
  .word:hover {
    text-decoration-style: solid;
    text-decoration-color: var(--ink);
  }
  .word.open {
    background: var(--band);
    text-decoration-style: dotted;
    text-decoration-color: var(--ink);
  }
  .word:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }
  .menu {
    position: absolute;
    z-index: 30;
    display: flex;
    flex-direction: column;
    box-sizing: border-box;
    min-width: calc(180 * var(--px));
    max-width: calc(440 * var(--px));
    max-height: calc(13 * 32 * var(--px) + 2px);
    overflow-y: auto;
    scrollbar-width: thin;
    scrollbar-color: var(--rule-control) transparent;
    padding: calc(4 * var(--px)) 0;
    border: 1px solid var(--rule-control);
    background: var(--paper-raised);
    font: var(--t-button);
    color: var(--ink);
    white-space: nowrap;
    text-decoration: none;
    user-select: none;
  }
</style>
