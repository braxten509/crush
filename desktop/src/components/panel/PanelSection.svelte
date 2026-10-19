<script>
  // One list section of the right panel and the details overlay (4.13): the
  // PROJECT label's style, then one row per item in --t-small, "None" when
  // the list is empty (the terminal's word, 4.12), and "…and N more" after a
  // list the details overlay cut. `row` draws one item's line.
  let { label, name, rows = [], more = "", row } = $props();
</script>

<section class="list-section" data-section={name} aria-label={label}>
  <h2 class="label">{label}</h2>
  {#if rows.length === 0}
    <p class="line none">None</p>
  {:else}
    <ul class="rows">
      {#each rows as item, i (i)}
        <li class="line row">{@render row(item)}</li>
      {/each}
    </ul>
    {#if more}<p class="line more">{more}</p>{/if}
  {/if}
</section>

<style>
  .label {
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
  }
  .rows,
  .none {
    margin-top: calc(8 * var(--px));
  }
  .line {
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    min-width: 0;
    min-height: calc(24 * var(--px));
    font: var(--t-small);
    font-variant-numeric: tabular-nums;
  }
  .none,
  .more {
    color: var(--slate);
  }
</style>
