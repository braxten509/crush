// The end of tests/out/look_align/in/collect.js (tests/look/align_app.py puts
// it together): runs the align skill's collector on window.__alignSurface,
// adds the text boxes and the sheet's layout edges, takes the annotations
// off again and returns the lot to probe_eval.
const surface = window.__alignSurface;
if (!document.querySelector(surface.selector)) surface.selector = surface.kind === "dialog" ? `[data-dialog="${surface.id}"]` : "body";
const collected = await window.workbenchAlign.collect(surface);
// A block-level label's box spans its whole column; measure the text itself.
collected.text_rects = Object.fromEntries([...document.querySelectorAll('[data-align-kind="text"]')].map((el) => {
  const range = document.createRange();
  range.selectNodeContents(el);
  const boxes = [...range.getClientRects()].filter((b) => b.width && b.height);
  if (!boxes.length) return [el.dataset.alignItem, null];
  const x = Math.min(...boxes.map((b) => b.left)), y = Math.min(...boxes.map((b) => b.top));
  const r = Math.max(...boxes.map((b) => b.right)), btm = Math.max(...boxes.map((b) => b.bottom));
  return [el.dataset.alignItem, [Math.floor(x), Math.floor(y), Math.ceil(r) - Math.floor(x), Math.ceil(btm) - Math.floor(y)]];
}));
// Layout edges: the boxes whose left and right edges should line up inside a sheet.
const box = (el) => {
  const r = el.getBoundingClientRect();
  return r.width && r.height ? [r.left, r.top, r.width, r.height] : null;
};
const front = [...document.querySelectorAll("[data-dialog]")].filter((el) => !el.inert).pop();
const parts = {};
if (front) {
  const want = {
    body: "[data-kit-body]", fields: "[data-kit-field]", lists: "[data-kit-list]",
    rows: "[data-kit-list] .row", areas: "[data-kit-area]", buttons: "[data-kit-buttons]", help: "[data-kit-help] > p.line",
    sections: ".kit-section", rules: ".rule",
  };
  for (const [name, selector] of Object.entries(want)) {
    // Thin boxes are rules, not content.
    parts[name] = [...front.querySelectorAll(selector)].map(box).filter((b) => b && b[3] >= 4);
  }
  // A list's box and its rows' frames span the sheet (the row highlight is
  // full width); the rows' first text is what lines up.
  parts.lists = [...front.querySelectorAll("[data-kit-list] .row .title, [data-kit-list] .row .name")].slice(0, 1).map((el) => {
    const range = document.createRange();
    range.selectNodeContents(el);
    const r = range.getBoundingClientRect();
    return r.width ? [r.left, r.top, r.width, r.height] : null;
  }).filter(Boolean);
  // The title's box spans the sheet; its text is what lines up.
  const title = front.querySelector("[data-kit-title] > .title");
  if (title) {
    const range = document.createRange();
    range.selectNodeContents(title);
    const r = range.getBoundingClientRect();
    parts.title = r.width ? [[r.left, r.top, r.width, r.height]] : [];
  }
}
collected.geometry = { dialog: front?.dataset.dialog ?? null, parts, viewport: [innerWidth, innerHeight], scale: devicePixelRatio };
for (const el of document.querySelectorAll("*")) {
  for (const name of [...el.getAttributeNames()]) if (name.startsWith("data-align")) el.removeAttribute(name);
}
return collected;
