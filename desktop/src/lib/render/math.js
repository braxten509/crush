// Math (CHAT-014; desktop-design 1.7 item 5). KaTeX renders to a string with
// trust off, so \href, \url and \includegraphics stay text; the string goes
// through DOMPurify's HTML profile (MathML kept) before it becomes nodes.
//
//   renderMath(tex, {display})   a fragment, or null when KaTeX gave nothing
//   mathExtensions               marked extensions: display math blocks
//                                ($$ … $$ and \[ … \], no blank line inside) and
//                                inline math ($ … $ with the currency-safe rule,
//                                $$ … $$, \( … \))
//   mathTitle(tex)               a figure's title: the first 24 characters
import katex from "katex";
import "katex/dist/katex.min.css";
import { mathFragment } from "./sanitize.js";

export const KATEX_OPTIONS = {
  trust: false,
  throwOnError: false,
  strict: "ignore",
  maxExpand: 1000,
  output: "htmlAndMathml",
  errorColor: "#a1261b", // --error-ink
};

export function renderMath(tex, { display = false, doc = document } = {}) {
  let html;
  try {
    html = katex.renderToString(String(tex), { ...KATEX_OPTIONS, displayMode: display });
  } catch {
    return null;
  }
  const fragment = mathFragment(html);
  if (!fragment || !fragment.firstChild) return null;
  return fragment.ownerDocument === doc ? fragment : doc.importNode(fragment, true);
}

export function mathTitle(tex) {
  const text = String(tex).replace(/\s+/g, " ").trim();
  return text.length > 24 ? `${text.slice(0, 24)}…` : text;
}

// A display block: the opener at a line's start (up to three spaces in), the
// closer at the end of a line, and no blank line between them.
const BLOCK = /^ {0,3}(\$\$|\\\[)((?:(?!\n[ \t]*\n)[\s\S])*?)(\$\$|\\\])[ \t]*(?:\n|$)/;
const CLOSER = { $$: "$$", "\\[": "\\]" };

function matchBlock(src) {
  const m = BLOCK.exec(src);
  // The closer must match the opener, and "$$$$" holds no math. ("$$ a $$ b"
  // on one line fails the end-of-line rule: inline math in a paragraph.)
  if (!m || CLOSER[m[1]] !== m[3] || m[2].trim() === "") return null;
  return m;
}

// Inline: $$…$$ and \(…\) anywhere; $…$ only when the opener has a non-space
// after it, the closer a non-space before it and no digit after it (so
// "$5 and $10" stays text), and no blank line inside.
const INLINE_DISPLAY = /^\$\$((?:(?!\n[ \t]*\n)[^$])+?)\$\$/;
const INLINE_PAREN = /^\\\(((?:(?!\n[ \t]*\n)[\s\S])+?)\\\)/;

function matchDollar(src) {
  if (src[0] !== "$" || src[1] === "$" || src.length < 3 || /\s/.test(src[1])) return null;
  for (let i = 1; i < src.length; i++) {
    const ch = src[i];
    if (ch === "\\") {
      i++;
      continue;
    }
    if (ch === "\n" && /^\n[ \t]*\n/.test(src.slice(i))) return null;
    if (ch === "$") {
      if (/\s/.test(src[i - 1]) || /[0-9]/.test(src[i + 1] ?? "")) continue;
      return { raw: src.slice(0, i + 1), text: src.slice(1, i) };
    }
  }
  return null;
}

export const mathExtensions = [
  {
    name: "mathBlock",
    level: "block",
    // Only where a block really starts, so a paragraph is never cut at a "$$"
    // that turns out not to open one.
    start(src) {
      for (let at = 0; at < src.length; ) {
        if (matchBlock(src.slice(at))) return at;
        const nl = src.indexOf("\n", at);
        if (nl < 0) return undefined;
        at = nl + 1;
      }
      return undefined;
    },
    tokenizer(src) {
      const m = matchBlock(src);
      if (!m) return undefined;
      return { type: "mathBlock", raw: m[0], text: m[2].trim(), open: m[1] };
    },
  },
  {
    name: "mathInline",
    level: "inline",
    start(src) {
      const m = /\$|\\\(/.exec(src);
      return m ? m.index : undefined;
    },
    tokenizer(src) {
      let m = INLINE_DISPLAY.exec(src);
      if (m) return { type: "mathInline", raw: m[0], text: m[1].trim(), display: true };
      m = INLINE_PAREN.exec(src);
      if (m) return { type: "mathInline", raw: m[0], text: m[1].trim(), display: false };
      const dollar = matchDollar(src);
      if (dollar) return { type: "mathInline", raw: dollar.raw, text: dollar.text, display: false };
      return undefined;
    },
  },
];
