<script>
  // The meter strip (4.13, SIDE-002, SIDE-014): while the right panel is
  // hidden, the same meters sit in the composer's bottom padding under the
  // field: per meter the small-caps label, a 60 × 4 bar and the percent (both
  // amber under 30 % left, rose under 10 %), 20 apart from the field's left
  // edge; then the token count when "Tokens used" is on (SIDE-019). It takes no room of
  // its own, so nothing moves when it comes and goes; it settles in and rolls
  // away (lib/motion.js). Placed by Conversation.
  import { settle } from "../../lib/motion.js";
  import { meterNow } from "./panel.svelte.js";

  let now = $state(Date.now());
  $effect(() => {
    const timer = setInterval(() => (now = Date.now()), 30_000);
    return () => clearInterval(timer);
  });

  const model = $derived(meterNow(now));
</script>

<div class="strip-anchor">
  <div class="strip" role="group" aria-label="Usage" data-meter-strip transition:settle|global={{ fold: true }}>
    {#each model.meters as meter (meter.id)}
      <span class="item" data-meter={meter.id}>
        <span class="label">{meter.label}</span>
        {#if meter.bar !== false}<span class="bar" role="meter" aria-label="{meter.label} left" aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(meter.left)}>
          <span class="fill {meter.level}" style:width="{meter.left}%"></span>
        </span>{/if}
        <span class="value {meter.level}">{meter.text}</span>
        {#if meter.level}<span class="visually-hidden">{meter.level === "out" ? "almost used up" : "running low"}</span>{/if}
      </span>
    {/each}
    {#if model.tokens}<span class="tokens">{model.tokens}</span>{/if}
  </div>
</div>

<style>
  .strip-anchor {
    position: relative;
    flex: none;
    height: 0;
  }
  /* The strip fills the composer's whole 33 picture px bottom padding, so its
     words sit midway between the field and the column's foot (owner,
     2026-10-20: the space above and below were unequal). Capitals and digits
     have no descenders, so their ink rides high in the line; 4 on top brings it
     to the middle. The field starts 20 in. */
  .strip {
    position: absolute;
    left: calc(20 * var(--px));
    right: calc(20 * var(--px));
    bottom: 0;
    box-sizing: border-box;
    height: calc(33 * var(--px));
    padding-top: calc(4 * var(--px));
    display: flex;
    align-items: center;
    gap: calc(20 * var(--px));
    overflow: hidden;
    white-space: nowrap;
  }
  .item {
    flex: none;
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
  }
  .label {
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
    color: var(--slate);
  }
  .bar {
    position: relative;
    box-sizing: border-box;
    width: calc(60 * var(--px));
    height: calc(4 * var(--px));
    border: 1px solid var(--meter-edge);
    background: var(--meter-track);
  }
  .fill {
    position: absolute;
    left: -1px;
    top: -1px;
    bottom: -1px;
    background: var(--meter);
    /* A changed amount eases to its new length and color. */
    transition:
      width 400ms var(--ease-paper),
      background-color 400ms var(--ease-paper);
  }
  .fill.low {
    background: var(--amber);
  }
  .fill.out {
    background: var(--error-ink);
  }
  .value.low {
    color: var(--amber-ink);
  }
  .value.out {
    color: var(--error-ink);
  }
  .value,
  .tokens {
    font: var(--t-small);
    font-variant-numeric: tabular-nums;
  }
  .tokens {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--slate);
  }
</style>
