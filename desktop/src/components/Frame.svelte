<script>
  // The page frame: one-pixel lines round the three columns that meet at the
  // corners, and the column dividers run into its top and bottom lines (owner,
  // 2026-10-20: the outer boxes are closed; only lines inside them stop short).
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
  /* The verticals cover the corner pixels, so the lines meet. */
  .vertical::before {
    content: "";
    position: absolute;
    left: 0;
    width: 1px;
    top: 0;
    bottom: 0;
    background: var(--rule-frame);
  }
  .horizontal::before {
    content: "";
    position: absolute;
    top: 0;
    height: 1px;
    left: 0;
    right: 0;
    background: var(--rule-frame);
  }
</style>
