<script>
  // The page frame: one-pixel lines round the three columns, with a small gap
  // at each corner and where a column divider meets the top or bottom line
  // (no + marks since owner, 2026-10-20).
  // These elements sit straight in the page grid (see App.svelte). Without the
  // panel (D11) the frame closes round the chat list and the conversation.
  let { panel = true } = $props();

  const columns = $derived(panel ? [2, 4, 6] : [2, 4]);
</script>

<span class="vertical" style:grid-column="1" aria-hidden="true"></span>
<span class="vertical" style:grid-column={panel ? "7" : "5"} aria-hidden="true"></span>
{#each columns as column (column)}
  <span class="horizontal" style:grid-column={column} style:grid-row="1" aria-hidden="true"></span>
  <span class="horizontal" style:grid-column={column} style:grid-row="3" aria-hidden="true"></span>
{/each}

<style>
  .vertical,
  .horizontal {
    position: relative;
    pointer-events: none;
  }
  .vertical {
    grid-row: 1 / 4;
  }
  /* Lines stop 11 picture pixels short of each corner and join. */
  .vertical::before {
    content: "";
    position: absolute;
    left: 0;
    width: 1px;
    top: calc(11 * var(--px));
    bottom: calc(14 * var(--px));
    background: var(--rule-frame);
  }
  .horizontal::before {
    content: "";
    position: absolute;
    top: 0;
    height: 1px;
    left: calc(11 * var(--px));
    right: calc(11 * var(--px));
    background: var(--rule-frame);
  }
</style>
