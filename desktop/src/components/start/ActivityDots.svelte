<script>
  // The activity mark of desktop-design 4.10, size "line": three ink dots
  // that fade in turn on the one beat of every busy mark (motion.js
  // busyBeat), still at full ink with reduced motion. The timeline's
  // ActivityMark (P3.5, timeline/ActivityMark.svelte) is the shared one; this
  // copy serves the gate and the opening line until the two pieces meet on
  // the merged branch.
  import { busyBeat } from "../../lib/motion.js";
</script>

<span class="dots" aria-hidden="true" use:busyBeat><span></span><span></span><span></span></span>

<style>
  /* Whole-pixel dots and gaps, as the timeline's ActivityMark. */
  .dots {
    display: inline-flex;
    flex: none;
    align-items: center;
    gap: round(calc(6 * var(--px)), 1px);
  }
  .dots span {
    flex: none;
    width: round(calc(4 * var(--px)), 1px);
    height: round(calc(4 * var(--px)), 1px);
    border-radius: 50%;
    background: var(--ink);
  }
</style>
