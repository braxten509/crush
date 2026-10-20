<script>
  // The usage meters at the panel's top (SIDE-007, SIDE-014, SIDE-020 to
  // SIDE-024): CONTEXT, then one meter per usage window of the agent CLI
  // (5-HOUR, WEEK, and other windows when "Other usage limits" is on), each
  // the part left as a percent and a bar, with the amber triangle under 20 %
  // left. Three share a row at the approved picture's proportions; more go on
  // the next row, fewer share the row evenly. Hover or focus shows the
  // terminal's words and "Customize…" (MeterDetail).
  import { runAction } from "../../lib/actions.js";
  import Icon from "../Icon.svelte";
  import MeterDetail from "./MeterDetail.svelte";
  import { meterNow } from "./panel.svelte.js";

  // The reset times count down and a window past its reset is whole again.
  let now = $state(Date.now());
  $effect(() => {
    const timer = setInterval(() => (now = Date.now()), 30_000);
    return () => clearInterval(timer);
  });

  const model = $derived(meterNow(now));
  const meters = $derived(model.meters);
  const perRow = $derived(Math.min(3, Math.max(1, meters.length)));
  const columns = $derived(perRow === 3 ? "116fr 32fr 115fr 31fr 114fr" : Array.from({ length: perRow }, () => "115fr").join(" 31fr "));
  const resets = $derived(meters.some((m) => m.resets));

  let hover = $state(false);
  let within = $state(false);
  const open = $derived(hover || within);

  function leave(event) {
    if (!event.currentTarget.contains(event.relatedTarget)) within = false;
  }
</script>

<div class="usage-wrap" role="presentation" onmouseenter={() => (hover = true)} onmouseleave={() => (hover = false)} onfocusin={() => (within = true)} onfocusout={leave}>
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <section class="usage" class:with-resets={resets} aria-label="Usage" aria-describedby={open ? "meter-detail" : undefined} tabindex="0" style:grid-template-columns={columns}>
    {#each meters as meter, i (meter.id)}
      {@const col = i % perRow}
      {@const row = Math.floor(i / perRow) + 1}
      {#if col > 0}<span class="separator" class:second={col === 2} style:grid-row={row} style:grid-column={2 * col} aria-hidden="true"></span>{/if}
      <div class="cell" data-meter={meter.id} style:grid-row={row} style:grid-column={2 * col + 1}>
        <h3 class="label">{meter.label}</h3>
        <p class="value" class:pending={meter.text === "checking…"}>
          <span>{meter.text}</span>
          {#if meter.warn}
            <Icon name="warning" />
            <span class="visually-hidden">near the limit</span>
          {/if}
        </p>
        {#if meter.bar !== false}<div class="meter" role="meter" aria-label="{meter.label} left" aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(meter.left)}>
          <span class="fill" style:width="{meter.left}%"></span>
        </div>{/if}
        {#if meter.resets}<p class="resets">{meter.resets}</p>{/if}
      </div>
    {:else}
      <p class="no-meters">No meters chosen.</p>
    {/each}
  </section>
  {#if open}
    <div class="detail-anchor">
      <MeterDetail id="meter-detail" lines={model.lines} oncustomize={() => runAction("composer.customize")} />
    </div>
  {/if}
</div>

<style>
  .usage-wrap {
    position: relative;
    flex: none;
    margin-top: calc(21 * var(--px));
  }
  .usage {
    display: grid;
    grid-auto-rows: calc(110 * var(--px));
  }
  .usage.with-resets {
    grid-auto-rows: calc(134 * var(--px));
  }
  .usage:focus-visible {
    outline-offset: calc(4 * var(--px));
  }
  .cell {
    position: relative;
    min-width: 0;
    padding-top: calc(7 * var(--px));
  }
  .label {
    font: var(--t-label);
    letter-spacing: var(--track-label);
    text-transform: uppercase;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .separator {
    width: 1px;
    margin-left: calc(15 * var(--px));
    background: var(--rule-frame);
  }
  .separator.second {
    margin-left: calc(14 * var(--px));
  }
  .value {
    display: flex;
    align-items: flex-start;
    height: calc(34 * var(--px));
    margin-top: calc(10 * var(--px));
    font: var(--t-figure);
    white-space: nowrap;
  }
  .value :global(.icon) {
    margin: calc(2 * var(--px)) 0 0 calc(16 * var(--px));
  }
  .value.pending {
    align-items: center;
    font: var(--t-small);
    color: var(--slate);
  }
  .meter {
    position: relative;
    box-sizing: border-box;
    height: calc(13 * var(--px));
    margin-top: calc(9 * var(--px));
    border: 1px solid var(--meter-edge);
    background: var(--meter-track);
  }
  .fill {
    position: absolute;
    left: -1px;
    top: -1px;
    bottom: -1px;
    background: var(--meter);
    /* A changed amount eases to its new length. */
    transition: width 400ms var(--ease-paper);
  }
  .resets {
    margin-top: calc(4 * var(--px));
    font: var(--t-small);
    color: var(--slate);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .no-meters {
    grid-column: 1 / -1;
    align-self: center;
    font: var(--t-small);
    color: var(--slate);
  }
  /* Below the meters, over the rule and the body; its padding keeps the
     pointer inside while it crosses to the popover. */
  .detail-anchor {
    position: absolute;
    z-index: 3;
    top: 100%;
    left: 0;
    right: 0;
    padding-top: calc(8 * var(--px));
  }
</style>
