<script>
  // A multiline editor (desktop-design-5-6 2.1, DLG-018, DLG-019; look 5.1):
  // a <textarea> with the terminal's editor keys on top of the browser's
  // (lib/kit/areakeys.js). It grows with its text from minLines to maxLines
  // drawn lines and scrolls past that. Ctrl+P and Ctrl+N move the caret by
  // drawn lines, as ↑ and ↓ do; Ctrl+G and Ctrl+A select all and Ctrl+Shift+C copies
  // the selection. The front sheet's onKey hears every key first, so a
  // question form may take ↑/↓ for its choices; `reserved` names keys the
  // editor leaves alone.
  //   value (bindable), minLines, maxLines, reserved?: string[],
  //   placeholder?, label? (its accessible name)
  import { tick } from "svelte";
  import { writeClipboard } from "../../lib/copy.js";
  import { comboOf } from "../../lib/keys.js";
  import { areaEdit, areaState } from "../../lib/kit/areakeys.js";
  import { applyEdit } from "../../lib/kit/fieldkeys.js";
  import { setPart } from "../../lib/kit/sheetkeys.js";

  let { value = $bindable(""), minLines = 1, maxLines = 10, reserved = [], placeholder = "", label = "" } = $props();

  let box = $state(null);
  let area = $state(null);
  let rows = $state(1);

  const isReserved = (combo) => reserved.some((key) => String(key).toLowerCase() === combo.toLowerCase());

  function onkeydown(event) {
    if (event.defaultPrevented || event.isComposing) return;
    const combo = comboOf(event);
    if (isReserved(combo)) return;
    if (combo === "Ctrl+P" || combo === "Ctrl+N") {
      event.preventDefault();
      lineMove(combo === "Ctrl+P" ? -1 : 1);
      return;
    }
    if (combo === "Ctrl+G" || combo === "Ctrl+A") {
      event.preventDefault();
      area.setSelectionRange(0, area.value.length, "forward");
      return;
    }
    const edit = areaEdit(combo, areaState(area.value, area.selectionStart, area.selectionEnd, area.selectionDirection));
    if (!edit) return;
    event.preventDefault();
    if ("copy" in edit) {
      if (edit.copy) writeClipboard(edit.copy);
      return;
    }
    applyEdit(area, edit);
    keepCaretInView();
  }

  // ↑/↓'s own move, by drawn lines; a page without it moves by text lines.
  function lineMove(delta) {
    const from = [area.selectionStart, area.selectionEnd];
    const caret = area.selectionDirection === "backward" ? from[0] : from[1];
    area.setSelectionRange(caret, caret);
    try {
      area.ownerDocument.getSelection()?.modify("move", delta < 0 ? "backward" : "forward", "line");
    } catch {
      // No line moves here: by text lines below.
    }
    if (area.selectionStart !== caret || area.selectionEnd !== caret) return keepCaretInView();
    const text = area.value;
    const lineStart = text.lastIndexOf("\n", caret - 1) + 1;
    const column = caret - lineStart;
    let target = caret;
    if (delta < 0 && lineStart > 0) {
      const prevStart = text.lastIndexOf("\n", lineStart - 2) + 1;
      target = Math.min(prevStart + column, lineStart - 1);
    } else if (delta > 0) {
      const lineEnd = text.indexOf("\n", caret);
      if (lineEnd >= 0) {
        const nextEnd = text.indexOf("\n", lineEnd + 1);
        target = Math.min(lineEnd + 1 + column, nextEnd < 0 ? text.length : nextEnd);
      }
    }
    area.setSelectionRange(target, target);
    keepCaretInView();
  }

  // The browser scrolls a textarea to its caret after typing, not after a
  // selection set from code. A hidden copy of the text, wrapped the same
  // way, tells where the caret's drawn line is.
  const COPIED = ["fontStyle", "fontVariant", "fontWeight", "fontStretch", "fontSize", "lineHeight", "fontFamily", "letterSpacing", "wordSpacing", "tabSize", "textIndent", "paddingTop", "paddingRight", "paddingBottom", "paddingLeft"];
  function keepCaretInView() {
    const style = getComputedStyle(area);
    const line = parseFloat(style.lineHeight) || 20;
    const mirror = document.createElement("div");
    for (const name of COPIED) mirror.style[name] = style[name];
    Object.assign(mirror.style, { position: "absolute", left: "-10000px", top: "0", visibility: "hidden", whiteSpace: "pre-wrap", overflowWrap: "break-word", boxSizing: "border-box", width: `${area.clientWidth}px` });
    const caret = area.selectionDirection === "backward" ? area.selectionStart : area.selectionEnd;
    mirror.textContent = area.value.slice(0, caret);
    const mark = document.createElement("span");
    mark.textContent = String.fromCharCode(0x200b); // a zero-width space
    mirror.appendChild(mark);
    document.body.appendChild(mirror);
    const top = mark.offsetTop; // the caret line's top, the padding included, as scrollTop counts
    mirror.remove();
    const padTop = parseFloat(style.paddingTop) || 0;
    const padBottom = parseFloat(style.paddingBottom) || 0;
    if (top - padTop < area.scrollTop) area.scrollTop = top - padTop;
    else if (top + line + padBottom > area.scrollTop + area.clientHeight) area.scrollTop = top + line + padBottom - area.clientHeight;
  }

  // Grow with the text, by drawn lines, between minLines and maxLines.
  async function fit() {
    if (!area) return;
    await tick();
    const style = getComputedStyle(area);
    const line = parseFloat(style.lineHeight) || 20;
    const pad = parseFloat(style.paddingTop) + parseFloat(style.paddingBottom);
    const saved = area.style.height;
    area.style.height = "0px";
    const drawn = Math.max(1, Math.round((area.scrollHeight - pad) / line));
    area.style.height = saved;
    rows = Math.min(Math.max(drawn, minLines), Math.max(minLines, maxLines));
  }

  $effect(() => {
    void value;
    void minLines;
    void maxLines;
    fit();
  });

  $effect(() => {
    if (!area) return;
    const observer = new ResizeObserver(() => fit());
    observer.observe(area);
    return () => observer.disconnect();
  });

  export function focus() {
    area?.focus({ preventScroll: true });
  }

  $effect(() => {
    if (!box) return;
    return setPart(box, {
      kind: "area",
      report: () => ({ kind: "area", lines: rows, min_lines: minLines, max_lines: maxLines, focused: document.activeElement === area }),
      heavy: () => {
        const r = area.getBoundingClientRect();
        return { kind: "area", rect: { x: r.x, y: r.y, w: r.width, h: r.height }, scroll_top: area.scrollTop, scroll_height: area.scrollHeight, client_height: area.clientHeight };
      },
    });
  });
</script>

<div class="area" data-kit-area bind:this={box} style:--rows={rows}>
  <textarea
    bind:this={area}
    bind:value
    {placeholder}
    aria-label={label || placeholder || undefined}
    autocomplete="off"
    spellcheck="false"
    {onkeydown}
  ></textarea>
</div>

<style>
  /* The field's box (look 5.1) holding several lines. */
  .area {
    display: flex;
    flex: none;
    box-sizing: border-box;
    border: 1px solid var(--rule-control);
    background: var(--paper);
  }
  .area:focus-within {
    outline: 1px solid var(--vermilion);
    outline-offset: calc(2 * var(--px));
  }
  textarea {
    flex: 1;
    min-width: 0;
    box-sizing: content-box;
    height: calc(var(--rows) * 26 * var(--px));
    margin: 0;
    padding: calc(12 * var(--px)) calc(12 * var(--px)) calc(12 * var(--px)) calc(11 * var(--px));
    border: 0;
    outline: 0;
    background: none;
    font: var(--t-field);
    color: var(--ink);
    resize: none;
    overflow-y: auto;
    appearance: none;
  }
  textarea::placeholder {
    color: var(--slate);
    opacity: 1;
  }
  textarea::selection {
    background: var(--selection);
  }
  textarea::-webkit-scrollbar {
    width: calc(6 * var(--px));
    background: transparent;
  }
  textarea::-webkit-scrollbar-thumb {
    background: var(--rule-control);
  }
</style>
