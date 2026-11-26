<script>
  // The usage meters at the panel's top (SIDE-007, SIDE-014, SIDE-020 to
  // SIDE-024): CONTEXT, then one meter per usage window of the agent CLI
  // (5-HOUR, WEEK, and other windows when "Other usage limits" is on), each
  // the part left as a percent and a bar; under 30 % left both turn amber,
  // under 10 % rose (owner, 2026-10-20, in place of the warning triangle).
  // Three share a row at the approved picture's proportions; four go two and
  // two (owner, 2026-10-24: no lone meter left under three, split by one
  // horizontal rule between the rows). More go on the next row, fewer share
  // the row evenly. No hover box (owner,
  // 2026-10-19): the meters say it all; Ctrl+P keeps Customize.
  import { metersPerRow } from "./metergrid.js";
  import { creditsNow, meterNow, meterRoom, refillNow } from "./panel.svelte.js";
  import { useRefill } from "../../lib/fallback/store.svelte.js";

  // The reset times count down and a window past its reset is whole again.
  let now = $state(Date.now());
  $effect(() => {
    const timer = setInterval(() => (now = Date.now()), 30_000);
    return () => clearInterval(timer);
  });

  const model = $derived(meterNow(now));
  const credits = $derived(creditsNow());
  const meters = $derived(credits
    ? [...model.meters, { id: "credits", label: "Credits", text: credits, bar: false }]
    : model.meters);
  const perRow = $derived(metersPerRow(meters.length));
  const rows = $derived(Array.from({ length: Math.ceil(meters.length / perRow) }, (_, i) =>
    meters.slice(i * perRow, (i + 1) * perRow)));
  const columnLines = $derived(!(perRow === 2 && rows.length > 1));
  // Each row owns its columns. The two bottom meters in a group of five
  // share the full width with one centered divider.
  function sharesFor(row) {
    if (meters.length === 5 && row === 1) return [115, 115];
    return perRow === 3 ? [116, 115, 114] : Array(perRow).fill(115);
  }
  function columnsFor(row) {
    return sharesFor(row).map((share) => `${share}fr`).join(" 31fr ");
  }
  const resets = $derived(meters.some((m) => m.resets));

  // The panel width every meter's label, figure and reset time need whole
  // (owner, 2026-10-20: none may overlap or be cut with "…"; a reset line's
  // tail is left out, owner 2026-10-25): each cell's
  // widest word over its share of the row, plus what the panel keeps around
  // the meters. Measured again when the meters, their words or the zoom change.
  let section = $state(null);
  $effect(() => {
    void meters;
    if (!section) return;
    const measure = () => {
      const cells = [...section.querySelectorAll(".cell")];
      if (cells.length === 0) return;
      let row = 0;
      // The words' own width, read from their text: a block's scrollWidth is
      // never under its box, which grew the panel a pixel a measure.
      const range = document.createRange();
      const width = (el) => {
        if (!el) return 0;
        range.selectNodeContents(el);
        return range.getBoundingClientRect().width;
      };
      cells.forEach((cell, i) => {
        const need = Math.max(...[".label", ".resets > .main", ".value > span"].map((s) => width(cell.querySelector(s))));
        const shares = sharesFor(Math.floor(i / perRow));
        const total = shares.reduce((sum, share) => sum + share, 31 * (shares.length - 1));
        row = Math.max(row, ((need + 1) * total) / shares[i % perRow]);
      });
      const column = section.closest(".panel-column") ?? section.parentElement;
      const around = column.getBoundingClientRect().width - section.getBoundingClientRect().width;
      const unit = parseFloat(getComputedStyle(document.documentElement).fontSize) / 16 || 1;
      meterRoom.width = Math.ceil((row + around) / unit);
    };
    measure();
    document.fonts?.ready.then(measure);
    const observer = new ResizeObserver(measure);
    observer.observe(section);
    return () => observer.disconnect();
  });

</script>

<div class="usage-wrap">
  <section class="usage" class:with-resets={resets} aria-label="Usage" bind:this={section}>
    {#each rows as row, r}
      {#if r > 0}<div class="row-rule" aria-hidden="true"></div>{/if}
      <div class="meter-row" class:later={r > 0} class:halves={meters.length === 5 && r === 1} style:grid-template-columns={columnsFor(r)}>
        {#each row as meter, col (meter.id)}
          {#if columnLines && col > 0}
            <span class="separator" class:second={col === 2} style:grid-column={2 * col} aria-hidden="true"></span>
          {/if}
          <div class="cell" data-meter={meter.id} style:grid-column={2 * col + 1}>
            <h3 class="label">{meter.label}</h3>
            <p class="value {meter.level}" class:pending={meter.text === "checking…"}>
              <span>{meter.text}</span>
              {#if meter.level}<span class="visually-hidden">{meter.level === "out" ? "almost used up" : "running low"}</span>{/if}
            </p>
            {#if meter.bar !== false}<div class="meter" role="meter" aria-label="{meter.label} left" aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(meter.left)}>
              <span class="fill {meter.level}" style:width="{meter.left}%"></span>
            </div>{/if}
            {#if meter.resets}<p class="resets"><span class="main">{meter.resets}</span>{#if meter.resetsTail}<span class="tail">{meter.resetsTail}</span>{/if}</p>{/if}
            {#if meter.refill}<button type="button" class="use-refill" data-refill-use onclick={() => useRefill(refillNow().kind, refillNow().account)}>Use one</button>{/if}
          </div>
        {/each}
      </div>
    {:else}
      <p class="no-meters">No meters chosen.</p>
    {/each}
  </section>
</div>

<style>
  .usage-wrap {
    position: relative;
    flex: none;
    margin-top: calc(21 * var(--px));
  }
  .usage {
    --row-height: calc(110 * var(--px));
  }
  .usage.with-resets {
    --row-height: calc(134 * var(--px));
  }
  .meter-row {
    display: grid;
    grid-template-rows: var(--row-height);
  }
  .usage:focus-visible {
    outline-offset: calc(4 * var(--px));
  }
  .cell {
    position: relative;
    min-width: 0;
    padding-top: calc(7 * var(--px));
  }
  .meter-row.later {
    padding-top: calc(21 * var(--px));
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
  .halves .separator {
    justify-self: center;
    margin-left: 0;
  }
  .row-rule {
    height: calc(1 * var(--px));
    background: var(--rule-frame);
  }
  .value {
    display: flex;
    align-items: flex-start;
    height: calc(34 * var(--px));
    margin-top: calc(10 * var(--px));
    font: var(--t-figure);
    white-space: nowrap;
  }
  .value.low {
    color: var(--amber-ink);
  }
  .value.out {
    color: var(--error-ink);
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
    box-shadow: inset 0 0 0 1px var(--meter-edge);
    background: var(--meter-track);
  }
  .fill {
    position: absolute;
    left: 0;
    top: 0;
    bottom: 0;
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
  .resets {
    margin-top: calc(4 * var(--px));
    font: var(--t-small);
    color: var(--slate);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  /* A tail (CONTEXT's " tokens") shows only while it fits whole: it wraps
     to a second line the box hides, and the panel's least width counts the
     words before it only (owner, 2026-10-25). */
  .resets:has(.tail) {
    display: flex;
    flex-wrap: wrap;
    height: calc(24 * var(--px));
  }
  .tail {
    white-space: pre;
  }
  .no-meters {
    grid-column: 1 / -1;
    align-self: center;
    font: var(--t-small);
    color: var(--slate);
  }
  /* A free refill's button, under its count (owner, 2026-10-21). */
  .use-refill {
    margin-top: calc(6 * var(--px));
    height: calc(26 * var(--px));
    padding: 0 calc(10 * var(--px));
    border: 1px solid var(--rule-control);
    border-radius: 0;
    background: var(--control-rest);
    font: var(--t-button);
    color: var(--ink);
    cursor: pointer;
  }
  .use-refill:hover {
    background: var(--control-hover);
  }
  .use-refill:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }
</style>
