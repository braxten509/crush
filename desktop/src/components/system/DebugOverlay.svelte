<script>
  // The debug overlay shown with CRUSH_UI_DEBUG=true (desktop-design-5-6
  // 2.28, MISC-002; internal/ui/model/draw.go:106-114): a small square near
  // the top left that takes a random color on every redraw, above everything
  // and ignoring the mouse. A redraw is an animation frame in which the page
  // changed: a MutationObserver schedules one color change per frame. App
  // mounts it only in debug mode. It takes no props.
  import { onMount } from "svelte";

  let square = $state(null);

  const random = () => `#${Math.floor(Math.random() * 0x1000000).toString(16).padStart(6, "0")}`;

  onMount(() => {
    let booked = 0;
    const paint = () => {
      booked = 0;
      if (square) square.style.background = random();
    };
    const observer = new MutationObserver((records) => {
      // The overlay's own color change is not a redraw of the page.
      if (records.every((r) => r.target === square)) return;
      if (!booked) booked = requestAnimationFrame(paint);
    });
    observer.observe(document.body, { subtree: true, childList: true, attributes: true, characterData: true });
    paint();
    return () => {
      observer.disconnect();
      if (booked) cancelAnimationFrame(booked);
    };
  });
</script>

<div class="overlay" bind:this={square} aria-hidden="true"></div>

<style>
  .overlay {
    position: fixed;
    top: calc(24 * var(--px));
    left: calc(36 * var(--px));
    width: calc(36 * var(--px));
    height: calc(48 * var(--px));
    z-index: 2147483647;
    pointer-events: none;
  }
</style>
