// Program colors (CHAT-045). Port of internal/ui/common/ansi16.go: the
// terminal's RemapANSI16 (:19) rewrites the 16 basic colors to the theme's
// palette, StripCursorControl (:126) drops cursor moves, erases and mode
// switches, and simulateCarriageReturns (:199) keeps the text after a line's
// last \r, as a real terminal would show it. Here the result becomes nodes:
// text in spans whose classes name the 16 colors (--ansi-0 … --ansi-15, in
// ansi.css) and whose 256 and true colors are set through the element's style
// object (CSSOM, which the content rule allows). No HTML string is ever made
// (desktop-design 1.7 item 8).
//
//   stripCursorControl(s)  ansi16.go:126, rule for rule (keeps colors, OSC, unknown CSI)
//   simulateCarriageReturns(s)  ansi16.go:199
//   ansiToNodes(s)         one fragment; plain CSI m sequences become spans,
//                          every other escape sequence is dropped
//   ansiLines(s)           one fragment per line, colors carried across lines
//   renderAnsi(s)          stripCursorControl, then ansiToNodes
//   stripAnsi(s)           charmbracelet/x/ansi Strip: escape sequences and DEL removed
import "./ansi.css";

const ESC = "\x1b";

// The length of the escape sequence that starts at s[i] (s[i] is ESC), read
// the way the VT500 parser of charmbracelet/x/ansi reads it: CSI up to its
// final byte, OSC up to BEL or ST, DCS/SOS/PM/APC up to ST, otherwise ESC,
// intermediates and one final byte. An unfinished sequence runs to the end.
export function escapeLength(s, i) {
  if (i + 1 >= s.length) return 1;
  const c = s[i + 1];
  if (c === "[") {
    let j = i + 2;
    while (j < s.length && s.charCodeAt(j) >= 0x20 && s.charCodeAt(j) <= 0x3f) j++;
    if (j < s.length && s.charCodeAt(j) >= 0x40 && s.charCodeAt(j) <= 0x7e) return j + 1 - i;
    return j - i;
  }
  if (c === "]" || c === "P" || c === "X" || c === "^" || c === "_") {
    for (let j = i + 2; j < s.length; j++) {
      if (s[j] === "\x07" && c === "]") return j + 1 - i;
      if (s[j] === ESC && s[j + 1] === "\\") return j + 2 - i;
    }
    return s.length - i;
  }
  let j = i + 1;
  while (j < s.length && s.charCodeAt(j) >= 0x20 && s.charCodeAt(j) <= 0x2f) j++;
  if (j < s.length && s.charCodeAt(j) >= 0x30 && s.charCodeAt(j) <= 0x7e) return j + 1 - i;
  return j - i;
}

// charmbracelet/x/ansi Strip (width.go:11): printable text and control
// characters stay; escape sequences and DEL go.
export function stripAnsi(s) {
  s = String(s);
  if (!s.includes(ESC) && !s.includes("\x7f")) return s;
  let out = "";
  for (let i = 0; i < s.length; ) {
    if (s[i] === ESC) {
      i += escapeLength(s, i);
    } else {
      if (s[i] !== "\x7f") out += s[i];
      i++;
    }
  }
  return out;
}

// A CSI sequence's parts: its private prefix (? > < =), parameters,
// intermediates and final byte.
function csiParts(seq) {
  const body = seq.slice(2);
  const final = body.length ? body[body.length - 1] : "";
  let inner = body.slice(0, -1);
  let prefix = "";
  if (/^[<=>?]/.test(inner)) {
    prefix = inner[0];
    inner = inner.slice(1);
  }
  const intermediates = (inner.match(/[\x20-\x2f]+$/) ?? [""])[0];
  const params = inner.slice(0, inner.length - intermediates.length);
  return { prefix, params, intermediates, final };
}

const CURSOR_FINALS = new Set(["A", "B", "C", "D", "E", "F", "G", "H", "f", "J", "K", "S", "T", "s", "u"]);

// ansi16.go:126 StripCursorControl.
export function stripCursorControl(s) {
  s = String(s);
  if (!s.includes(ESC) && !s.includes("\r")) return s;
  let out = "";
  for (let i = 0; i < s.length; ) {
    if (s[i] !== ESC) {
      out += s[i];
      i++;
      continue;
    }
    const n = escapeLength(s, i);
    const seq = s.slice(i, i + n);
    i += n;
    if (seq[1] === "[") {
      const { final } = csiParts(seq);
      if (final === "m") out += seq; // SGR: kept
      else if (final === "h" || final === "l") {
        // DEC private (and plain) mode set and reset: stripped
      } else if (CURSOR_FINALS.has(final)) {
        // cursor moves, erases, scrolls, save and restore: stripped
      } else out += seq; // unknown CSI: kept
    } else if (seq.length === 2 && (seq[1] === "7" || seq[1] === "8")) {
      // DEC save and restore cursor: stripped
    } else {
      out += seq;
    }
  }
  return out.includes("\r") ? simulateCarriageReturns(out) : out;
}

// ansi16.go:199 simulateCarriageReturns: within each line the text after the
// last \r wins.
export function simulateCarriageReturns(s) {
  return String(s)
    .split("\n")
    .map((line) => {
      const at = line.lastIndexOf("\r");
      return at >= 0 ? line.slice(at + 1) : line;
    })
    .join("\n");
}

// The xterm 256-color palette for indexes 16-255 (0-15 are the theme's).
export function rgb256(n) {
  if (n >= 232) {
    const v = 8 + (n - 232) * 10;
    return [v, v, v];
  }
  const i = n - 16;
  const level = (k) => (k === 0 ? 0 : 55 + k * 40);
  return [level(Math.floor(i / 36)), level(Math.floor(i / 6) % 6), level(i % 6)];
}

function initialState() {
  return { fg: null, bg: null, bold: false, dim: false, italic: false, underline: false, inverse: false, conceal: false, strike: false };
}

// A color: {index: 0-15} drawn with a class, or {rgb: [r, g, b]} drawn with
// the style object.
function colorFrom(params, at) {
  const kind = params[at + 1];
  if (kind === 5 && params.length > at + 2) {
    const n = params[at + 2];
    if (n >= 0 && n <= 15) return { color: { index: n }, used: 3 };
    if (n >= 16 && n <= 255) return { color: { rgb: rgb256(n) }, used: 3 };
    return { color: null, used: 3 };
  }
  if (kind === 2 && params.length > at + 4) {
    const [r, g, b] = params.slice(at + 2, at + 5).map((v) => Math.max(0, Math.min(255, v)));
    return { color: { rgb: [r, g, b] }, used: 5 };
  }
  return { color: null, used: kind === undefined ? 1 : 2 };
}

// The numbers of one SGR sequence. "38:2::r:g:b" (colons) is read like
// "38;2;r;g;b"; an empty parameter is 0.
function sgrNumbers(params) {
  if (params === "") return [0];
  const out = [];
  for (const part of params.split(";")) {
    if (part.includes(":")) {
      const sub = part.split(":").map((v) => (v === "" ? 0 : parseInt(v, 10) || 0));
      // 38:2:<colorspace>:r:g:b carries a color space id before r g b.
      if ((sub[0] === 38 || sub[0] === 48 || sub[0] === 58) && sub[1] === 2 && sub.length >= 6) sub.splice(2, 1);
      out.push(...sub);
    } else out.push(part === "" ? 0 : parseInt(part, 10) || 0);
  }
  return out;
}

function applySgr(state, params) {
  const p = sgrNumbers(params);
  for (let i = 0; i < p.length; i++) {
    const n = p[i];
    if (n === 0) Object.assign(state, initialState());
    else if (n === 1) state.bold = true;
    else if (n === 2) state.dim = true;
    else if (n === 3) state.italic = true;
    else if (n === 4 || n === 21) state.underline = true;
    else if (n === 7) state.inverse = true;
    else if (n === 8) state.conceal = true;
    else if (n === 9) state.strike = true;
    else if (n === 22) state.bold = state.dim = false;
    else if (n === 23) state.italic = false;
    else if (n === 24) state.underline = false;
    else if (n === 27) state.inverse = false;
    else if (n === 28) state.conceal = false;
    else if (n === 29) state.strike = false;
    else if (n >= 30 && n <= 37) state.fg = { index: n - 30 };
    else if (n >= 90 && n <= 97) state.fg = { index: 8 + n - 90 };
    else if (n >= 40 && n <= 47) state.bg = { index: n - 40 };
    else if (n >= 100 && n <= 107) state.bg = { index: 8 + n - 100 };
    else if (n === 39) state.fg = null;
    else if (n === 49) state.bg = null;
    else if (n === 38 || n === 48 || n === 58) {
      const { color, used } = colorFrom(p, i);
      if (n === 38) state.fg = color ?? state.fg;
      else if (n === 48) state.bg = color ?? state.bg;
      i += used - 1;
    }
  }
}

function plainState(state) {
  return !state.fg && !state.bg && !state.bold && !state.dim && !state.italic && !state.underline && !state.inverse && !state.conceal && !state.strike;
}

function css([r, g, b]) {
  return `rgb(${r}, ${g}, ${b})`;
}

// One run of text in the current state.
function runNode(doc, text, state) {
  const node = doc.createTextNode(text);
  if (plainState(state)) return node;
  const span = doc.createElement("span");
  const classes = [];
  let fg = state.fg;
  let bg = state.bg;
  if (state.inverse) {
    [fg, bg] = [bg, fg];
    classes.push("ansi-inverse");
    if (!fg) classes.push("ansi-fg-paper");
    if (!bg) classes.push("ansi-bg-ink");
  }
  if (fg?.index !== undefined) classes.push(`ansi-fg-${fg.index}`);
  if (bg?.index !== undefined) classes.push(`ansi-bg-${bg.index}`);
  if (state.bold) classes.push("ansi-bold");
  if (state.dim) classes.push("ansi-dim");
  if (state.italic) classes.push("ansi-italic");
  if (state.underline) classes.push("ansi-underline");
  if (state.strike) classes.push("ansi-strike");
  if (state.conceal) classes.push("ansi-conceal");
  if (classes.length) span.className = classes.join(" ");
  if (fg?.rgb) span.style.color = css(fg.rgb);
  if (bg?.rgb) span.style.backgroundColor = css(bg.rgb);
  span.append(node);
  return span;
}

// Walks s, calling text(runText, state) for every run and newline() at each
// "\n" when lines are wanted.
function walk(s, onText, onNewline) {
  const state = initialState();
  let run = "";
  const flush = () => {
    if (run) onText(run, state);
    run = "";
  };
  for (let i = 0; i < s.length; ) {
    const ch = s[i];
    if (ch === ESC) {
      const n = escapeLength(s, i);
      const seq = s.slice(i, i + n);
      i += n;
      if (seq[1] === "[") {
        const { prefix, params, intermediates, final } = csiParts(seq);
        if (final === "m" && !prefix && !intermediates) {
          flush();
          applySgr(state, params);
        }
      }
      continue;
    }
    if (ch === "\n" && onNewline) {
      flush();
      onNewline();
    } else if (ch !== "\x7f") run += ch;
    i++;
  }
  flush();
}

// The text with its colors as nodes, newlines kept as text.
export function ansiToNodes(s, doc = document) {
  const fragment = doc.createDocumentFragment();
  walk(String(s), (text, state) => fragment.append(runNode(doc, text, state)));
  return fragment;
}

// One fragment per line; a color set on one line carries on to the next, as
// in a terminal.
export function ansiLines(s, doc = document) {
  const lines = [doc.createDocumentFragment()];
  walk(
    String(s),
    (text, state) => lines[lines.length - 1].append(runNode(doc, text, state)),
    () => lines.push(doc.createDocumentFragment()),
  );
  return lines;
}

// stripCursorControl, then nodes: what a program's output looks like.
export function renderAnsi(s, doc = document) {
  return ansiToNodes(stripCursorControl(s), doc);
}
