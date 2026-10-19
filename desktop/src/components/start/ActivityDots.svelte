<script>
  // The activity mark of desktop-design 4.10, size "line": three ink dots
  // that fade in turn (CSS only), still at full ink with reduced motion.
  // The timeline's ActivityMark (P3.5, timeline/ActivityMark.svelte) is the
  // shared one; this copy serves the gate and the opening line until the two
  // pieces meet on the merged branch.
</script>

<span class="dots" aria-hidden="true"><span></span><span></span><span></span></span>

<style>
  .dots {
    display: inline-flex;
    flex: none;
    align-items: center;
    gap: calc(6 * var(--px));
    width: calc(24 * var(--px));
    height: calc(4 * var(--px));
  }
  .dots span {
    flex: none;
    width: calc(4 * var(--px));
    height: calc(4 * var(--px));
    border-radius: 50%;
    background: var(--ink);
    opacity: 0.25;
    animation: fade 1.2s ease-in-out infinite;
  }
  .dots span:nth-child(2) {
    animation-delay: 0.2s;
  }
  .dots span:nth-child(3) {
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
    .dots span {
      animation: none;
      opacity: 1;
    }
  }
</style>
