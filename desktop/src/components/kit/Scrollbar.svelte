<script>
  // The app's scrollbar on a sheet's scroller (desktop-design-5-6 2.1, 2.13
  // SET-006): the target's own scrollbar, the app's plain bar (app.css: a
  // thin grey rectangle, no arrows), as everywhere in the app (owner, 2026-10-26). It exists only
  // while the content overflows, so its column is kept only then (DLG-012,
  // internal/ui/dialog/internal/kit/layout.go:59-76). Modes as
  // options.tui.scrollbar: "never" hides it;
  // "always" shows it; "default" shows the thumb while scrolling and 2 s after
  // (and while it is dragged). A hidden thumb is only transparent (app.css),
  // so the bar still takes presses. Draws nothing itself.
  //   target (the scrolling element), mode: "default" | "always" | "never"
  import { userActed } from "../../lib/barfade.js";

  const SHOW_MS = 2000;

  let { target = null, mode = "default" } = $props();

  let shown = $state(false);
  let dragging = $state(false);
  let timer = null;

  function flash() {
    if (mode !== "default") return;
    shown = true;
    clearTimeout(timer);
    timer = setTimeout(hide, SHOW_MS);
  }

  function hide() {
    if (dragging) {
      timer = setTimeout(hide, SHOW_MS);
      return;
    }
    shown = false;
  }

  $effect(() => {
    const element = target;
    if (!element) return;
    element.classList.add("kit-scroll");
    // Only a scroll the user made shows it (lib/barfade.js).
    const onScroll = () => {
      if (userActed() || dragging) flash();
    };
    // A press on the bar's column, even while its thumb is hidden.
    const onDown = (event) => {
      if (event.button !== 0 || event.target !== element || event.offsetX < element.clientWidth) return;
      dragging = true;
      flash();
      window.addEventListener(
        "mouseup",
        () => {
          dragging = false;
          flash();
        },
        { once: true, capture: true },
      );
    };
    // Content over by a dot or less fits (owner, 2026-10-26): zoomed sizes
    // leave a sheet's body a sliver of a dot shorter than its own content, and
    // the bar then showed for nothing. Checked whenever the scroller or one of
    // its children changes size.
    const fit = () =>
      element.classList.toggle(
        "kit-bar-fits",
        element.scrollHeight - element.clientHeight <= 1 && element.scrollWidth - element.clientWidth <= 1,
      );
    // (A page without ResizeObserver, as the unit tests' jsdom, skips it.)
    const sizes = typeof ResizeObserver === "function" ? new ResizeObserver(fit) : null;
    const watch = () => {
      sizes?.disconnect();
      sizes?.observe(element);
      for (const child of element.children) sizes?.observe(child);
    };
    const children = new MutationObserver(watch);
    watch();
    children.observe(element, { childList: true });
    element.addEventListener("scroll", onScroll, { passive: true });
    element.addEventListener("mousedown", onDown);
    return () => {
      sizes?.disconnect();
      children.disconnect();
      element.removeEventListener("scroll", onScroll);
      element.removeEventListener("mousedown", onDown);
      element.classList.remove("kit-scroll", "kit-bar-never", "kit-bar-always", "kit-bar-shown", "kit-bar-fits");
      clearTimeout(timer);
    };
  });

  $effect(() => {
    const element = target;
    if (!element) return;
    element.classList.toggle("kit-bar-never", mode === "never");
    element.classList.toggle("kit-bar-always", mode === "always");
    element.classList.toggle("kit-bar-shown", mode === "default" && (shown || dragging));
  });

  // Whether the bar shows now (for the probe).
  export function showing() {
    return mode === "always" || (mode === "default" && (shown || dragging));
  }
</script>

<style>
  /* --bar-shown makes WebKitGTK redraw the bar (app.css). */
  :global(.kit-scroll.kit-bar-shown) {
    --bar-shown: 1;
  }
  :global(.kit-scroll.kit-bar-never) {
    scrollbar-width: none;
  }
  /* Doubled to outrank the scroller's own two-class overflow rule. */
  :global(.kit-scroll.kit-scroll.kit-bar-fits) {
    overflow: hidden;
  }
  :global(.kit-scroll.kit-bar-never::-webkit-scrollbar) {
    display: none;
  }
</style>
