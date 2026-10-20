<script module>
  // The activity mark (desktop-design 4.10; CHAT-103, CHAT-104): three round
  // ink dots that fade 0.25 → 1 → 0.25 over 1.2 s, each 0.2 s after the one
  // before, drawn with CSS only, so only marks on the page run (the virtual
  // list takes the rest off it) and an idle chat has none. `line` sits in
  // replies and tools, `small` in tool groups. With prefers-reduced-motion
  // the dots stay at full ink.
  //
  // Sizes in picture px. 4.10 gives the dots and gaps; `line` is then 24 wide
  // as CHAT-104 says, while `small` comes to 15, not CHAT-104's 12, which
  // would need gaps of 1.5 px.
  export const MARK_SIZES = {
    line: { dot: 4, gap: 6, width: 4 * 3 + 6 * 2 },
    small: { dot: 3, gap: 3, width: 3 * 3 + 3 * 2 },
  };
</script>

<script>
  // Placed by StatusLine; ToolHeader, ShellItem, ToolGroup (P3.6); the right panel's TodoPanel.
  let { size = "line" } = $props();

  const kind = $derived(size === "small" ? "small" : "line");
</script>

<span class="mark {kind}" data-size={kind} aria-hidden="true"><span class="dot"></span><span class="dot"></span><span class="dot"></span></span>

<style>
  .mark {
    display: inline-flex;
    align-items: center;
    flex: none;
    vertical-align: middle;
  }
  .line {
    gap: calc(6 * var(--px));
  }
  .small {
    gap: calc(3 * var(--px));
  }
  .dot {
    display: block;
    border-radius: 50%;
    background: var(--ink);
    animation: fade 1.2s ease-in-out infinite both;
  }
  .line .dot {
    width: calc(4 * var(--px));
    height: calc(4 * var(--px));
  }
  .small .dot {
    width: calc(3 * var(--px));
    height: calc(3 * var(--px));
  }
  .dot:nth-child(2) {
    animation-delay: 0.2s;
  }
  .dot:nth-child(3) {
    animation-delay: 0.4s;
  }
  @keyframes fade {
    0%,
    100% {
      opacity: 0.25;
    }
    50% {
      opacity: 1;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .dot {
      animation: none;
      opacity: 1;
    }
  }
</style>
