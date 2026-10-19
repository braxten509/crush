// Test runs only (the probe's heavy fields, desktop-design 1.9): the boxes the
// align skill's screenshot checker needs for this piece's surfaces, read from
// the window's own layout. tests/look/align_real.py takes the same boxes from
// Chromium, which cannot show what the engine sends; tests/driver/p3_start.py
// pairs these with the real window's capture. The specs follow the align
// skill's mark() helper: a row (a control or line frame) and its items, each
// an icon or a text, fixed or variable (data: names, paths, counts, versions),
// with optional groups and shared columns.

function physical(b) {
  const d = window.devicePixelRatio || 1;
  const x = Math.floor(b.x * d);
  const y = Math.floor(b.y * d);
  return [x, y, Math.ceil((b.x + b.width) * d) - x, Math.ceil((b.y + b.height) * d) - y];
}

function union(boxes) {
  const x = Math.min(...boxes.map((b) => b.left));
  const y = Math.min(...boxes.map((b) => b.top));
  return { x, y, width: Math.max(...boxes.map((b) => b.right)) - x, height: Math.max(...boxes.map((b) => b.bottom)) - y };
}

function box(el) {
  let b = el.getBoundingClientRect();
  if (!b.width || !b.height) {
    const parts = [...el.getClientRects(), ...Array.from(el.children, (c) => c.getBoundingClientRect())].filter((r) => r.width && r.height);
    if (parts.length) b = union(parts);
  }
  return physical(b);
}

// The text itself (the union of its line boxes), not its element's box.
function textBox(el) {
  const range = document.createRange();
  range.selectNodeContents(el);
  const parts = [...range.getClientRects()].filter((r) => r.width && r.height);
  return parts.length ? physical(union(parts)) : null;
}

// The first meaningful letter or digit, as the align skill picks it.
function reference(el) {
  const segmenter = new Intl.Segmenter(undefined, { granularity: "grapheme" });
  const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    for (const part of segmenter.segment(node.data)) {
      if (!/[\p{L}\p{N}]/u.test(part.segment)) continue;
      const range = document.createRange();
      range.setStart(node, part.index);
      range.setEnd(node, part.index + part.segment.length);
      if (range.getBoundingClientRect().width > 0) return { range, text: part.segment };
    }
  }
  return null;
}

function textLines(el) {
  const range = document.createRange();
  range.selectNodeContents(el);
  const bands = [];
  for (const b of [...range.getClientRects()].filter((r) => r.width && r.height).sort((a, c) => a.top - c.top)) {
    const band = bands.find((a) => Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top) > Math.min(a.bottom - a.top, b.height) / 2);
    if (band) {
      band.top = Math.min(band.top, b.top);
      band.bottom = Math.max(band.bottom, b.bottom);
    } else bands.push({ top: b.top, bottom: b.bottom });
  }
  return bands.length;
}

// Drawn and wholly on screen: inside the window and inside every ancestor that
// clips (a scrolled list's rows below its bottom are not in the capture).
function visible(el) {
  const r = el.getBoundingClientRect();
  const style = getComputedStyle(el);
  if (!(r.width > 0 && r.height > 0 && style.visibility !== "hidden" && style.opacity !== "0")) return false;
  if (r.left < 0 || r.top < 0 || r.right > window.innerWidth || r.bottom > window.innerHeight) return false;
  for (let a = el.parentElement; a; a = a.parentElement) {
    const s = getComputedStyle(a);
    if (s.overflowX === "visible" && s.overflowY === "visible") continue;
    const c = a.getBoundingClientRect();
    if (r.left < c.left - 0.5 || r.top < c.top - 0.5 || r.right > c.right + 0.5 || r.bottom > c.bottom + 0.5) return false;
  }
  return true;
}

// specs: [{row: selector, id: string | (el) => string, source, center, items:
// [{name, kind: "icon" | "text", el: selector | ":scope", textKind, group, column}]}]
export function collectMarks(specs) {
  const rows = [];
  const textRects = {};
  for (const spec of specs) {
    for (const rowEl of document.querySelectorAll(spec.row)) {
      if (!visible(rowEl)) continue;
      const rowId = typeof spec.id === "function" ? spec.id(rowEl) : spec.id;
      const elements = [];
      for (const item of spec.items) {
        const el = item.el === ":scope" ? rowEl : rowEl.querySelector(item.el);
        if (!el || !visible(el)) continue;
        const id = `${rowId}-${item.name}`;
        const out = { id, kind: item.kind, group: item.group ?? "row", rect: box(el), source: spec.source };
        if (item.column) {
          out.column = `${item.column}-${item.kind}`;
          out.column_anchor = item.kind === "icon" ? "center" : "start";
        }
        if (item.kind === "text") {
          out.text_kind = item.textKind ?? "fixed";
          out.text = el.textContent;
          out.text_lines = textLines(el);
          const ref = reference(el);
          if (ref) {
            out.reference_text = ref.text;
            out.reference_rect = physical(ref.range.getBoundingClientRect());
          }
          const tight = textBox(el);
          if (tight) textRects[id] = tight;
        }
        elements.push(out);
      }
      if (!elements.length) continue;
      rows.push({
        id: rowId,
        label: rowId,
        rect: box(rowEl),
        source: spec.source,
        center: spec.center !== false,
        center_reason: spec.center === false ? "Not a control frame" : undefined,
        elements,
      });
    }
  }
  return { device_scale: window.devicePixelRatio || 1, rows, text_rects: textRects, spacing_groups: [] };
}

const slug = (text) =>
  String(text ?? "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "")
    .slice(0, 32);

const GATE = [
  { row: ".gate .starting", id: "gate-starting", center: false, source: "src/components/start/EngineGate.svelte", items: [
    { name: "icon", kind: "icon", el: ".dots" },
    { name: "text", kind: "text", el: ".label" },
  ] },
  { row: ".gate .title", id: "gate-title", center: false, source: "src/components/start/EngineGate.svelte", items: [{ name: "text", kind: "text", el: ":scope", column: "gate" }] },
  { row: ".gate .fix", id: "gate-fix", center: false, source: "src/components/start/EngineGate.svelte", items: [{ name: "text", kind: "text", el: ":scope", column: "gate" }] },
  { row: ".gate .try", id: "gate-try", source: "src/components/start/EngineGate.svelte", items: [{ name: "text", kind: "text", el: ".label" }] },
];

const START = [
  // The landing page's head: the wordmark and the Charm line share the landing block's left edge.
  { row: ".head .mark .word", id: "head-wordmark", center: false, source: "src/components/start/Wordmark.svelte", items: [{ name: "text", kind: "text", el: ":scope", column: "landing-left" }] },
  { row: ".landing .columns", id: "landing-headings", center: false, source: "src/components/start/Landing.svelte", items: [
    { name: "lsps", kind: "text", el: ".lsps .label", group: "lsps" },
    { name: "mcps", kind: "text", el: ".mcps .label", group: "mcps" },
    { name: "skills", kind: "text", el: ".skills .label", group: "skills" },
  ] },
  { row: ".landing .lsps", id: "landing-lsps", center: false, source: "src/components/start/Landing.svelte", items: [
    { name: "heading", kind: "text", el: ".label", column: "landing-left" },
  ] },
  { row: ".landing .column .none", id: (el) => `landing-none-${slug(el.closest(".column")?.dataset.column)}`, center: false, source: "src/components/start/Landing.svelte", items: [{ name: "text", kind: "text", el: ":scope" }] },
  { row: ".landing .column .item", id: (el) => `landing-${slug(el.closest(".column")?.dataset.column)}-${slug(el.querySelector(".name")?.textContent)}`, source: "src/components/start/Landing.svelte", items: [
    { name: "icon", kind: "icon", el: ".icon" },
  ] },
  // The init prompt (START-002): its fixed lines share a left edge; each button centres its word.
  { row: ".init .question", id: "init-question", center: false, source: "src/components/start/InitPrompt.svelte", items: [{ name: "text", kind: "text", el: ":scope", column: "init-left" }] },
  { row: ".init .hint", id: "init-hint", center: false, source: "src/components/start/InitPrompt.svelte", items: [{ name: "text", kind: "text", el: ":scope", column: "init-left" }] },
  { row: ".init .ask", id: "init-ask", center: false, source: "src/components/start/InitPrompt.svelte", items: [{ name: "text", kind: "text", el: ":scope", column: "init-left" }] },
  { row: ".init .yes", id: "init-yes", source: "src/components/start/InitPrompt.svelte", items: [{ name: "text", kind: "text", el: ".label" }] },
  { row: ".init .no", id: "init-no", source: "src/components/start/InitPrompt.svelte", items: [{ name: "text", kind: "text", el: ".label" }] },
  { row: ".init .buttons", id: "init-buttons", center: false, source: "src/components/start/InitPrompt.svelte", items: [
    { name: "yes", kind: "text", el: ".yes .label", group: "yes" },
    { name: "no", kind: "text", el: ".no .label", group: "no" },
  ] },
  // The opening line (LIST-034): the mark against its line; the title is data.
  { row: ".opening .line", id: "opening-line", center: false, source: "src/components/start/Opening.svelte", items: [
    { name: "icon", kind: "icon", el: ".dots" },
    { name: "text", kind: "text", el: ".label", textKind: "variable" },
  ] },
  { row: ".onboarding .lead", id: "onboarding-lead", center: false, source: "src/components/start/Onboarding.svelte", items: [{ name: "text", kind: "text", el: ":scope" }] },
];

const LIST = [
  { row: ".chats .new-chat", id: "new-chat", source: "src/components/ChatList.svelte", items: [
    { name: "icon", kind: "icon", el: "svg" },
    { name: "text", kind: "text", el: ".label" },
  ] },
  { row: ".chats .search", id: "search", source: "src/components/ChatList.svelte", items: [
    { name: "icon", kind: "icon", el: "svg" },
    { name: "text", kind: "text", el: "kbd" },
  ] },
  { row: ".chats .day", id: (el) => `day-${slug(el.textContent)}`, center: false, source: "src/components/ChatList.svelte", items: [{ name: "text", kind: "text", el: ":scope", column: "list-left" }] },
  { row: ".chats .empty", id: "list-empty", center: false, source: "src/components/ChatList.svelte", items: [{ name: "text", kind: "text", el: ":scope", column: "list-left" }] },
  { row: ".chats .row .state", id: (el) => `state-${slug(el.closest("[data-id]")?.dataset.id)}`, center: false, source: "src/components/list/ChatRow.svelte", items: [
    { name: "icon", kind: "icon", el: "svg" },
    { name: "text", kind: "text", el: "span", textKind: "variable" },
  ] },
];

export const gateMarks = () => collectMarks(GATE);
export const startMarks = () => collectMarks(START);
export const listMarks = () => collectMarks(LIST);
