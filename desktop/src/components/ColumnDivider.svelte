<script>
  // A column divider that can be dragged (or moved with the arrow keys) to
  // resize the column beside it. At rest it is just the frame's one-pixel line.
  // A fixed divider (the left list's narrow strip) is only that line.
  let { side, label, value, min, max, column, ondrag, ondone, fixed = false } = $props();

  let dragging = $state(false);
  let lastX = 0;

  // CSS pixels per picture pixel at the current zoom.
  function scale() {
    return parseFloat(getComputedStyle(document.documentElement).fontSize) / 16;
  }

  function down(event) {
    if (fixed || event.button !== 0) return;
    dragging = true;
    lastX = event.clientX;
    event.currentTarget.setPointerCapture(event.pointerId);
    event.preventDefault();
  }
  function move(event) {
    if (!dragging) return;
    const delta = (event.clientX - lastX) / scale();
    if (Math.abs(delta) < 0.5) return;
    lastX = event.clientX;
    ondrag(side, delta);
  }
  function up(event) {
    if (!dragging) return;
    dragging = false;
    event.currentTarget.releasePointerCapture?.(event.pointerId);
    ondone();
  }
  function key(event) {
    if (fixed) return;
    const step = event.shiftKey ? 40 : 8;
    if (event.key === "ArrowLeft") ondrag(side, -step);
    else if (event.key === "ArrowRight") ondrag(side, step);
    else return;
    event.preventDefault();
    ondone();
  }
</script>

<div
  class="divider"
  class:dragging
  class:fixed
  class:right={side === "right"}
  style:grid-column={column}
  role="separator"
  aria-orientation="vertical"
  aria-label={fixed ? undefined : label}
  aria-valuenow={fixed ? undefined : value}
  aria-valuemin={fixed ? undefined : min}
  aria-valuemax={fixed ? undefined : max}
  tabindex={fixed ? undefined : 0}
  onpointerdown={down}
  onpointermove={move}
  onpointerup={up}
  onpointercancel={up}
  onkeydown={key}
></div>

<style>
  .divider {
    position: relative;
    grid-row: 1 / 4;
    z-index: 2;
    outline: none;
    cursor: col-resize;
    touch-action: none;
  }
  /* The line runs into the frame's top and bottom lines (owner, 2026-10-20). */
  .divider::before {
    content: "";
    position: absolute;
    left: 0;
    width: 1px;
    top: 0;
    bottom: 0;
    background: var(--rule-frame);
  }
  /* A wider grip than the line itself, invisible. */
  .divider::after {
    content: "";
    position: absolute;
    left: calc(-5 * var(--px));
    right: calc(-5 * var(--px));
    top: calc(11 * var(--px));
    bottom: calc(14 * var(--px));
  }
  /* The conversation's scrollbar runs down the right divider's left side, so
     that grip reaches into the panel instead (owner, 2026-10-20: the bar must
     stay clickable). */
  .divider.right::after {
    left: 0;
    right: calc(-10 * var(--px));
  }
  .divider.fixed {
    cursor: default;
  }
  .divider:not(.fixed):hover::before,
  .divider.dragging::before {
    background: var(--rule-control);
  }
  .divider:focus-visible::before {
    background: var(--vermilion);
  }
</style>
