// Colors and side-scrolling for diff code (CHAT-110, CHAT-112). The terminal
// highlights each diff line on its own, with the language of the file's name
// or, without one, the language its lexer finds in the old text
// (diffview/syntax.go); the desktop guesses only from at most 20 KB. The
// colored nodes come from render/code.js, which cleans them (1.7 item 7).
import hljs from "highlight.js/lib/core";
import { charCells, escapeControl } from "../diff.js";
import { highlight, languageForPath } from "../render/code.js";

export const GUESS_LIMIT = 20 * 1024;

// The language to color a file's diff with; "plaintext" for none.
export function diffLanguage(path, sample = "") {
  const byName = languageForPath(path);
  if (byName) return byName;
  const text = String(sample ?? "");
  if (text.trim() === "" || text.length > GUESS_LIMIT) return "plaintext";
  try {
    const guess = hljs.highlightAuto(text);
    return guess.relevance >= 5 && guess.language ? guess.language : "plaintext";
  } catch {
    return "plaintext";
  }
}

// One line of code as colored nodes: control characters drawn as their
// pictures first, so none reaches the page raw.
export function codeLine(text, language, doc = document) {
  return highlight(escapeControl(String(text ?? "")), language || "plaintext", doc);
}

// Drops the first n cells of a fragment's text (render.go lineContent's
// ansi.GraphemeWidth.Cut on the colored line), keeping the colors of what
// stays; marks that join the cut character go with it.
export function cutCells(fragment, n) {
  if (!(n > 0)) return fragment;
  let left = n;
  let trailing = false;
  const walker = (fragment.ownerDocument ?? document).createTreeWalker(fragment, 4 /* NodeFilter.SHOW_TEXT */);
  const texts = [];
  for (let node = walker.nextNode(); node; node = walker.nextNode()) texts.push(node);
  for (const node of texts) {
    const chars = [...node.data];
    let i = 0;
    while (i < chars.length && left > 0) left -= charCells(chars[i++]);
    if (left <= 0) {
      if (!trailing) {
        while (i < chars.length && charCells(chars[i]) === 0) i++;
        trailing = i < chars.length;
      }
    }
    node.data = chars.slice(i).join("");
    if (trailing) break;
  }
  return fragment;
}
