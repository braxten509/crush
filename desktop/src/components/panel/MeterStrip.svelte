<script>
  // The meter strip (4.13, SIDE-002, SIDE-014): while the right panel is
  // hidden, the same meters sit in the composer's bottom padding under the
  // field: per meter the small-caps label, a 60 × 4 bar, the percent and the
  // amber triangle when it applies, 20 apart from the field's left edge; then
  // the token count when "Tokens used" is on (SIDE-019). It takes no room of
  // its own, so nothing moves when it comes and goes. Placed by Conversation.
  import Icon from "../Icon.svelte";
  import { meterNow } from "./panel.svelte.js";

  let now = $state(Date.now());
  $effect(() => {
    const timer = setInterval(() => (now = Date.now()), 30_000);
    return () => clearInterval(timer);
  });

  const model = $derived(meterNow(now));
</script>

<div class="strip-anchor">
  <div class="strip" role="group" aria-label="Usage" data-meter-strip>
    {#each model.meters as meter (meter.id)}
      <span class="item" data-meter={meter.id}>
        <span class="label">{meter.label}</span>
        <span class="bar" role="meter" aria-label="{meter.label} used" aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(meter.used)}>
          <span class="fill" style:width="{meter.used}%"></span>
        </span>
        <span class="value">{meter.text}</span>
        {#if meter.warn}
          <Icon name="warning" size={12} />
          <span class="visually-hidden">near the limit</span>
        {/if}
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
  /* The composer's bottom padding is 25 picture px; the field starts 20 in. */
  .strip {
    position: absolute;
    left: calc(20 * var(--px));
    right: calc(20 * var(--px));
    bottom: 0;
    height: calc(25 * var(--px));
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
