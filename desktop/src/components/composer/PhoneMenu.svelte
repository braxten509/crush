<script>
  import { unfold } from "../../lib/motion.js";
  import { bindKey } from "../../lib/keys.js";

  let { button, onpick, onclose } = $props();

  let box = $state(null);
  let place = $state(null);

  function close(refocus = false) {
    onclose();
    if (refocus && button?.isConnected) button.focus({ preventScroll: true });
  }

  function pick(what) {
    onclose();
    onpick(what);
  }

  function items() {
    return [...(box?.querySelectorAll("button") ?? [])];
  }

  function move(by) {
    const list = items();
    const at = list.indexOf(document.activeElement);
    list[(at + by + list.length) % list.length]?.focus({ preventScroll: true });
    return true;
  }

  function enter() {
    const item = items().find((b) => b === document.activeElement);
    if (!item) return false;
    item.click();
    return true;
  }

  $effect(() => {
    if (!box || !button) return;
    const r = button.getBoundingClientRect();
    const gap = 2;
    const w = box.offsetWidth;
    const h = box.offsetHeight;
    const above = r.top - gap - h;
    place = {
      x: Math.max(0, Math.min(r.left, window.innerWidth - w)),
      y: above >= 0 ? above : r.bottom + gap,
    };
    box.querySelector("button")?.focus({ preventScroll: true });
    const outside = (event) => {
      if (!box?.contains(event.target) && !button.contains(event.target)) close();
    };
    const away = () => close();
    window.addEventListener("mousedown", outside, true);
    window.addEventListener("wheel", away, { capture: true, passive: true });
    window.addEventListener("blur", away);
    window.addEventListener("resize", away);
    const unbind = [
      bindKey("overlay", "ArrowDown", () => move(1)),
      bindKey("overlay", "ArrowUp", () => move(-1)),
      bindKey("overlay", "Enter", enter),
      bindKey("overlay", "Escape", () => (close(true), true)),
      bindKey("overlay", "Tab", () => (close(), false)),
      bindKey("overlay", "Shift+Tab", () => (close(), false)),
    ];
    return () => {
      for (const fn of unbind) fn();
      window.removeEventListener("mousedown", outside, true);
      window.removeEventListener("wheel", away, { capture: true });
      window.removeEventListener("blur", away);
      window.removeEventListener("resize", away);
    };
  });
</script>

<!-- svelte-ignore a11y_interactive_supports_focus -->
<div
  class="phone-menu"
  role="menu"
  aria-label="Phone"
  data-phone-menu
  bind:this={box}
  style:left="{place?.x ?? 0}px"
  style:top="{place?.y ?? 0}px"
  style:visibility={place ? null : "hidden"}
  transition:unfold={{ from: "bottom" }}
>
  <button class="entry" type="button" role="menuitem" data-phone-pick="picture" onclick={() => pick("picture")}>Take Picture</button>
  <button class="entry" type="button" role="menuitem" data-phone-pick="record" onclick={() => pick("record")}>Record</button>
</div>

<style>
  .phone-menu {
    position: fixed;
    z-index: 30;
    display: flex;
    flex-direction: column;
    box-sizing: border-box;
    padding: calc(6 * var(--px)) 0;
    background: var(--paper-raised);
    border: 1px solid var(--rule-control);
  }
  .entry {
    box-sizing: border-box;
    min-height: calc(36 * var(--px));
    padding: calc(5 * var(--px)) calc(16 * var(--px));
    border: 0;
    background: none;
    font: var(--t-body-small);
    color: var(--ink);
    text-align: left;
    white-space: nowrap;
    cursor: default;
  }
  .entry:hover,
  .entry:focus-visible {
    background: var(--pick);
    outline: none;
  }
</style>
