// Markdown tokens → page nodes (CHAT-014, CHAT-017; desktop-design 1.7 items
// 1-4, 4.1). marked gives tokens only; this builds every element itself with
// createElement and text nodes, never from an HTML string. Elements it makes:
// p h1-h6 ul ol li blockquote pre code a em strong del br hr span, and table
// thead tbody tr th td for a figure (img only in figures, from data: and
// blob:). Attributes: class (alignment too), href (checked by links.js),
// draggable and rel on anchors, data-* for its own bookkeeping.
//
// Every text node is registered in the root's SourceMap with the stored
// offsets of its characters; inline marks, links and code spans are registered
// as spans (so a selection of their whole content brings the marks back), and
// chips, math, task boxes, rules, line breaks and fence lines as atoms.
//
//   new Builder({doc, map, toStored, messageId, visual})
//     .setVisuals(tokens, base)   numbers the visuals among tokens from base
//     .block(token, start)        the node for one top-level token at lexed
//                                 offset start, or null
//     .table(token, abs)          a table token as a <table> (figures)
//   alignLines(derived, raw)      offsets of a container's inner text → its raw
import { highlight } from "./code.js";
import { KIND_LABEL, figureOf, visualBlocks } from "./figures.js";
import { makeAnchor, safeHref } from "./links.js";
import { renderMath } from "./math.js";
import { ENTITY_REF, decodeEntities, decodeEntity, htmlTextUnits, stripTags } from "./sanitize.js";
import { emitText } from "./sourcemap.js";
import "./markdown.css";

const ENTITY_AT = new RegExp(ENTITY_REF.source, "y");

const shift = (abs, k) => (k === 0 ? abs : (i) => abs(k + i));

// derived (a container's inner text: its children's raws joined) mapped onto
// the container's raw: line by line, by suffix when the inner line is the raw
// line without its prefix ("> ", list indentation), else by the characters in
// order. Returns an array: derived offset (0 … length) → raw offset.
export function alignLines(derived, raw) {
  const map = new Int32Array(derived.length + 1);
  const dLines = derived.split("\n");
  const rLines = raw.split("\n");
  if (dLines.length > rLines.length) {
    let j = 0;
    for (let i = 0; i < derived.length; i++) {
      const k = raw.indexOf(derived[i], j);
      if (k >= 0) {
        map[i] = k;
        j = k + 1;
      } else map[i] = j;
    }
    map[derived.length] = j;
    return map;
  }
  let d = 0;
  let base = 0;
  for (let i = 0; i < dLines.length; i++) {
    const line = dLines[i];
    const src = rLines[i];
    if (src.endsWith(line)) {
      const off = base + src.length - line.length;
      for (let k = 0; k < line.length; k++) map[d + k] = off + k;
    } else {
      let j = 0;
      for (let k = 0; k < line.length; k++) {
        const at = src.indexOf(line[k], j);
        if (at >= 0) {
          map[d + k] = base + at;
          j = at + 1;
        } else map[d + k] = base + j;
      }
    }
    d += line.length;
    map[d] = base + src.length;
    if (i < dLines.length - 1) d++;
    base += src.length + 1;
  }
  return map;
}

function defaultChip(doc, info) {
  const chip = info.href ? makeAnchor(doc, info.href) : doc.createElement("span");
  chip.className = info.inline ? "md-chip md-chip-inline" : "md-chip";
  chip.dataset.kind = info.kind;
  if (info.figureId) chip.dataset.figure = info.figureId;
  const part = (className, text = "") => {
    const node = doc.createElement("span");
    node.className = className;
    if (text) node.textContent = text;
    return node;
  };
  const line = part("md-chip-line");
  const dotbox = part("md-chip-dotbox");
  dotbox.append(part("md-chip-dot"));
  line.append(part("md-chip-kind", KIND_LABEL[info.kind]), dotbox, part("md-chip-title", info.host ?? info.title));
  chip.append(line);
  return chip;
}

export class Builder {
  constructor({ doc = document, map, toStored, messageId = "", sessionId = "", visual = null }) {
    this.doc = doc;
    this.map = map;
    this.S = toStored;
    this.messageId = messageId;
    this.sessionId = sessionId;
    this.visual = visual;
    this.visuals = new Map(); // token → visual (figures.js), index counted from the base
    this.countByTop = [];
    this.stable = true; // the block being built is before the stable boundary
    this.created = []; // visual nodes made since the caller last emptied it
    this.inLink = 0;
  }

  setVisuals(tokens, base = 0) {
    this.countByTop = tokens.map(() => 0);
    for (const visual of visualBlocks(tokens)) {
      this.visuals.set(visual.token, { ...visual, index: base + visual.index });
      this.countByTop[visual.top]++;
    }
    return this.countByTop;
  }

  el(tag, className = "") {
    const node = this.doc.createElement(tag);
    if (className) node.className = className;
    return node;
  }

  atom(node, a0, a1) {
    const s0 = this.S(a0);
    this.map.setAtom(node, s0, Math.max(s0, this.S(a1)));
  }

  span(node, a0, a1, c0, c1) {
    this.map.setSpan(node, this.S(a0), this.S(a1), this.S(c0), this.S(c1));
  }

  // Display text → pieces in stored offsets: one piece per run of characters
  // that map one to one, one per decoded entity or escape.
  pieces(raw, abs, { entities = true, shown = null } = {}) {
    const out = [];
    const push = (text, a0, a1) => {
      const s0 = this.S(a0);
      const s1 = Math.max(s0, this.S(a1));
      const last = out[out.length - 1];
      if (last && text.length === s1 - s0 && last.text.length === last.s1 - last.s0 && last.s1 === s0) {
        last.text += text;
        last.s1 = s1;
      } else out.push({ text, s0, s1 });
    };
    for (let k = 0; k < raw.length; ) {
      if (entities && raw[k] === "&") {
        ENTITY_AT.lastIndex = k;
        const m = ENTITY_AT.exec(raw);
        if (m) {
          const value = decodeEntity(m[0]);
          if (value !== m[0]) {
            push(value, abs(k), abs(k + m[0].length - 1) + 1);
            k += m[0].length;
            continue;
          }
        }
      }
      const a = abs(k);
      push(shown ? shown[k] : raw[k], a, a + 1);
      k++;
    }
    return out;
  }

  text(parent, raw, abs, options) {
    if (raw) emitText(this.map, parent, this.pieces(raw, abs, options));
  }

  // Block tokens whose raws follow each other in the text abs maps.
  blocks(tokens, abs, parent) {
    let at = 0;
    for (const token of tokens) {
      const node = this.blockAt(token, shift(abs, at));
      if (node) parent.append(node);
      at += token.raw.length;
    }
  }

  block(token, start) {
    return this.blockAt(token, (i) => start + i);
  }

  blockAt(token, abs) {
    switch (token.type) {
      case "space":
      case "def":
        return null;
      case "paragraph":
        return this.paragraph(token, abs);
      case "heading": {
        const h = this.el(`h${token.depth}`, "md-h");
        const lead = /^ {0,3}#{1,6}(?=[ \t]|$)[ \t]*/.exec(token.raw);
        let off = token.raw.indexOf(token.text, lead ? lead[0].length : 0);
        if (off < 0) off = lead ? lead[0].length : 0;
        this.inline(token.tokens, shift(abs, off), h);
        return h;
      }
      case "code":
        return this.visuals.has(token) ? this.visualNode(token, abs, false) : this.code(token, abs);
      case "table":
      case "mathBlock":
        return this.visuals.has(token) ? this.visualNode(token, abs, false) : this.plain(token, abs);
      case "blockquote": {
        const quote = this.el("blockquote");
        this.container(token.tokens, token.raw, abs, quote);
        return quote;
      }
      case "list":
        return this.list(token, abs);
      case "hr": {
        const rule = this.el("span", "md-rule");
        rule.append(this.el("hr"));
        this.atom(rule, abs(0), abs(token.raw.length));
        return rule;
      }
      case "html":
        return this.html(token, abs);
      case "text": {
        // A tight list item's text: inline content straight into the item.
        const fragment = this.doc.createDocumentFragment();
        if (token.tokens) {
          let off = token.raw.indexOf(token.text);
          if (off < 0) off = 0;
          this.inline(token.tokens, shift(abs, off), fragment);
        } else this.text(fragment, token.raw, abs);
        return fragment;
      }
      case "checkbox":
        return this.taskBox(token, abs);
      default:
        return this.plain(token, abs);
    }
  }

  // Anything else: its source as text in a paragraph.
  plain(token, abs) {
    const p = this.el("p");
    this.text(p, token.raw, abs, { entities: false });
    return p;
  }

  // Children of a blockquote or a list item, mapped through the container.
  container(tokens, raw, abs, parent) {
    const derived = tokens.map((t) => t.raw).join("");
    const map = alignLines(derived, raw);
    this.blocks(tokens, (k) => abs(map[Math.min(k, derived.length)]), parent);
  }

  paragraph(token, abs) {
    let off = token.raw.indexOf(token.text);
    if (off < 0) off = 0;
    // A picture alone on its line is a chip where the block was.
    const only = token.tokens.length === 1 ? token.tokens[0] : null;
    if (only && only.type === "image" && this.visuals.has(only)) return this.visualNode(only, shift(abs, off), false);
    const p = this.el("p");
    this.inline(token.tokens, shift(abs, off), p);
    // A paragraph of raw inline tags only (<video></video>) shows nothing.
    if (p.children.length === 0 && !p.textContent.trim()) return null;
    return p;
  }

  list(token, abs) {
    const list = this.el(token.ordered ? "ol" : "ul", token.loose ? "md-list md-loose" : "md-list");
    const first = token.ordered ? Number(token.start === "" ? 1 : token.start) : 1;
    let at = 0;
    token.items.forEach((item, i) => {
      let off = token.raw.startsWith(item.raw, at) ? at : token.raw.indexOf(item.raw, at);
      if (off < 0) off = at;
      const li = this.el("li");
      if (token.ordered) li.dataset.n = String(first + i);
      if (item.task) {
        li.className = "md-task";
        li.dataset.checked = item.checked ? "true" : "false";
      }
      this.container(item.tokens, item.raw, shift(abs, off), li);
      list.append(li);
      at = off + item.raw.length;
    });
    return list;
  }

  // A table drawn as a table, for the panel's figure (the chat shows a chip):
  // the header cells in thead, the rows in tbody, a column's alignment as a
  // class. A cell is found in its line by its text, so its inline marks map
  // to their own characters.
  table(token, abs) {
    const lines = token.raw.split("\n");
    const starts = [];
    let at = 0;
    for (const line of lines) {
      starts.push(at);
      at += line.length + 1;
    }
    const row = (cells, line, tag) => {
      const tr = this.el("tr");
      const text = lines[line] ?? "";
      let cursor = 0;
      cells.forEach((cell, i) => {
        const align = token.align?.[i];
        const node = this.el(tag, align ? `md-align-${align}` : "");
        const found = text.indexOf(cell.text, cursor);
        const off = found >= 0 ? found : cursor;
        cursor = off + cell.text.length;
        this.inline(cell.tokens, shift(abs, (starts[line] ?? 0) + off), node);
        tr.append(node);
      });
      return tr;
    };
    const table = this.el("table", "md-table");
    const head = this.el("thead");
    head.append(row(token.header, 0, "th"));
    const body = this.el("tbody");
    token.rows.forEach((cells, r) => body.append(row(cells, r + 2, "td")));
    table.append(head, body);
    return table;
  }

  taskBox(token, abs) {
    const box = this.el("span", "md-taskbox");
    box.dataset.checked = token.checked ? "true" : "false";
    this.atom(box, abs(0), abs(token.raw.length));
    return box;
  }

  html(token, abs) {
    const text = stripTags(token.raw);
    if (!text) return null;
    const p = this.el("p", "md-html");
    // stripTags' text lined up, one code unit at a time, with the characters of
    // the raw HTML that show (tags left out); a decoded reference keeps the
    // range of the whole reference.
    const units = htmlTextUnits(token.raw);
    const chars = [];
    units.forEach((unit, u) => {
      for (let c = 0; c < unit.text.length; c++) chars.push([unit.text[c], u]);
    });
    const pieces = [];
    let j = 0;
    let after = 0; // raw offset after the last character found
    let lastUnit = -1;
    for (let k = 0; k < text.length; k++) {
      let m = j;
      while (m < chars.length && chars[m][0] !== text[k]) m++;
      let unit = -1;
      let s0;
      let s1;
      if (m < chars.length) {
        unit = chars[m][1];
        j = m + 1;
        const { start, end } = units[unit];
        s0 = this.S(abs(start));
        s1 = Math.max(s0, this.S(abs(end - 1) + 1));
        after = end;
      } else {
        // Not in the scan (the parser read something differently): an empty range.
        s0 = s1 = this.S(abs(after));
      }
      const last = pieces[pieces.length - 1];
      if (last && unit >= 0 && unit === lastUnit) last.text += text[k];
      else if (last && s1 - s0 === 1 && last.text.length === last.s1 - last.s0 && last.s1 === s0) {
        last.text += text[k];
        last.s1 = s1;
      } else pieces.push({ text: text[k], s0, s1 });
      lastUnit = unit;
    }
    emitText(this.map, p, pieces);
    return p;
  }

  code(token, abs) {
    const raw = token.raw;
    const pre = this.el("pre", "md-code");
    const body = this.el("code", "md-code-body");
    const fence = /^ {0,3}(`{3,}|~{3,})([^\n]*)/.exec(raw);
    let contentStart = 0;
    let contentEnd = raw.length;
    let closed = true;
    let closer = null;
    const lang = String(token.lang ?? "").trim().split(/\s+/)[0];
    if (fence) {
      const nl = raw.indexOf("\n");
      const openEnd = nl < 0 ? raw.length : nl;
      contentStart = nl < 0 ? raw.length : nl + 1;
      const trimmed = raw.endsWith("\n") ? raw.slice(0, -1) : raw;
      const lastNl = trimmed.lastIndexOf("\n");
      const lastLine = lastNl < 0 ? "" : trimmed.slice(lastNl + 1);
      const close = lastNl >= 0 && lastNl + 1 >= contentStart ? /^ {0,3}(`{3,}|~{3,})[ \t]*$/.exec(lastLine) : null;
      closed = Boolean(close && close[1][0] === fence[1][0] && close[1].length >= fence[1].length);
      contentEnd = closed ? lastNl : trimmed.length;
      // The opening fence line, with the language label drawn inside the block.
      const label = this.el("span", "md-code-lang");
      if (lang) label.textContent = lang;
      this.atom(label, abs(0), abs(openEnd));
      pre.append(label);
      if (closed) {
        closer = this.el("span", "md-fence");
        this.atom(closer, abs(lastNl + 1), abs(lastNl + 1 + lastLine.length));
      }
      pre.dataset.lang = lang.toLowerCase();
    }
    const text = token.text ?? "";
    const content = raw.slice(contentStart, Math.max(contentStart, contentEnd));
    const map = alignLines(text, content);
    // Highlighting waits for the closing fence (1.6 item 5).
    const fragment = closed ? highlight(text, fence ? lang : "plaintext", this.doc) : plainText(this.doc, text);
    let k = 0;
    const walker = this.doc.createTreeWalker(fragment, 4); // NodeFilter.SHOW_TEXT
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const len = node.data.length;
      const runs = [];
      for (let i = 0; i < len; i++, k++) {
        const a = abs(contentStart + map[Math.min(k, text.length)]);
        const s0 = this.S(a);
        const s1 = Math.max(s0, this.S(a + 1));
        const n = runs.length;
        if (n && s1 - s0 === 1 && runs[n - 3] - runs[n - 4] === runs[n - 1] - runs[n - 2] && runs[n - 1] === s0) {
          runs[n - 3]++;
          runs[n - 1] = s1;
        } else runs.push(i, i + 1, s0, s1);
      }
      this.map.setText(node, runs);
    }
    body.append(fragment);
    pre.append(body);
    if (closer) pre.append(closer);
    return pre;
  }

  visualNode(token, abs, inline) {
    const visual = this.visuals.get(token);
    const figureId = this.stable ? `${this.messageId}:${visual.index}` : null;
    const info = {
      kind: visual.kind,
      index: visual.index,
      title: visual.title,
      source: visual.source,
      figureId,
      inline,
      figure: figureOf(this.sessionId, this.messageId, visual),
    };
    if (visual.host) {
      info.host = visual.host;
      info.href = visual.src;
    }
    const node = this.visual?.(info) ?? defaultChip(this.doc, info);
    node.dataset.visual = String(visual.index);
    this.atom(node, abs(0), abs(token.raw.length));
    this.created.push(node);
    return node;
  }

  inline(tokens, abs, parent) {
    let at = 0;
    for (const token of tokens ?? []) {
      this.inlineAt(token, shift(abs, at), parent);
      at += token.raw.length;
    }
  }

  inlineAt(token, abs, parent) {
    switch (token.type) {
      case "text":
        if (token.tokens) {
          let off = token.raw.indexOf(token.text);
          if (off < 0) off = 0;
          this.inline(token.tokens, shift(abs, off), parent);
        } else this.text(parent, token.raw, abs);
        return;
      case "escape":
        emitText(this.map, parent, [{ text: token.text, s0: this.S(abs(0)), s1: this.S(abs(token.raw.length - 1) + 1) }]);
        return;
      case "codespan":
        return this.codespan(token, abs, parent);
      case "strong":
      case "em":
      case "del":
        return this.mark(token, abs, parent);
      case "link":
        return this.link(token, abs, parent);
      case "image":
        return this.image(token, abs, parent);
      case "br": {
        const br = this.el("br");
        this.atom(br, abs(0), abs(token.raw.length));
        parent.append(br);
        return;
      }
      case "html":
        // Raw HTML shows as its text with every tag removed (1.7 item 2). An
        // inline HTML token is one tag, comment or declaration: no text at
        // all; the words between two tags are text tokens of their own.
        return;
      case "mathInline": {
        const span = this.el("span", "md-math");
        const fragment = renderMath(token.display ? `\\displaystyle ${token.text}` : token.text, { doc: this.doc });
        if (fragment) span.append(fragment);
        else span.textContent = token.raw;
        this.atom(span, abs(0), abs(token.raw.length));
        parent.append(span);
        return;
      }
      case "checkbox":
        parent.append(this.taskBox(token, abs));
        return;
      default:
        this.text(parent, token.raw, abs, { entities: false });
    }
  }

  codespan(token, abs, parent) {
    const code = this.el("code", "md-icode");
    const ticks = /^`+/.exec(token.raw)?.[0].length ?? 1;
    const inner = token.raw.slice(ticks, token.raw.length - ticks);
    const off = inner.length === token.text.length ? ticks : ticks + 1;
    // marked turns a code span's newlines into spaces: same length, so every
    // shown character still stands for one stored character.
    this.text(code, inner.slice(off - ticks, off - ticks + token.text.length), shift(abs, off), { entities: false, shown: token.text });
    this.span(code, abs(0), abs(token.raw.length), abs(off), abs(off + token.text.length));
    parent.append(code);
  }

  mark(token, abs, parent) {
    const node = this.el(token.type);
    const inner = token.tokens.map((t) => t.raw).join("");
    const d = Math.max(0, Math.floor((token.raw.length - inner.length) / 2));
    let off = token.raw.startsWith(inner, d) ? d : token.raw.indexOf(inner);
    if (off < 0) off = d;
    this.inline(token.tokens, shift(abs, off), node);
    this.span(node, abs(0), abs(token.raw.length), abs(off), abs(off + inner.length));
    parent.append(node);
  }

  link(token, abs, parent) {
    const kept = safeHref(decodeEntities(token.href ?? ""));
    const node = kept ? makeAnchor(this.doc, kept) : this.el("span", "md-link-refused");
    const label = token.tokens.map((t) => t.raw).join("");
    let off = token.raw[0] === "[" || token.raw[0] === "<" ? 1 : 0;
    if (!token.raw.startsWith(label, off)) {
      const at = token.raw.indexOf(label);
      off = at >= 0 ? at : off;
    }
    this.inLink++;
    this.inline(token.tokens, shift(abs, off), node);
    this.inLink--;
    this.span(node, abs(0), abs(token.raw.length), abs(off), abs(off + label.length));
    parent.append(node);
  }

  image(token, abs, parent) {
    if (!this.inLink && this.visuals.has(token)) {
      parent.append(this.visualNode(token, abs, true));
      return;
    }
    // Inside a link, or an address that may not be shown: the alt text.
    const alt = token.text ?? "";
    let off = token.raw.indexOf(alt, 2);
    if (off < 0) off = 2;
    this.text(parent, alt, shift(abs, off));
  }
}

function plainText(doc, text) {
  const fragment = doc.createDocumentFragment();
  if (text) fragment.append(doc.createTextNode(text));
  return fragment;
}
