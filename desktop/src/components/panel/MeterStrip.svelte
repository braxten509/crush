<script>
  // The strip under the typing box. {meters}: while the right panel is
  // hidden, the same meters sit in the composer's bottom padding under the
  // field (4.13, SIDE-002, SIDE-014): per meter the small-caps label, a
  // 60 × 4 bar and the percent (both amber under 30 % left, rose under 10 %),
  // 20 apart from the field's left edge; then the token count when "Tokens
  // used" is on (SIDE-019). {remote}: while the phone shares the shown chat,
  // a vermilion dot and REMOTE, after the meters (owner, 2026-10-21: a status
  // line under the typing box, with nothing after it); clicking it opens the
  // Remote control sheet. It sits in the composer's 15 bottom padding plus 18
  // of its own room, which grows and shrinks with it as it settles in and
  // rolls away, so the space under the field glides along (owner,
  // 2026-10-20: "slide the bottom area's space at the same time"). Placed by
  // Conversation while either shows; a part that comes or goes while the
  // other stays settles in or rolls away by itself.
  import { paper, settle, tick } from "../../lib/motion.js";
  import { openRemote } from "../../lib/remote/share.js";

  let { meters = true, remote = false } = $props();

  // The strip's own room, scaled with the strip's motion (no clipping: the
  // strip reaches up into the composer's padding).
  function room(node) {
    const height = parseFloat(getComputedStyle(node).height) || 0;
    return paper((t) => `height: ${(t * height).toFixed(2)}px;${tick(t)}`);
  }
  import { meterNow, refillNow } from "./panel.svelte.js";
  import { useRefill } from "../../lib/fallback/store.svelte.js";

  let now = $state(Date.now());
  $effect(() => {
    const timer = setInterval(() => (now = Date.now()), 30_000);
    return () => clearInterval(timer);
  });

  const model = $derived(meterNow(now));
</script>

<div class="strip-anchor" transition:room|global>
  <div class="strip" data-meter-strip transition:settle|global={{ fold: true }}>
    {#if meters}<span class="meters" role="group" aria-label="Usage" transition:settle>
    {#each model.meters as meter (meter.id)}
      <span class="item" data-meter={meter.id}>
        <span class="label">{meter.label}</span>
        {#if meter.bar !== false}<span class="bar" role="meter" aria-label="{meter.label} left" aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(meter.left)}>
          <span class="fill {meter.level}" style:width="{meter.left}%"></span>
        </span>{/if}
        <span class="value {meter.level}">{meter.text}</span>
        {#if meter.refill}<button type="button" class="use-refill" data-refill-use onclick={() => useRefill(refillNow().kind, refillNow().account)}>Use one</button>{/if}
        {#if meter.level}<span class="visually-hidden">{meter.level === "out" ? "almost used up" : "running low"}</span>{/if}
      </span>
    {/each}
    {#if model.tokens}<span class="tokens">{model.tokens}</span>{/if}
    </span>{/if}
    {#if remote}<button type="button" class="item remote" data-remote-line title="Remote control" transition:settle onclick={openRemote}>
      <span class="dot" aria-hidden="true"></span><span class="label">Remote</span>
    </button>{/if}
  </div>
</div>

<style>
  /* 18 of its own under the composer's 15 = the 33 the strip fills. */
  .strip-anchor {
    position: relative;
    flex: none;
    height: calc(18 * var(--px));
  }
  /* The strip fills the composer's 15 bottom padding and its own 18, so its
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
  .meters {
    flex: 0 1 auto;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: calc(20 * var(--px));
    overflow: hidden;
  }
  .item {
    flex: none;
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
  }
  /* The share's mark: a flat button, its words in ink after a whole-pixel
     vermilion dot about as tall as the capitals. */
  .remote {
    gap: calc(10 * var(--px));
    margin: 0;
    padding: 0;
    border: 0;
    background: none;
    cursor: pointer;
  }
  .remote .dot {
    flex: none;
    width: round(calc(10 * var(--px)), 1px);
    height: round(calc(10 * var(--px)), 1px);
    border-radius: 50%;
    background: var(--vermilion);
  }
  .remote .label {
    color: var(--ink);
  }
  .remote:hover .label {
    color: var(--vermilion-ink);
  }
  .remote:focus-visible {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
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
  /* A free refill's button, after its count (owner, 2026-10-21). */
  .use-refill {
    height: calc(18 * var(--px));
    padding: 0 calc(6 * var(--px));
    border: 1px solid var(--rule-control);
    border-radius: 0;
    background: var(--control-rest);
    font: var(--t-small);
    line-height: 1;
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
