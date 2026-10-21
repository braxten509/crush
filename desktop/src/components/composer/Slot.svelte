<script>
  // The slot (Q8; 4.9): a form in the field-and-send row's place: the
  // permission prompt, the plan handoff, Customize Composer (P3.8), later
  // the question forms. It grows upward to at most 60% of the middle column
  // (Composer). entry is ui.slot; a new entry (a newer request) is a new form.
  // It unrolls up out of the typing box's row and, going, rolls back down
  // into it, lying over the returning row at the composer's foot.
  import { unfold } from "../../lib/motion.js";

  let { entry } = $props();
  // While the slot rolls away its entry is already gone (ui.slot is null):
  // it keeps drawing the last one, or reading it throws and the window
  // shows no form again (owner, 2026-10-21).
  let last = null;
  const shown = $derived.by(() => (entry ? (last = entry) : last));
  const LEAVING = "position: absolute; left: calc(20 * var(--px)); right: calc(20 * var(--px)); bottom: calc(33 * var(--px)); z-index: 3;";
</script>

<section class="slot" data-probe="slot" data-kind={shown.kind} transition:unfold|global={{ from: "bottom", overlay: LEAVING }}>
  {#key shown}
    <shown.component {...shown.props} />
  {/key}
</section>

<style>
  .slot {
    flex: 0 1 auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
    border: 1px solid var(--rule-control);
    background: var(--paper-raised);
  }
</style>
