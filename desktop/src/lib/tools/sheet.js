// The Changes sheet's rules (DLG-294 to DLG-311). Ports of dialog/changes.go
// (folding, file list, headers, code text, paths, footer, keys) and
// model/reviews.go:22-73 (loading the saved reviews first).
import { cells } from "../diff.js";
import { prettyPath } from "./go.js";
import { ADDED, anyCounted, buildReviewChanges, cleanPath, counted, DELETED, hasEdits, kindName, stats } from "./review.js";

// A file with more diff lines than this starts folded.
export const FOLD_OVER = 600;
export const startsFolded = (file) => file.lines.length > FOLD_OVER;

export const TAB_STOP = 4;

// The expired text (model/reviews.go:67).
export const EXPIRED_TEXT = "This saved diff has expired. Full diffs are kept for the latest five prompts; the chat is still here.";

// countNoun: "1 file", "3 files".
export const countNoun = (n, noun) => (n === 1 ? `1 ${noun}` : `${n} ${noun}s`);

// The heading's source line: "Ran 4 tools · 3 files" and the totals when
// any file has counts.
export function sourceLine(title, files) {
  const line = { text: `${title} · ${countNoun(files.length, "file")}`, adds: null, dels: null };
  if (anyCounted(files)) Object.assign(line, stats(files));
  return line;
}

// The color of a file's kind square: added, removed, or other.
export function kindTone(kind) {
  if (kind === ADDED) return "add";
  if (kind === DELETED) return "del";
  return "other";
}

// A file list row's right side: its counts, or its kind when it has no
// authored change or no counts.
export function listStat(file) {
  if (!hasEdits(file) || !counted(file)) return { label: kindName(file.kind) };
  return { adds: file.adds, dels: file.dels };
}

// A file header: the badge (the kind) and the counts when counted.
export function fileHead(file) {
  return { badge: kindName(file.kind), tone: kindTone(file.kind), counts: counted(file) ? { adds: file.adds, dels: file.dels } : null };
}

// The digits of a file's number columns: at least 3.
export function numberDigits(file) {
  let max = 0;
  for (const l of file.lines) max = Math.max(max, l.old, l.new);
  return Math.max(3, String(max).length);
}

// wrapCode's text: tabs to the next 4-column stop, control characters dropped.
export function codeText(text) {
  let out = "";
  let col = 0;
  for (const ch of String(text ?? "")) {
    const code = ch.codePointAt(0);
    if (ch === "\t") {
      const n = TAB_STOP - (col % TAB_STOP);
      out += " ".repeat(n);
      col += n;
    } else if (code < 0x20 || code === 0x7f) {
      // dropped
    } else {
      out += ch;
      col++;
    }
  }
  return out;
}

// shortPath: relative to the project folder when inside it, else as given.
export function shortPath(path, cwd) {
  const target = cleanPath(path);
  const base = cleanPath(cwd || ".");
  let rel = null;
  if (base === "." && !target.startsWith("/")) rel = target;
  else if (target === base) rel = ".";
  else if (base === "/" && target.startsWith("/")) rel = target.slice(1);
  else if (target.startsWith(base + "/")) rel = target.slice(base.length + 1);
  if (rel !== null && !rel.startsWith("..")) return rel;
  return prettyPath(path);
}

// truncateLeft: the end of a path (the file name) kept when it is too wide.
export function truncateLeft(text, width) {
  if (width <= 1) return "";
  if (cells(text) <= width) return text;
  const chars = [...text];
  let drop = cells(text) - width + 1;
  let i = 0;
  while (i < chars.length && drop > 0) drop -= cells(chars[i++]);
  return "…" + chars.slice(i).join("");
}

// The footer's key hints, shorter as the sheet narrows (width in characters).
export const FOOTER_KEYS = [
  "wheel/↑↓ scroll · click a file to jump · click its header to fold · n/p next/prev file · esc close",
  "↑↓/wheel scroll · click file: jump · click header: fold · n/p file · esc close",
  "↑↓ scroll · n/p file · esc close",
];
export function footerKeys(width) {
  for (const keys of FOOTER_KEYS.slice(0, 2)) if (cells(keys) <= width - 4) return keys;
  return FOOTER_KEYS[2];
}

// Keys (changes.go:81-89): each maps to an action.
export const SHEET_KEYS = {
  Escape: "close",
  "Alt+Escape": "close",
  q: "close",
  ArrowUp: "up",
  k: "up",
  ArrowDown: "down",
  j: "down",
  PageUp: "pageUp",
  b: "pageUp",
  "Shift+Space": "pageUp",
  PageDown: "pageDown",
  Space: "pageDown",
  f: "pageDown",
  Home: "top",
  g: "top",
  End: "bottom",
  "Shift+G": "bottom",
  n: "next",
  "]": "next",
  Tab: "next",
  p: "prev",
  "[": "prev",
  "Shift+Tab": "prev",
};

// jumpFile: the scroll position of the next or previous file's box, given
// the top of each file box and the current scroll position; no wrap: past
// the last file nothing moves (null), before the first the sheet goes to 0.
export function jumpTarget(heads, top, dir) {
  if (dir > 0) {
    for (const h of heads) if (h > top + 1) return h;
    return null;
  }
  for (let i = heads.length - 1; i >= 0; i--) if (heads[i] < top - 1) return heads[i];
  return 0;
}

// -- Loading (model/reviews.go openReview and applyReview) --

// Whether an engine error means the saved diff is gone (expired, or the
// message no longer exists).
export function isExpired(error) {
  const text = String(error?.message ?? error ?? "");
  return text.includes("no longer saved") || error?.status === 404 || error?.code === "not_found";
}

// The calls of the sheet's tool call ids, in that order, with their results,
// read from a chat's stored messages ({ids, byId}).
export function callsFromChat(chat, toolCallIds) {
  const calls = new Map();
  const results = new Map();
  for (const id of chat?.ids ?? []) {
    const m = chat.byId.get(id);
    for (const part of m?.parts ?? []) {
      if (part?.type === "tool_call") calls.set(part.data.id, { ...part.data, messageId: id });
      else if (part?.type === "tool_result") results.set(part.data.tool_call_id, part.data);
    }
  }
  const out = [];
  for (const id of toolCallIds ?? []) {
    const call = calls.get(id);
    if (call) out.push({ call: { id, name: call.name, input: call.input }, result: results.get(id) ?? null });
  }
  return out;
}

// The files of the sheet: each saved summary is replaced by its full review
// (loaded once per review id, from the review's own chat), then the changes
// are built. loadReview(sessionId, messageId) resolves to a Message.
export async function loadSheetFiles(sessionId, inputs, loadReview) {
  const loaded = new Map();
  const full = [];
  for (const input of inputs) {
    if (!input.result) continue;
    let result = input.result;
    const review = result.review;
    if (review && review.summary) {
      let message = loaded.get(review.id);
      if (!message) {
        message = await loadReview(review.session_id || sessionId, review.id);
        loaded.set(review.id, message);
      }
      for (const part of message?.parts ?? []) {
        if (part?.type === "tool_result" && part.data.tool_call_id === (result.tool_call_id ?? input.call.id)) {
          result = part.data;
          break;
        }
      }
    }
    full.push({ call: input.call, result });
  }
  return buildReviewChanges(full);
}
