<script>
  // The `@` popup (TYPE-026 to TYPE-028, TYPE-061; 4.9): up to 10 rows of 32
  // picture px, its text starting above the "@" that opened it (a mirror of
  // the field finds where that is), kept inside the window. The best match is
  // drawn lowest; matched letters are at weight 600 and the focused row is on
  // --band. Only the rows in view are drawn, and the wheel moves the view. A
  // click picks a row; the field keeps focus. The keys are the popup scope's
  // (lib/plugins/composer.js).
  import { untrack } from "svelte";
  import { MAX_ROWS, popupColumns } from "../../lib/composer/mentions.js";
  import { composer, fieldElement, focusRow, pickRow } from "../../lib/stores/composer.svelte.js";

  const GAP = 6; // picture px between the popup and the line it belongs to
  const MARGIN = 8; // picture px it keeps from the window's edges

  const popup = $derived(composer.popup);
  const rows = $derived(popup?.rows ?? []);
  const columns = $derived(popupColumns(rows));

  let first = $state(0); // the index of the lowest row drawn
  let box = $state(null);
  let place = $state(null);

  const view = $derived(rows.slice(first, first + MAX_ROWS).map((entry, at) => ({ entry, index: first + at })));

  // The focused row stays in view.
  $effect(() => {
    const focused = popup?.focused ?? 0;
    const count = rows.length;
    untrack(() => {
      let next = first;
      if (focused < next) next = focused;
      else if (focused >= next + MAX_ROWS) next = focused - MAX_ROWS + 1;
      first = Math.max(0, Math.min(next, count - MAX_ROWS));
    });
  });

  // The wheel moves the view; down the screen is toward the best row.
  $effect(() => {
    if (!box) return;
    const wheel = (event) => {
      event.preventDefault();
      const step = Math.sign(event.deltaY);
      if (step) first = Math.max(0, Math.min(first - step, rows.length - MAX_ROWS));
    };
    box.addEventListener("wheel", wheel, { passive: false });
    return () => box.removeEventListener("wheel", wheel);
  });

  $effect(() => {
    const start = popup?.start;
    void columns;
    void view.length;
    const field = fieldElement();
    if (!box || !field || start === undefined) return;
    const update = () => (place = placement(field, start));
    update();
    window.addEventListener("resize", update);
    return () => window.removeEventListener("resize", update);
  });

  const COPIED = [
    "boxSizing",
    "width",
    "fontFamily",
    "fontSize",
    "fontWeight",
    "fontStyle",
    "fontVariant",
    "fontStretch",
    "letterSpacing",
    "wordSpacing",
    "lineHeight",
    "textTransform",
    "textIndent",
    "tabSize",
    "paddingTop",
    "paddingRight",
    "paddingBottom",
    "paddingLeft",
    "borderTopWidth",
    "borderRightWidth",
    "borderBottomWidth",
    "borderLeftWidth",
  ];

  // Where character `index` of the field starts on the screen: a hidden copy
  // of the field with the text before it, then a mark holding the rest, so
  // the lines wrap as the field's do.
  function charPoint(field, index) {
    const style = getComputedStyle(field);
    const mirror = document.createElement("div");
    for (const name of COPIED) mirror.style[name] = style[name];
    Object.assign(mirror.style, { position: "absolute", top: "0", left: "0", visibility: "hidden", whiteSpace: "pre-wrap", overflowWrap: "break-word", borderStyle: "solid", borderColor: "transparent" });
    mirror.textContent = field.value.slice(0, index);
    const mark = document.createElement("span");
    mark.textContent = field.value.slice(index) || "​";
    mirror.append(mark);
    document.body.append(mirror);
    const x = mark.offsetLeft;
    const y = mark.offsetTop;
    mirror.remove();
    const rect = field.getBoundingClientRect();
    return { x: rect.left + x - field.scrollLeft, y: rect.top + y - field.scrollTop, line: parseFloat(style.lineHeight) || 0 };
  }

  function placement(field, start) {
    const unit = (parseFloat(getComputedStyle(document.documentElement).fontSize) || 16) / 16;
    const margin = MARGIN * unit;
    const gap = GAP * unit;
    const point = charPoint(field, start);
    const row = box.querySelector("[data-row]");
    const inset = row ? parseFloat(getComputedStyle(row).paddingLeft) || 0 : 0;
    const border = parseFloat(getComputedStyle(box).borderLeftWidth) || 0;
    const width = box.offsetWidth;
    const height = box.offsetHeight;
    let top = point.y - gap - height;
    // No room above the line: below it.
    if (top < margin) top = Math.min(point.y + point.line + gap, window.innerHeight - margin - height);
    const left = Math.min(point.x - inset - border, window.innerWidth - margin - width);
    return { left: Math.max(margin, left), top: Math.max(margin, top) };
  }

  // The row's text in runs of matched and unmatched characters.
  function runs(text, chars) {
    const hits = new Set(chars);
    const out = [];
    Array.from(text).forEach((char, index) => {
      const hit = hits.has(index);
      const last = out[out.length - 1];
      if (last && last.hit === hit) last.text += char;
      else out.push({ text: char, hit });
    });
    return out;
  }
</script>

{#if view.length > 0}
  <div
    class="popup"
    role="listbox"
    aria-label="Files"
    tabindex="-1"
    data-probe="popup"
    bind:this={box}
    style:--columns={columns}
    style:left={place ? `${place.left}px` : null}
    style:top={place ? `${place.top}px` : null}
    style:visibility={place ? null : "hidden"}
    onmousedown={(event) => event.preventDefault()}
  >
    {#each view as { entry, index } (entry.row.text)}
      <!-- The keys (↑ ↓ Enter Tab) are the popup scope's; the click is the mouse's way to the same pick. -->
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <div
        class="row"
        class:focused={index === popup.focused}
        role="option"
        tabindex="-1"
        aria-selected={index === popup.focused}
        data-row
        onpointermove={() => focusRow(index)}
        onclick={() => pickRow(index)}
      >
        <span class="text">{#each runs(entry.row.text, entry.chars) as run, at (at)}{#if run.hit}<b>{run.text}</b>{:else}{run.text}{/if}{/each}</span>
      </div>
    {/each}
  </div>
{/if}

<style>
  .popup {
    position: fixed;
    z-index: 5;
    display: flex;
    flex-direction: column-reverse;
    box-sizing: content-box;
    width: calc(var(--columns) * 1ch);
    max-width: calc(100vw - 2 * 8 * var(--px) - 2px);
    border: 1px solid var(--rule-control);
    background: var(--paper-raised);
    font: var(--t-code);
    color: var(--ink);
    user-select: none;
    animation: paper-unfold-up var(--motion) var(--ease-paper);
  }
  .row {
    display: flex;
    align-items: center;
    box-sizing: border-box;
    height: calc(32 * var(--px));
    padding: 0 1ch;
    cursor: pointer;
    white-space: pre;
  }
  .row.focused {
    background: var(--band);
  }
  .text {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  b {
    font-weight: 600;
  }
</style>
