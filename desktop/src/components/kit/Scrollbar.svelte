<script>
  // The app's scrollbar on a sheet's scroller (desktop-design-5-6 2.1, 2.13
  // SET-006, look 5.0): the target's own scrollbar drawn as the chat's
  // (CHAT-094): a 6 wide --rule-control thumb and no track. It exists only
  // while the content overflows, so its column is kept only then (DLG-012,
  // internal/ui/dialog/internal/kit/layout.go:59-76). Modes as
  // options.tui.scrollbar: "default" shows the thumb while scrolling and 2 s
  // after (and while it is dragged), "always", "never". A hidden thumb is only
  // transparent, so the bar still takes presses. Draws nothing itself.
  //   target (the scrolling element), mode: "default" | "always" | "never"
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
    const onScroll = () => flash();
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
    element.addEventListener("scroll", onScroll, { passive: true });
    element.addEventListener("mousedown", onDown);
    return () => {
      element.removeEventListener("scroll", onScroll);
      element.removeEventListener("mousedown", onDown);
      element.classList.remove("kit-scroll", "kit-bar-never", "kit-bar-always", "kit-bar-shown");
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

  // Whether the thumb shows now (for the probe).
  export function showing() {
    return mode === "always" || (mode === "default" && (shown || dragging));
  }
</script>

<style>
  :global(.kit-scroll::-webkit-scrollbar) {
    width: calc(6 * var(--px));
    background: transparent;
  }
  :global(.kit-scroll::-webkit-scrollbar-track) {
    background: transparent;
  }
  :global(.kit-scroll::-webkit-scrollbar-thumb) {
    background: transparent;
  }
  /* --bar-shown makes WebKitGTK redraw the bar (app.css). */
  :global(.kit-scroll.kit-bar-shown) {
    --bar-shown: 1;
  }
  :global(.kit-scroll.kit-bar-shown::-webkit-scrollbar-thumb),
  :global(.kit-scroll.kit-bar-always::-webkit-scrollbar-thumb) {
    background: var(--rule-control);
  }
  :global(.kit-scroll.kit-bar-never) {
    scrollbar-width: none;
  }
  :global(.kit-scroll.kit-bar-never::-webkit-scrollbar) {
    display: none;
  }
</style>
