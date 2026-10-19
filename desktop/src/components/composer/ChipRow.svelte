<script>
  // The chip row under the composer rule (TYPE-035 to TYPE-038, SIDE-026;
  // 4.9): the draft's attachments, then the chat's kept skills, on one line.
  // What does not fit becomes "N more…"; a first chip too wide for the line
  // has its name trimmed, to at least 5 characters (fitChips,
  // lib/composer/chips.js). A hidden copy of every chip measures them. In
  // delete mode the attachments show their digit where the ✕ was, so nothing
  // moves, and a ✕ click does nothing.
  import Chip from "../render/Chip.svelte";
  import { attachmentChip, fitChips, moreText, skillChip } from "../../lib/composer/chips.js";
  import { composer, draftOf, keepSkill, keptSkills, removeAttachment } from "../../lib/stores/composer.svelte.js";

  const DIGITS = 10; // delete mode numbers the first ten (TYPE-038)
  const MIN_NAME = 5;

  const items = $derived([
    ...draftOf().attachments.map((attachment, index) => ({ key: `a${index}`, chip: attachmentChip(attachment), index })),
    ...keptSkills().map((skill) => ({ key: `s${skill.id}`, chip: skillChip(skill), skill })),
  ]);

  let row = $state(null);
  let measure = $state(null);
  let width = $state(0);
  let fit = $state({ shown: Number.POSITIVE_INFINITY, more: 0, firstName: null, fixed: 0 });

  const shown = $derived(items.slice(0, fit.shown));

  $effect(() => {
    if (!row) return;
    const observer = new ResizeObserver(() => (width = row.clientWidth));
    observer.observe(row);
    return () => observer.disconnect();
  });

  // Effects run after the DOM update, so the hidden copy already holds the chips.
  $effect(() => {
    void items;
    void width;
    if (row && measure) fit = measured();
  });

  let canvas = null;

  function textWidth(text, font) {
    canvas ??= document.createElement("canvas");
    const context = canvas.getContext("2d");
    context.font = font;
    return context.measureText(text).width;
  }

  function measured() {
    const chips = [...measure.querySelectorAll("[data-measure]")];
    if (chips.length === 0) return { shown: 0, more: 0, firstName: null, fixed: 0 };
    const widths = chips.map((chip) => chip.getBoundingClientRect().width);
    const gap = parseFloat(getComputedStyle(row).columnGap) || 0;
    const moreFont = getComputedStyle(measure.querySelector("[data-measure-more]")).font;
    const name = chips[0].querySelector(".name");
    const nameWidth = name ? name.getBoundingClientRect().width : 0;
    const fixed = widths[0] - nameWidth;
    const minName = name ? textWidth(Array.from(name.textContent).slice(0, MIN_NAME).join(""), getComputedStyle(name).font) : 0;
    const result = fitChips({
      widths,
      moreWidth: (n) => textWidth(moreText(n), moreFont),
      available: row.clientWidth,
      gap,
      fixed,
      minName,
    });
    return { ...result, fixed };
  }

  function remove(item) {
    if (composer.deleteMode) return;
    if (item.skill) keepSkill(item.skill, false);
    else removeAttachment(item.index);
  }

  const digit = (item) => (composer.deleteMode && item.skill === undefined && item.index < DIGITS ? item.index : null);
</script>

<div class="chip-row">
  <div class="chips" bind:this={row}>
    {#each shown as item, at (item.key)}
      <span class="place" data-chip style:max-width={at === 0 && fit.firstName !== null ? `${fit.fixed + fit.firstName}px` : null}>
        <Chip chip={item.chip} removable number={digit(item)} onremove={() => remove(item)} />
      </span>
    {/each}
    {#if fit.more > 0}<span class="more" data-more>{moreText(fit.more)}</span>{/if}
  </div>
  <div class="measure" bind:this={measure} aria-hidden="true" inert>
    {#each items as item (item.key)}
      <span class="place" data-measure><Chip chip={item.chip} removable /></span>
    {/each}
    <span class="more" data-measure-more>{moreText(1)}</span>
  </div>
</div>

<style>
  .chip-row {
    position: relative;
    flex: none;
    padding: calc(10 * var(--px)) 0;
  }
  .chips {
    display: flex;
    align-items: center;
    gap: calc(8 * var(--px));
    height: calc(30 * var(--px));
    overflow: hidden;
  }
  .place {
    flex: none;
    display: flex;
    min-width: 0;
  }
  .more {
    flex: none;
    font: var(--t-small);
    color: var(--slate);
    white-space: nowrap;
  }
  .measure {
    position: absolute;
    top: 0;
    left: 0;
    display: flex;
    gap: calc(8 * var(--px));
    width: max-content;
    visibility: hidden;
    pointer-events: none;
  }
</style>
