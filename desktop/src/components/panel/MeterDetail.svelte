<script>
  // The meters' detail popover (4.13, SIDE-014): the terminal's footer words
  // in --t-small ("Context 38% left · ~124k/200k tokens", "5h 14% left ·
  // resets 2h41m", …), then the "Customize…" text button (SIDE-016, the
  // desktop's way in until phase 5's palette). Placed by Meters.
  let { lines = [], oncustomize, id = undefined } = $props();
</script>

<div class="detail" role="group" aria-label="Usage details" {id}>
  {#each lines as line, i (i)}
    <p class="line">{line}</p>
  {/each}
  <button type="button" class="customize" onclick={oncustomize}>Customize…</button>
</div>

<style>
  .detail {
    box-sizing: border-box;
    width: fit-content;
    max-width: 100%;
    padding: calc(12 * var(--px)) calc(16 * var(--px));
    background: var(--paper-raised);
    border: 1px solid var(--rule-control);
  }
  .line {
    font: var(--t-small);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .customize {
    margin-top: calc(6 * var(--px));
    font: var(--t-small);
    color: var(--link);
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: calc(4 * var(--px));
  }
  .line + .customize {
    margin-top: calc(8 * var(--px));
  }
</style>
