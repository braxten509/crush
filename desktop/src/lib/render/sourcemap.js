// The renderer's source map (CHAT-088; phase 4's branch cuts, desktop-design
// 3.1). Every text node the renderer makes is registered with the stored-text
// offsets of its characters, so a point or a selection in the page turns back
// into a range of the stored Markdown (M.Content().Text), and a stored range
// turns into a page range. Offsets are UTF-16 code units, the engine's unit.
//
// What is registered, per rendered root (a reply's MarkdownView, a user
// message's text):
//   text nodes  runs [d0, d1, s0, s1]: display characters d0..d1 come from
//               stored s0..s1. When both spans have the same length the run is
//               one to one; otherwise (an escape, an entity, a tab drawn as
//               spaces) every character of the run stands for all of s0..s1.
//   atoms       elements that stand for one stored range as a whole (a visuals
//               chip, rendered math, a task box): a selection end inside one
//               takes all of it.
//   spans       inline marks (strong, em, del, code, links) with their whole
//               range and their content's range: a selection that covers the
//               whole content takes the marks too ("whole inline spans bring
//               their marks", CHAT-088 and 3.1).
//
//   sourceMapOf(root)                      the map of a rendered root, or null
//   map.pointToSource(node, offset, side)  side "start" (the first character at
//                                          or after the point) or "end" (the end
//                                          of the last character before it)
//   map.rangeToSource(range, {wholeSpans}) → {start, end} or null
//   map.sourceToRange(start, end)          → a DOM Range or null
//   map.decorations                        the decorations drawn in this root

const maps = new WeakMap(); // root → SourceMap

export function sourceMapOf(root) {
  return (root && maps.get(root)) ?? null;
}

export class SourceMap {
  constructor(root, source = "") {
    this.root = root;
    this.source = source;
    this.decorations = [];
    this.texts = new WeakMap(); // Text → number[] (runs, flat)
    this.atoms = new WeakMap(); // Element → [s0, s1]
    this.spans = new WeakMap(); // Element → [s0, s1, c0, c1]
    maps.set(root, this);
  }

  setText(node, runs) {
    this.texts.set(node, runs);
  }

  setAtom(el, s0, s1) {
    this.atoms.set(el, [s0, s1]);
  }

  setSpan(el, s0, s1, c0, c1) {
    this.spans.set(el, [s0, s1, c0, c1]);
  }

  // The innermost atom that holds node, inside the root.
  atomOf(node) {
    for (let n = node; n && n !== this.root; n = n.parentNode) {
      if (n.nodeType === 1 && this.atoms.has(n)) return n;
    }
    return null;
  }

  // The stored offset at a page point. side "start": where the first character
  // at or after the point starts; "end": where the last character before the
  // point ends. Points outside the root clamp to its ends; null when the root
  // shows no stored character at all.
  pointToSource(node, offset, side = "start") {
    if (!node) return null;
    if (!this.root.contains(node)) {
      const before = this.root.compareDocumentPosition(node) & Node.DOCUMENT_POSITION_PRECEDING;
      return before ? this.edge("start") : this.edge("end");
    }
    const atom = this.atomOf(node);
    if (atom) {
      const [s0, s1] = this.atoms.get(atom);
      return side === "start" ? s0 : s1;
    }
    if (node.nodeType === 3 && this.texts.has(node)) {
      const runs = this.texts.get(node);
      if (side === "start" && offset < node.data.length) {
        const hit = charRange(runs, offset);
        if (hit) return hit[0];
      }
      if (side === "end" && offset > 0) {
        const hit = charRange(runs, Math.min(offset, node.data.length) - 1);
        if (hit) return hit[1];
      }
    }
    // Between characters of different nodes: walk to the nearest unit.
    const unit = side === "start" ? this.nextUnit(node, offset) : this.prevUnit(node, offset);
    if (!unit) return side === "start" ? this.edge("end") : this.edge("start");
    return side === "start" ? unitStart(this, unit) : unitEnd(this, unit);
  }

  // The first stored offset (side "start") or the last (side "end") the root shows.
  edge(side) {
    const unit = side === "start" ? this.nextUnit(this.root, 0) : this.prevUnit(this.root, this.root.childNodes.length);
    if (!unit) return null;
    return side === "start" ? unitStart(this, unit) : unitEnd(this, unit);
  }

  // A page range → the stored range it shows: {start, end}, or null when it
  // shows no stored character. With wholeSpans (the default), marks come along
  // when the selection covers a whole mark's content.
  rangeToSource(range, { wholeSpans = true } = {}) {
    if (!range) return null;
    let start = this.pointToSource(range.startContainer, range.startOffset, "start");
    let end = this.pointToSource(range.endContainer, range.endOffset, "end");
    if (start === null || end === null || end <= start) return null;
    if (wholeSpans) [start, end] = this.widenToSpans(range, start, end);
    return { start, end };
  }

  // Widens [start, end) over every mark whose content it covers, inner marks
  // first, so `**`code`**` selected whole brings all four stars and both ticks.
  widenToSpans(range, start, end) {
    const candidates = new Set();
    const collect = (unit) => {
      for (let n = unit; n && n !== this.root; n = n.parentNode) {
        if (n.nodeType === 1 && this.spans.has(n)) candidates.add(n);
      }
    };
    const inside = (n) => n && this.root.contains(n);
    const first = inside(range.startContainer) ? this.nextUnit(range.startContainer, range.startOffset, true) : this.nextUnit(this.root, 0);
    const last = inside(range.endContainer) ? this.prevUnit(range.endContainer, range.endOffset, true) : this.prevUnit(this.root, this.root.childNodes.length);
    if (first) collect(first);
    if (last) collect(last);
    let changed = true;
    while (changed) {
      changed = false;
      for (const el of candidates) {
        const [s0, s1, c0, c1] = this.spans.get(el);
        if (start <= c0 && end >= c1 && (start > s0 || end < s1)) {
          start = Math.min(start, s0);
          end = Math.max(end, s1);
          changed = true;
        }
      }
    }
    return [start, end];
  }

  // A stored range → the page range that shows it, or null when none of it is drawn.
  sourceToRange(start, end) {
    if (!(end > start)) return null;
    let from = null;
    let to = null;
    for (const unit of this.units()) {
      const s0 = unitStart(this, unit);
      const s1 = unitEnd(this, unit);
      if (from === null && s1 > start) from = pointIn(this, unit, start, "start");
      if (s0 < end) to = pointIn(this, unit, end, "end");
      if (s0 >= end) break;
    }
    if (!from || !to) return null;
    const range = this.root.ownerDocument.createRange();
    range.setStart(from[0], from[1]);
    range.setEnd(to[0], to[1]);
    return range;
  }

  // Every unit (a registered text node, or an atom) in page order.
  *units() {
    yield* walkUnits(this, this.root);
  }

  // The first unit at or after a point. With inclusive, a text node point
  // counts its own node even past its last character (for span widening).
  nextUnit(node, offset, inclusive = false) {
    if (node.nodeType === 3) {
      const atom = this.atomOf(node);
      if (atom) return atom;
      if (this.texts.has(node) && (offset < node.data.length || inclusive)) return node;
      return this.unitAfter(node);
    }
    const atom = this.atomOf(node);
    if (atom) return atom;
    const child = node.childNodes[offset];
    if (child) {
      for (const unit of walkUnits(this, child)) return unit;
      return this.unitAfter(child);
    }
    return this.unitAfter(node, true);
  }

  // The last unit before a point.
  prevUnit(node, offset, inclusive = false) {
    if (node.nodeType === 3) {
      const atom = this.atomOf(node);
      if (atom) return atom;
      if (this.texts.has(node) && (offset > 0 || inclusive)) return node;
      return this.unitBefore(node);
    }
    const atom = this.atomOf(node);
    if (atom) return atom;
    const child = offset > 0 ? node.childNodes[offset - 1] : null;
    if (child) {
      let last = null;
      for (const unit of walkUnits(this, child)) last = unit;
      return last ?? this.unitBefore(child);
    }
    return this.unitBefore(node, true);
  }

  // The first unit after node's subtree (or after node itself when asked from
  // its end), inside the root.
  unitAfter(node, fromEnd = false) {
    let n = fromEnd ? node : node;
    for (;;) {
      if (n === this.root) return null;
      let next = n.nextSibling;
      while (next) {
        for (const unit of walkUnits(this, next)) return unit;
        next = next.nextSibling;
      }
      n = n.parentNode;
      if (!n) return null;
    }
  }

  unitBefore(node) {
    let n = node;
    for (;;) {
      if (n === this.root) return null;
      let prev = n.previousSibling;
      while (prev) {
        let last = null;
        for (const unit of walkUnits(this, prev)) last = unit;
        if (last) return last;
        prev = prev.previousSibling;
      }
      n = n.parentNode;
      if (!n) return null;
    }
  }
}

function* walkUnits(map, node) {
  if (node.nodeType === 3) {
    if (map.texts.has(node)) yield node;
    return;
  }
  if (node.nodeType !== 1 && node.nodeType !== 11) return;
  if (node.nodeType === 1 && map.atoms.has(node)) {
    yield node;
    return;
  }
  for (let child = node.firstChild; child; child = child.nextSibling) yield* walkUnits(map, child);
}

// The stored range [s0, s1) of display character k, or null.
function charRange(runs, k) {
  for (let i = 0; i < runs.length; i += 4) {
    const d0 = runs[i];
    const d1 = runs[i + 1];
    if (k < d0 || k >= d1) continue;
    const s0 = runs[i + 2];
    const s1 = runs[i + 3];
    if (d1 - d0 === s1 - s0) return [s0 + (k - d0), s0 + (k - d0) + 1];
    return [s0, s1];
  }
  return null;
}

function unitStart(map, unit) {
  if (unit.nodeType === 1) return map.atoms.get(unit)[0];
  const runs = map.texts.get(unit);
  return runs.length ? runs[2] : 0;
}

function unitEnd(map, unit) {
  if (unit.nodeType === 1) return map.atoms.get(unit)[1];
  const runs = map.texts.get(unit);
  return runs.length ? runs[runs.length - 1] : 0;
}

// A DOM point inside unit for a stored offset: the start of the first
// character that ends after it (side "start") or the end of the last
// character that starts before it (side "end").
function pointIn(map, unit, offset, side) {
  if (unit.nodeType === 1) {
    const parent = unit.parentNode;
    const at = Array.prototype.indexOf.call(parent.childNodes, unit);
    return side === "start" ? [parent, at] : [parent, at + 1];
  }
  const runs = map.texts.get(unit);
  const length = unit.data.length;
  if (side === "start") {
    for (let k = 0; k < length; k++) {
      const hit = charRange(runs, k);
      if (hit && hit[1] > offset) return [unit, k];
    }
    return [unit, length];
  }
  for (let k = length - 1; k >= 0; k--) {
    const hit = charRange(runs, k);
    if (hit && hit[0] < offset) return [unit, k + 1];
  }
  return [unit, 0];
}

// Writes display text into parent as text nodes registered in map, split
// where decorations begin and end. pieces: [{text, s0, s1}] in order; a piece
// whose text and stored range have the same length maps one to one, any other
// piece maps as a whole. decorations: [{kind, start, end, title?}] sorted by
// start (indexes into map.decorations).
export function emitText(map, parent, pieces, decorations = map.decorations) {
  const doc = parent.ownerDocument;
  // Cut pieces at decoration edges (one-to-one pieces only; a whole piece
  // belongs to the decoration that holds its start).
  const cuts = new Set();
  for (const deco of decorations) {
    cuts.add(deco.start);
    cuts.add(deco.end);
  }
  const parts = [];
  for (const piece of pieces) {
    if (!piece.text) continue;
    const linear = piece.text.length === piece.s1 - piece.s0;
    if (!linear || cuts.size === 0) {
      parts.push(piece);
      continue;
    }
    let from = 0;
    for (let k = 1; k < piece.text.length; k++) {
      if (cuts.has(piece.s0 + k)) {
        parts.push({ text: piece.text.slice(from, k), s0: piece.s0 + from, s1: piece.s0 + k });
        from = k;
      }
    }
    parts.push({ text: piece.text.slice(from), s0: piece.s0 + from, s1: piece.s1 });
  }
  // Group consecutive parts under the same decoration (or none) into one node.
  let group = [];
  let groupDeco = -1;
  const flush = () => {
    if (!group.length) return;
    const text = group.map((p) => p.text).join("");
    const node = doc.createTextNode(text);
    const runs = [];
    let d = 0;
    for (const p of group) {
      runs.push(d, d + p.text.length, p.s0, p.s1);
      d += p.text.length;
    }
    map.setText(node, runs);
    if (groupDeco >= 0) {
      const deco = decorations[groupDeco];
      const span = doc.createElement("span");
      span.className = `md-deco md-deco-${String(deco.kind).replace(/[^a-z0-9-]/gi, "")}`;
      span.dataset.deco = String(groupDeco);
      if (typeof deco.title === "string") span.title = deco.title;
      span.append(node);
      parent.append(span);
    } else parent.append(node);
    group = [];
  };
  for (const part of parts) {
    const at = decorations.findIndex((deco) => part.s0 >= deco.start && part.s0 < deco.end);
    if (at !== groupDeco) {
      flush();
      groupDeco = at;
    }
    group.push(part);
  }
  flush();
}
