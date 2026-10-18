// Your text as typed (CHAT-004). Port of internal/ui/chat/user.go:70 and
// renderPlain (:125): Go's strings.TrimSpace on the stored text, then
// charmbracelet/x/ansi Strip, then "\r\n" → "\n", "\r" → "\n" and a tab → four
// spaces, in that order. No Markdown: <tags>, *stars* and `backticks` stay as
// typed. Every shown character keeps the stored offsets it came from, so a
// selection names a range of M.Content().Text (phase 4's branch cut, 3.1).
//
//   userText(stored)   the shown text: a String (so it can be used as text
//                      anywhere) that also carries the map:
//                        .text                the shown text as a plain string
//                        .toSource(i, side)   shown offset → stored offset
//                        .toShown(j, side)    stored offset → shown offset
//                        .pieces              [{text, s0, s1}] in order
//   userTextInto(el, stored, {decorations})
//                      writes the shown text into el as text nodes registered
//                      in a SourceMap (sourceMapOf(el)), as replies are, so
//                      copy and branching read both kinds the same way.
import { escapeLength } from "./ansi.js";
import { trimSpaceRange } from "./strings.js";
import { SourceMap, emitText } from "./sourcemap.js";

// The stored text after TrimSpace and Strip, as characters with their stored
// offsets.
function stripped(stored) {
  const [a, b] = trimSpaceRange(stored);
  const s = stored.slice(a, b);
  const chars = [];
  for (let i = 0; i < s.length; ) {
    if (s[i] === "\x1b") {
      i += escapeLength(s, i);
      continue;
    }
    if (s[i] !== "\x7f") chars.push([s[i], a + i]);
    i++;
  }
  return { chars, a, b };
}

// The replacer over the stripped characters: pieces whose shown text and
// stored range have the same length map one to one; the rest map as a whole.
function piecesOf(stored) {
  const { chars, a, b } = stripped(stored);
  const pieces = [];
  let run = null;
  const flushRun = () => {
    if (run) pieces.push(run);
    run = null;
  };
  for (let k = 0; k < chars.length; k++) {
    const [ch, at] = chars[k];
    if (ch === "\r" || ch === "\t") {
      flushRun();
      if (ch === "\r" && chars[k + 1]?.[0] === "\n") {
        pieces.push({ text: "\n", s0: at, s1: chars[k + 1][1] + 1 });
        k++;
      } else if (ch === "\r") pieces.push({ text: "\n", s0: at, s1: at + 1 });
      else pieces.push({ text: "    ", s0: at, s1: at + 1 });
      continue;
    }
    // A plain character extends the run only when it is the next stored
    // character; a dropped escape between two characters starts a new run.
    if (run && run.s1 === at) {
      run.text += ch;
      run.s1 = at + 1;
    } else {
      flushRun();
      run = { text: ch, s0: at, s1: at + 1 };
    }
  }
  flushRun();
  return { pieces, a, b };
}

export class UserText extends String {
  constructor(stored) {
    const source = String(stored ?? "");
    const { pieces, a, b } = piecesOf(source);
    super(pieces.map((p) => p.text).join(""));
    this.source = source;
    this.pieces = pieces;
    this.trimmed = [a, b];
    const starts = [];
    let d = 0;
    for (const p of pieces) {
      starts.push(d);
      d += p.text.length;
    }
    this.starts = starts;
  }

  get text() {
    return this.valueOf();
  }

  // Shown offset i (0..length) → stored offset. side "start": where the
  // character at i starts (a point inside a tab's spaces takes the whole tab);
  // "end": where the character before i ends.
  toSource(i, side = "start") {
    const n = this.pieces.length;
    if (n === 0) return this.trimmed[0];
    const length = this.valueOf().length;
    if (side === "start") {
      if (i >= length) return this.pieces[n - 1].s1;
      const k = this.pieceAt(Math.max(0, i));
      return pointStart(this.pieces[k], Math.max(0, i) - this.starts[k]);
    }
    if (i <= 0) return this.pieces[0].s0;
    const k = this.pieceAt(Math.min(i, length) - 1);
    return pointEnd(this.pieces[k], Math.min(i, length) - this.starts[k]);
  }

  // Stored offset j → shown offset. A stored offset inside dropped text (an
  // escape sequence, trimmed space) goes to the next shown character; inside
  // a piece that maps as a whole, to its start (side "start") or end ("end").
  toShown(j, side = "start") {
    for (let k = 0; k < this.pieces.length; k++) {
      const p = this.pieces[k];
      if (j <= p.s0) return this.starts[k];
      if (j < p.s1) {
        if (p.text.length === p.s1 - p.s0) return this.starts[k] + (j - p.s0);
        return side === "start" ? this.starts[k] : this.starts[k] + p.text.length;
      }
    }
    return this.valueOf().length;
  }

  // The piece that holds shown character i.
  pieceAt(i) {
    let lo = 0;
    let hi = this.starts.length - 1;
    while (lo < hi) {
      const mid = (lo + hi + 1) >> 1;
      if (this.starts[mid] <= i) lo = mid;
      else hi = mid - 1;
    }
    return lo;
  }
}

function pointStart(piece, k) {
  if (piece.text.length === piece.s1 - piece.s0) return piece.s0 + k;
  return piece.s0;
}

function pointEnd(piece, k) {
  if (piece.text.length === piece.s1 - piece.s0) return piece.s0 + k;
  return piece.s1;
}

export function userText(stored) {
  return new UserText(stored);
}

// Writes the shown text into el (its old content is replaced) as registered
// text nodes; decorations ([{kind, start, end, title?}], stored offsets, for
// example phase 4's branch marks) are drawn as span.md-deco around their text.
// Returns the SourceMap.
export function userTextInto(el, stored, { decorations = [] } = {}) {
  const shown = userText(stored);
  el.replaceChildren();
  const map = new SourceMap(el, shown.source);
  map.decorations = [...decorations].sort((x, y) => x.start - y.start);
  emitText(map, el, shown.pieces);
  return map;
}
