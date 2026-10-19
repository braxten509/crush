// Test runs only (the probe's heavy fields, 1.9): the alignment rows of this
// piece's surfaces as the real window lays them out, for tests/driver/
// p3_side.py, which pairs them with the window's own capture and runs the
// align skill's checker on it (the method of tests/look/align_real.py, with
// the boxes read from this renderer instead of headless Chromium, which has
// no engine data to lay the panel out with). Only icons and fixed words are
// measured; names, numbers and states are data ("variable") and left out,
// as the align skill asks.
//
//   alignRows(doc) → [{id, label, rect, center, center_reason, elements: [
//     {id, kind: "icon"|"text", rect, text, text_kind, text_lines, reference_text,
//      reference_rect, group, column, column_anchor}]}]
// Rects are physical pixels [x, y, w, h] (the hidden screen's scale is 1).

const slug = (s) =>
  String(s ?? "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "")
    .slice(0, 32);

function physical(box, win) {
  const d = win.devicePixelRatio || 1;
  const x = Math.floor(box.left * d);
  const y = Math.floor(box.top * d);
  return [x, y, Math.ceil(box.right * d) - x, Math.ceil(box.bottom * d) - y];
}

function union(boxes) {
  const list = boxes.filter((b) => b && b.width > 0 && b.height > 0);
  if (list.length === 0) return null;
  const left = Math.min(...list.map((b) => b.left));
  const top = Math.min(...list.map((b) => b.top));
  const right = Math.max(...list.map((b) => b.right));
  const bottom = Math.max(...list.map((b) => b.bottom));
  return { left, top, right, bottom, width: right - left, height: bottom - top };
}

// The box that is really painted: inside the window and inside every
// scrolling or clipping box around it. null when any part is cut off.
function shown(el, box, win) {
  if (!box || box.width <= 0 || box.height <= 0) return false;
  if (box.left < 0 || box.top < 0 || box.right > win.innerWidth || box.bottom > win.innerHeight) return false;
  for (let p = el.parentElement; p && p !== el.ownerDocument.body; p = p.parentElement) {
    const style = win.getComputedStyle(p);
    if (style.visibility === "hidden" || style.opacity === "0") return false;
    if (/(auto|scroll|hidden|clip)/.test(style.overflowX + style.overflowY)) {
      const clip = p.getBoundingClientRect();
      if (box.left < clip.left - 0.5 || box.top < clip.top - 0.5 || box.right > clip.right + 0.5 || box.bottom > clip.bottom + 0.5) return false;
    }
  }
  return true;
}

// The first letter or digit of the text, as the align skill's reference.
function reference(el, doc) {
  const walker = doc.createTreeWalker(el, 4 /* NodeFilter.SHOW_TEXT */);
  const segmenter = new Intl.Segmenter(undefined, { granularity: "grapheme" });
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    for (const part of segmenter.segment(node.data)) {
      if (!/[\p{L}\p{N}]/u.test(part.segment)) continue;
      const range = doc.createRange();
      range.setStart(node, part.index);
      range.setEnd(node, part.index + part.segment.length);
      const box = range.getBoundingClientRect();
      if (box.width > 0) return { text: part.segment, box };
    }
  }
  return null;
}

function textBox(el, doc) {
  const range = doc.createRange();
  range.selectNodeContents(el);
  return union([...range.getClientRects()]);
}

function textLines(el, doc) {
  const range = doc.createRange();
  range.selectNodeContents(el);
  const bands = [];
  for (const b of [...range.getClientRects()].filter((b) => b.width && b.height).sort((a, c) => a.top - c.top)) {
    const band = bands.find((a) => Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top) > Math.min(a.bottom - a.top, b.height) / 2);
    if (band) {
      band.top = Math.min(band.top, b.top);
      band.bottom = Math.max(band.bottom, b.bottom);
    } else bands.push({ top: b.top, bottom: b.bottom });
  }
  return bands.length;
}

function textItem(el, id, extra, doc, win) {
  const box = textBox(el, doc);
  const ref = reference(el, doc);
  if (!box || !ref || !shown(el, box, win)) return null;
  return {
    id,
    kind: "text",
    rect: physical(box, win),
    text: el.textContent.trim(),
    text_kind: "fixed",
    text_lines: textLines(el, doc),
    reference_text: ref.text,
    reference_rect: physical(ref.box, win),
    group: "row",
    ...extra,
  };
}

function iconItem(el, id, extra, win) {
  const box = el.getBoundingClientRect();
  if (!shown(el, box, win)) return null;
  return { id, kind: "icon", rect: physical(box, win), group: "row", column_anchor: "center", ...extra };
}

function row(id, label, box, elements, win, center = true, reason = "") {
  const members = elements.filter(Boolean);
  if (!box || members.length === 0) return null;
  const out = { id, label, rect: physical(box, win), center, elements: members };
  if (!center) out.center_reason = reason;
  return out;
}

const LABEL_LINE = "a label alone in its line box: its letters, not its box, are what aligns";

function panelRows(doc, win) {
  const rows = [];
  const panel = doc.querySelector('[data-probe="panel"]');
  if (!panel) return rows;
  // The section labels share the panel's left edge, with the first meter's.
  for (const label of panel.querySelectorAll("h2.label")) {
    const id = `label-${slug(label.textContent)}`;
    rows.push(row(id, label.textContent.trim(), label.getBoundingClientRect(), [textItem(label, id, { column: "panel-label" }, doc, win)], win, false, LABEL_LINE));
  }
  // The meter labels share one line (all capitals, so their letters center alike).
  const meterLabels = [...panel.querySelectorAll(".usage .cell .label")];
  const meterItems = meterLabels.map((label, i) =>
    textItem(label, `meter-${slug(label.textContent)}`, i === 0 ? { column: "panel-label" } : {}, doc, win),
  );
  rows.push(row("meter-labels", "Meter labels", union(meterLabels.map((l) => l.getBoundingClientRect())), meterItems, win, false, "the labels' own line"));
  // List rows: the state mark centered in its row; the marks of a section share a column.
  for (const section of panel.querySelectorAll("[data-section]")) {
    const name = section.dataset.section;
    [...section.querySelectorAll("li.row")].forEach((li, i) => {
      const status = li.querySelector(".status");
      const counts = [...li.querySelectorAll(".count:not(.letter) .icon")];
      rows.push(
        row(
          `${name}-row-${i + 1}`,
          `${name} row ${i + 1}`,
          li.getBoundingClientRect(),
          [
            status && iconItem(status, `${name}-${i + 1}-state`, { column: `${name}-state` }, win),
            ...counts.map((icon, j) => iconItem(icon, `${name}-${i + 1}-count-${j + 1}`, {}, win)),
          ],
          win,
        ),
      );
    });
  }
  return rows;
}

function stripRows(doc, win) {
  const strip = doc.querySelector("[data-meter-strip]");
  if (!strip) return [];
  const items = [];
  strip.querySelectorAll(".item").forEach((item, i) => {
    const label = item.querySelector(".label");
    if (label) items.push(textItem(label, `strip-${slug(label.textContent)}`, {}, doc, win));
    const icon = item.querySelector(".icon");
    if (icon) items.push(iconItem(icon, `strip-warning-${i + 1}`, {}, win));
  });
  return [row("meter-strip", "Meter strip", strip.getBoundingClientRect(), items, win)];
}

function noticeRows(doc, win) {
  const notice = doc.querySelector("[data-notice-kind]");
  if (!notice) return [];
  const icon = notice.querySelector(".mark .icon");
  const action = notice.querySelector("[data-notice-action]");
  return [
    row("notice", "Notice line", notice.getBoundingClientRect(), [icon && iconItem(icon, "notice-icon", {}, win), action && textItem(action, "notice-action", {}, doc, win)], win),
  ];
}

function helpRows(doc, win) {
  const sheet = doc.querySelector("[data-help-sheet]");
  if (!sheet) return [];
  const groups = sheet.querySelector(".groups");
  const box = groups?.getBoundingClientRect();
  const style = groups ? win.getComputedStyle(groups) : null;
  const left = box ? box.left + parseFloat(style.paddingLeft) : 0;
  const width = box ? box.width - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight) : 0;
  const rows = [];
  // Group labels in one of the three columns share its left edge.
  for (const label of sheet.querySelectorAll(".group .label")) {
    const at = label.getBoundingClientRect();
    const column = width > 0 ? Math.min(2, Math.max(0, Math.floor(((at.left - left + 4) / width) * 3))) : 0;
    const id = `help-${slug(label.textContent)}`;
    rows.push(row(id, label.textContent.trim(), at, [textItem(label, id, { column: `help-column-${column + 1}` }, doc, win)], win, false, LABEL_LINE));
  }
  return rows;
}

function detailsRows(doc, win) {
  const details = doc.querySelector("[data-details-overlay]");
  if (!details) return [];
  const labels = [...details.querySelectorAll(".columns h2.label")];
  const rows = [
    row(
      "details-labels",
      "Details section labels",
      union(labels.map((l) => l.getBoundingClientRect())),
      labels.map((l) => textItem(l, `details-${slug(l.textContent)}`, {}, doc, win)),
      win,
      false,
      "the labels' own line",
    ),
  ];
  for (const section of details.querySelectorAll("[data-section]")) {
    const name = section.dataset.section;
    [...section.querySelectorAll("li.row")].forEach((li, i) => {
      const status = li.querySelector(".status");
      rows.push(row(`details-${name}-row-${i + 1}`, `details ${name} row ${i + 1}`, li.getBoundingClientRect(), [status && iconItem(status, `details-${name}-${i + 1}-state`, { column: `details-${name}-state` }, win)], win));
    });
  }
  return rows;
}

function viewerRows(doc, win) {
  const bar = doc.querySelector("[data-image-viewer] .bar");
  if (!bar) return [];
  const buttons = [...bar.querySelectorAll(".buttons button")];
  return [row("viewer-bar", "Viewer bar", bar.getBoundingClientRect(), buttons.map((b) => textItem(b, `viewer-${slug(b.textContent)}`, {}, doc, win)), win)];
}

function detailPopoverRows(doc, win) {
  const button = doc.querySelector("#meter-detail .customize");
  if (!button) return [];
  return [row("meter-detail-customize", "Customize… button", button.getBoundingClientRect(), [textItem(button, "meter-detail-customize", {}, doc, win)], win, false, LABEL_LINE)];
}

export function alignRows(doc = globalThis.document) {
  const win = doc.defaultView;
  return [
    ...panelRows(doc, win),
    ...stripRows(doc, win),
    ...noticeRows(doc, win),
    ...helpRows(doc, win),
    ...detailsRows(doc, win),
    ...viewerRows(doc, win),
    ...detailPopoverRows(doc, win),
  ].filter(Boolean);
}
