<script>
  // The slot (Q8; 4.9): a form in the field-and-send row's place: the
  // permission prompt, the plan handoff, Customize Composer (P3.8), later
  // the question forms. It grows upward to at most 60% of the middle column
  // (Composer). entry is ui.slot; a new entry (a newer request) is a new form.
  let { entry } = $props();
</script>

<section class="slot" data-probe="slot" data-kind={entry.kind}>
  {#key entry}
    <entry.component {...entry.props} />
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
