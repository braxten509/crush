<script>
  // A sheet's help footer (desktop-design-5-6 2.1, DLG-009; look 5.1): a rule
  // with clear ends, then one line of "key description" pairs joined by " · ".
  // Disabled pairs are left out; pairs that do not fit are dropped from the
  // end and "…" follows when it fits, so the line never wraps and never ends
  // on a separator (lib/kit/helpfit.js, the terminal's ShortHelpLine,
  // internal/ui/dialog/internal/kit/help.go:26-65). The pairs are measured as
  // drawn and fitted again whenever the line's width or the fonts change.
  //   pairs: [{key, text, disabled?}], in the terminal's order
  import { fitHelp, helpText, HELP_ELLIPSIS, HELP_SEPARATOR } from "../../lib/kit/helpfit.js";
  import { setPart } from "../../lib/kit/sheetkeys.js";
  import Rule from "../Rule.svelte";

  let { pairs = [] } = $props();

  let box = $state(null);
  let line = $state(null);
  let meter = $state(null);
  let fit = $state({ shown: [], ellipsis: false });
  let sizes = $state(null);

  const enabled = $derived(pairs.filter((pair) => !pair.disabled));

  function measure() {
    if (!line || !meter) return;
    const width = (el) => el?.getBoundingClientRect().width ?? 0;
    const each = [...meter.querySelectorAll("[data-pair]")].map(width);
    const widths = new Map(enabled.map((pair, i) => [pair, each[i] ?? 0]));
    const measured = {
      pair: (pair) => widths.get(pair) ?? 0,
      separator: width(meter.querySelector("[data-separator]")),
      space: width(meter.querySelector("[data-space]")),
      ellipsis: width(meter.querySelector("[data-ellipsis]")),
    };
    const room = line.clientWidth;
    fit = fitHelp(enabled, room, measured);
    sizes = { room, pairs: each, separator: measured.separator, space: measured.space, ellipsis: measured.ellipsis };
  }

  $effect(() => {
    void enabled;
    if (!line || !meter) return;
    measure();
    const observer = new ResizeObserver(() => measure());
    observer.observe(line);
    const fonts = document.fonts;
    fonts?.addEventListener?.("loadingdone", measure);
    fonts?.ready?.then(() => measure());
    return () => {
      observer.disconnect();
      fonts?.removeEventListener?.("loadingdone", measure);
    };
  });

  $effect(() => {
    if (!box) return;
    return setPart(box, {
      kind: "help",
      report: () => ({
        kind: "help",
        pairs: enabled.map((pair) => ({ key: pair.key, text: pair.text })),
        disabled: pairs.filter((pair) => pair.disabled).map((pair) => pair.key),
        shown: fit.shown.length,
        ellipsis: fit.ellipsis,
        text: helpText(fit),
      }),
      heavy: () => ({
        kind: "help",
        box: rectOf(box),
        line: rectOf(line),
        text_rect: textRect(line),
        drawn: line?.textContent ?? "",
        pairs: [...(line?.querySelectorAll("[data-shown]") ?? [])].map(rectOf),
        sizes,
      }),
    });
  });

  function rectOf(el) {
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return { x: r.x, y: r.y, w: r.width, h: r.height };
  }

  function textRect(el) {
    if (!el || !el.firstChild) return null;
    const range = document.createRange();
    range.selectNodeContents(el);
    const r = range.getBoundingClientRect();
    return { x: r.x, y: r.y, w: r.width, h: r.height, lines: range.getClientRects().length };
  }
</script>

{#snippet pairView(pair)}<kbd>{pair.key}</kbd> <span class="text">{pair.text}</span>{/snippet}

<div class="help" data-kit-help bind:this={box}>
  <Rule marks="both" class="kit-help-rule" />
  <p class="line" bind:this={line}>
    {#each fit.shown as pair, i (i)}{#if i > 0}<span class="separator">{HELP_SEPARATOR}</span>{/if}<span class="pair" data-shown>{@render pairView(pair)}</span>{/each}{#if fit.ellipsis}<span class="separator">{fit.shown.length ? ` ${HELP_ELLIPSIS}` : HELP_ELLIPSIS}</span>{/if}
  </p>
  <p class="meter" aria-hidden="true" bind:this={meter}>
    {#each enabled as pair, i (i)}<span class="pair" data-pair>{@render pairView(pair)}</span>{/each}<span class="separator" data-separator>{HELP_SEPARATOR}</span><span class="separator" data-space> </span><span class="separator" data-ellipsis>{HELP_ELLIPSIS}</span>
  </p>
</div>

<style>
  .help {
    position: relative;
    flex: none;
  }
  /* The rule spans the frame, padding included, 14 above the line. */
  .help :global(.rule.kit-help-rule) {
    margin: 0 calc(-26 * var(--px)) calc(14 * var(--px));
  }
  .line,
  .meter {
    height: calc(32 * var(--px));
    display: flex;
    align-items: center;
    overflow: hidden;
  }
  .meter {
    position: absolute;
    left: 0;
    bottom: 0;
    width: max-content;
    visibility: hidden;
    pointer-events: none;
  }
  /* The space between a key and its words is a small-text space. */
  .pair {
    font: var(--t-small);
    white-space: pre;
  }
  kbd {
    font: var(--t-mono);
    color: var(--ink);
  }
  .text {
    font: var(--t-small);
    color: var(--mark);
  }
  .separator {
    font: var(--t-small);
    color: var(--rule);
    white-space: pre;
  }
</style>
