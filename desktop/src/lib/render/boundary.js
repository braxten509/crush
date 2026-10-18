// The stable boundary of a streaming reply (CHAT-018; desktop-design 1.6
// item 4): the offset of the lexed text before which the rendered blocks can
// never change, however the reply goes on. A port of the terminal's rules,
// internal/ui/chat/markdown_boundary.go (findSafeMarkdownBoundary and its
// helpers, rule for rule) and the relaxed cut of streaming_markdown.go:258-290
// for long tails without a blank line.
//
// The terminal glues two independent renders and accepts a re-wrap at the
// cut; the desktop must draw exactly what a full render draws, so the rules
// gain four additions (each only ever refuses a cut the terminal would take,
// or is one the terminal applies only elsewhere):
//   1. A fence is open unless both the terminal's toggle count is even and a
//      CommonMark fence tracker (same character, closer at least as long, no
//      info string) agrees: "````" then "```" leaves the block open.
//   2. A link reference definition anywhere in the text (outside fences)
//      means no boundary at all: a definition that arrives later changes
//      links above it, which a cut could never redraw.
//   3. The relaxed cut lands only between two top-level blocks of the tail
//      (a cut inside a paragraph would split it in two) and only with a whole
//      line after it.
//   4. Rule 4 (no setext underline right after the cut) applies to relaxed
//      cuts only: after a blank line no underline can reach back, and keeping
//      it there would let the boundary move backward, so a figure could
//      vanish while the reply streams.
// The result never decreases as text is appended (figure ids rely on it).
//
//   stableBoundary(text, {lex})   0 when nothing is stable yet
import { trimSpace } from "./strings.js";

// streaming_markdown.go:258: a tail longer than this may take a relaxed cut.
export const RELAX_AFTER = 2 << 10;

export function stableBoundary(text, { lex = null } = {}) {
  if (!text) return 0;
  const lines = scanLines(text);
  if (lines.hasRefDef) return 0;
  const blank = latestBlankBoundary(text, lines);
  if (text.length - blank <= RELAX_AFTER || !lex) return blank;
  return Math.max(blank, relaxedBoundary(text, lines, blank, lex));
}

// One forward pass: for every line, where it starts and what the lines up to
// and including it hold. state[k] describes the prefix that ends before line k.
function scanLines(text) {
  const starts = [0];
  for (let i = text.indexOf("\n"); i >= 0; i = text.indexOf("\n", i + 1)) starts.push(i + 1);
  const count = starts.length;
  const line = (k) => text.slice(starts[k], k + 1 < count ? starts[k + 1] - 1 : text.length);
  // Prefix state before line k (k = 0 … count).
  const state = new Array(count + 1);
  let toggles = 0; // the terminal's countFenceLines
  let fence = null; // the CommonMark tracker: {char, length} while open
  let html = false;
  let listMarker = false;
  let lastNonBlank = -1; // index of the last line with TrimSpace(line) != ""
  let lastHazardLine = -1; // the terminal's lastNonBlankRaw for B1 (outside fences)
  let hasRefDef = false;
  let toggleInFence = false;
  state[0] = { toggles, fenceOpen: false, html, listMarker, lastNonBlank, lastHazardLine };
  for (let k = 0; k < count; k++) {
    const s = line(k);
    if (isFenceLine(s)) {
      toggles++;
      toggleInFence = !toggleInFence;
    } else if (!toggleInFence) {
      const trimmed = s.replace(/^[ \t]+/, "");
      if (trimmed !== "") {
        lastHazardLine = k;
        if (isListItemMarker(trimmed)) listMarker = true;
        if (isHTMLBlockOpener(s)) html = true;
        if (isLinkRefDefinition(s)) hasRefDef = true;
      }
    }
    fence = trackFence(fence, s);
    if (trimSpace(s) !== "") lastNonBlank = k;
    state[k + 1] = { toggles, fenceOpen: fence !== null, html, listMarker, lastNonBlank, lastHazardLine };
  }
  return { starts, count, line, state, hasRefDef };
}

// A CommonMark fence tracker over one line.
function trackFence(open, line) {
  const m = /^ {0,3}(`{3,}|~{3,})(.*)$/.exec(line);
  if (open) {
    if (m && m[1][0] === open.char && m[1].length >= open.length && /^[ \t]*$/.test(m[2])) return null;
    return open;
  }
  if (!m) return null;
  if (m[1][0] === "`" && m[2].includes("`")) return null;
  return { char: m[1][0], length: m[1].length };
}

// markdown_boundary.go:44 findSafeMarkdownBoundary, over the line scan: the
// latest p after a blank line whose prefix passes isSafeBoundaryAt.
function latestBlankBoundary(text, lines) {
  for (let k = lines.count - 1; k >= 2; k--) {
    // Line k starts right after a blank line (k - 1) that itself follows a newline.
    if (!isBlankOrSpaces(lines.line(k - 1))) continue;
    if (safePrefix(lines, k)) return lines.starts[k];
  }
  return 0;
}

// isSafeBoundaryAt's rules 2, 2b and 3 for the prefix that ends before line k.
function safePrefix(lines, k) {
  const st = lines.state[k];
  // (2) fences closed: the terminal's parity and the CommonMark tracker.
  if (st.toggles % 2 !== 0 || st.fenceOpen) return false;
  // (2b) B2: an HTML block opener anywhere in the prefix.
  if (st.html) return false;
  // (2b) B1 refined: a list marker earlier and an indented last line that is
  // not itself a marker.
  if (st.listMarker && st.lastHazardLine >= 0) {
    const last = lines.line(st.lastHazardLine);
    const trimmed = last.replace(/^[ \t]+/, "");
    if (!isListItemMarker(trimmed) && (last[0] === " " || last[0] === "\t")) return false;
  }
  // (3) the last non-blank line opens no construct.
  if (st.lastNonBlank >= 0 && lineOpensConstruct(lines.line(st.lastNonBlank))) return false;
  return true;
}

// streaming_markdown.go:276 relaxedBoundary, at block edges only (addition 3).
function relaxedBoundary(text, lines, from, lex) {
  const tokens = lex(text.slice(from));
  // The offsets between top-level blocks; whitespace-only (space) tokens may be cut.
  const edges = new Set();
  let at = from;
  for (const token of tokens) {
    if (token.type === "space") for (let i = at; i <= at + token.raw.length; i++) edges.add(i);
    else edges.add(at);
    at += token.raw.length;
  }
  edges.add(at);
  // Latest first; the line after the cut must be whole, so what follows the
  // cut is known (the last line, which may still grow, is never one).
  for (let k = lines.count - 2; k >= 1; k--) {
    const q = lines.starts[k];
    if (q <= from) break;
    if (!edges.has(q)) continue;
    if (!safePrefix(lines, k)) continue;
    // (4) what follows must not be a setext underline for the line above.
    if (isSetextUnderlineCandidate(firstNonBlankLine(text.slice(q)))) continue;
    return q;
  }
  return 0;
}

function firstNonBlankLine(s) {
  for (const line of s.split("\n")) if (trimSpace(line) !== "") return line;
  return "";
}

function isBlankOrSpaces(s) {
  for (const ch of s) if (ch !== " " && ch !== "\t") return false;
  return true;
}

// markdown_boundary.go:351 isFenceLine.
export function isFenceLine(line) {
  let i = 0;
  while (i < line.length && i < 3 && line[i] === " ") i++;
  if (i >= line.length) return false;
  const c = line[i];
  if (c !== "`" && c !== "~") return false;
  let run = 0;
  while (i < line.length && line[i] === c) {
    i++;
    run++;
  }
  return run >= 3;
}

// markdown_boundary.go:413 lineOpensConstruct.
export function lineOpensConstruct(line) {
  if (line.length > 0 && line[0] === "\t") return true;
  if (line.startsWith("    ")) return true;
  const trimmed = line.replace(/^[ \t]+/, "");
  if (trimmed === "") return false;
  if (trimmed[0] === ">") return true;
  if (isListItemMarker(trimmed)) return true;
  if (line.includes("|")) return true;
  if (isSetextUnderlineCandidate(trimmed)) return true;
  return false;
}

// markdown_boundary.go:459 isListItemMarker (line already left-trimmed).
export function isListItemMarker(line) {
  if (line === "") return false;
  const c = line[0];
  if (c === "-" || c === "*" || c === "+") return line.length >= 2 && (line[1] === " " || line[1] === "\t");
  let i = 0;
  while (i < line.length && line[i] >= "0" && line[i] <= "9") i++;
  if (i === 0 || i > 9 || i >= line.length) return false;
  if (line[i] !== "." && line[i] !== ")") return false;
  if (i + 1 >= line.length) return false;
  return line[i + 1] === " " || line[i + 1] === "\t";
}

// markdown_boundary.go:497 isSetextUnderlineCandidate.
export function isSetextUnderlineCandidate(line) {
  let i = 0;
  while (i < line.length && (line[i] === " " || line[i] === "\t")) i++;
  if (i === line.length) return false;
  const c = line[i];
  if (c !== "=" && c !== "-") return false;
  let j = i;
  while (j < line.length && line[j] === c) j++;
  for (; j < line.length; j++) if (line[j] !== " " && line[j] !== "\t") return false;
  return true;
}

// markdown_boundary.go:531 isHTMLBlockOpener.
export function isHTMLBlockOpener(line) {
  let i = 0;
  while (i < line.length && i < 3 && line[i] === " ") i++;
  const rest = line.slice(i);
  if (rest.length < 2 || rest[0] !== "<") return false;
  if (rest.startsWith("<!--") || rest.startsWith("<?") || rest.startsWith("<![CDATA[")) return true;
  if (rest.length >= 3 && rest[1] === "!" && /[A-Za-z]/.test(rest[2])) return true;
  const low = rest.toLowerCase();
  for (const t of ["<script", "<pre", "<style", "<textarea"]) {
    if (low.startsWith(t)) {
      const next = low.length > t.length ? low[t.length] : "";
      if (next === "" || next === " " || next === "\t" || next === ">") return true;
    }
  }
  let j = 1;
  if (j < rest.length && rest[j] === "/") j++;
  return j < rest.length && /[A-Za-z]/.test(rest[j]);
}

// markdown_boundary.go:613 isLinkRefDefinition.
export function isLinkRefDefinition(line) {
  let i = 0;
  while (i < line.length && i < 3 && line[i] === " ") i++;
  if (i >= line.length || line[i] !== "[") return false;
  i++;
  const labelStart = i;
  while (i < line.length && line[i] !== "]") i++;
  if (i >= line.length || i === labelStart) return false;
  i++;
  if (i >= line.length || line[i] !== ":") return false;
  i++;
  while (i < line.length && (line[i] === " " || line[i] === "\t")) i++;
  return i < line.length;
}
