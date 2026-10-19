// Copying from the timeline (desktop-design 1.6 item 8; CHAT-012, CHAT-030,
// CHAT-087 to CHAT-091, CHAT-100). Every copy goes through clipboard_write
// (1.2.8): success shows the row's notice, a failure the command's own
// message ("Failed to copy to clipboard", AppError clipboard_failed), as a
// warning like the terminal's (internal/ui/common/common.go:251-268).
//
//   copyBlocks(stored) / copyVisible(stored)
//                            a reply's visible text (c/y and "Copy whole
//                            message", CHAT-030): no Markdown marks, code
//                            verbatim, plan markers and HTML left out, links
//                            copy their label; a port of markdownCopyBlocks
//                            (internal/ui/chat/copy_source.go:29-85)
//   itemCopy(item, message)  {text, notice} for c/y on a selected item, or null
//   registerItemCopy(kind, fn)
//                            tool, tool group and shell items copy in their own
//                            formats (CHAT-044, CHAT-056: lib/tools/copy.js, P3.6)
//   copySelection(range, items)
//                            the selected text as Markdown source (CHAT-088):
//                            each end item through its source map, the items
//                            between whole from their stored text, never read
//                            from the page, joined with a blank line (1.6 item 8)
//   writeClipboard(text, notice)
import { clipboard_write } from "./engine/commands.js";
import { lexMarkdown } from "./render/lex.js";
import { stripPlanMarkers } from "./render/plan.js";
import { decodeEntities } from "./render/sanitize.js";
import { sourceMapOf } from "./render/sourcemap.js";
import { trimSpace } from "./render/strings.js";
import { notify } from "./stores/ui.svelte.js";
import { noticeParts, ranTools } from "./timeline.js";
import { keyAtPoint } from "./selection.js";

export const COPIED_MESSAGE = "Message copied to clipboard";
export const COPIED_SELECTION = "Selected text copied to clipboard";
export const COPY_FAILED = "Failed to copy to clipboard";

// -- The clipboard (CHAT-090) --

export async function writeClipboard(text, notice) {
  try {
    await clipboard_write(text);
  } catch (error) {
    notify("warn", (error && typeof error === "object" && error.message) || COPY_FAILED);
    return false;
  }
  if (notice) notify("info", notice);
  return true;
}

// -- A reply's visible text (CHAT-030) --

export function copyBlocks(stored) {
  const blocks = [];
  walkBlocks(lexMarkdown(stripPlanMarkers(String(stored ?? ""))), blocks);
  return blocks;
}

export function copyVisible(stored) {
  return copyBlocks(stored).join("\n\n");
}

// The block walk (copy_source.go:33-49): code verbatim, HTML blocks skipped,
// every block holding inline text becomes one block.
function walkBlocks(tokens, out) {
  for (const t of tokens ?? []) {
    switch (t.type) {
      case "code":
        out.push(codeLines(t));
        break;
      case "html":
      case "space":
      case "hr":
      case "def":
        break;
      case "paragraph":
      case "heading":
      case "text":
        if (t.tokens?.length) out.push(inlineText(t.tokens));
        else if (t.type === "text" && t.text) out.push(softBreaks(decodeEntities(t.text)));
        break;
      case "blockquote":
        walkBlocks(t.tokens, out);
        break;
      case "list":
        for (const item of t.items ?? []) walkBlocks(item.tokens, out);
        break;
      case "table":
        for (const cell of t.header ?? []) if (cell.tokens?.length) out.push(inlineText(cell.tokens));
        for (const row of t.rows ?? []) for (const cell of row) if (cell.tokens?.length) out.push(inlineText(cell.tokens));
        break;
      case "mathBlock":
        // The terminal reads display math as a paragraph of its own text.
        out.push(softBreaks(t.raw.trim()));
        break;
      default:
        if (t.tokens) walkBlocks(t.tokens, out);
    }
  }
}

// node.Lines().Value: every line of the code with its newline.
function codeLines(t) {
  const text = t.text ?? "";
  return text === "" ? "" : `${text}\n`;
}

// A soft line break copies as one space; a hard one as a newline
// (copy_source.go:67-71).
function softBreaks(s) {
  return s.replace(/[ \t]*\n[ \t]*/g, " ");
}

// copyInlineSource (copy_source.go:53-85): text with escapes and entities
// read, raw HTML left out, code spans without their ticks, links and pictures
// by their words.
function inlineText(tokens) {
  let out = "";
  for (const t of tokens ?? []) {
    switch (t.type) {
      case "html":
      case "checkbox":
        break;
      case "text":
        out += t.tokens?.length ? inlineText(t.tokens) : softBreaks(decodeEntities(t.text ?? ""));
        break;
      case "escape":
      case "codespan":
        out += t.text ?? "";
        break;
      case "br":
        out += "\n";
        break;
      case "image":
        out += decodeEntities(t.text ?? "");
        break;
      case "mathInline":
        out += t.raw ?? "";
        break;
      default:
        out += t.tokens?.length ? inlineText(t.tokens) : (t.text ?? "");
    }
  }
  return out;
}

// -- c/y on a selected item (CHAT-012, CHAT-030, CHAT-100) --

const copiers = new Map(); // kind → (item) → {text, notice} | null

export function registerItemCopy(kind, fn) {
  copiers.set(kind, fn);
}

// What c/y copies for the item: a message's text as sent (user.go:291-296,
// also for notices, which are user messages there), a reply's visible text
// (assistant.go:763-768), a tool or shell in its registered format. message
// is the stored message, when the item has one.
export function itemCopy(item, message = null) {
  if (!item) return null;
  switch (item.kind) {
    case "user":
    case "pending":
      return { text: item.text ?? "", notice: COPIED_MESSAGE };
    case "notice": {
      const stored = (message?.parts ?? []).find((p) => p?.type === "text")?.data?.text;
      return { text: stored ?? noticeLine(item), notice: COPIED_MESSAGE };
    }
    case "reply":
      return { text: copyVisible(item.text), notice: COPIED_MESSAGE };
    default: {
      const fn = copiers.get(item.kind);
      return fn ? (fn(item) ?? null) : null;
    }
  }
}

function noticeLine(item) {
  const p = noticeParts(item);
  return `${p.before}${p.strong}${p.after}`;
}

// -- A selection (CHAT-087, CHAT-088) --

// An item's whole text when a selection passes over it: the source of a
// message (user text, a reply's Markdown without plan markers), else the line
// it shows.
export function wholeText(item) {
  switch (item.kind) {
    case "user":
    case "pending":
      return trimSpace(item.text ?? "");
    case "reply":
      return stripPlanMarkers(item.text ?? "");
    case "notice":
      return noticeLine(item);
    case "banner":
      return item.details ? `${bannerTag(item)}${item.title}\n${item.details}` : `${bannerTag(item)}${item.title}`;
    case "footer":
      return item.saved ? `${item.text} ${item.saved}` : item.text;
    case "status":
      return item.words;
    case "tools":
      return ranTools(item.calls?.length ?? 0);
    case "shell":
      return item.output ? `$ ${item.command}\n${item.output}` : `$ ${item.command}`;
    default: {
      const fn = copiers.get(item.kind);
      return fn ? (fn(item)?.text ?? "") : "";
    }
  }
}

function bannerTag(item) {
  if (item.banner === "error") return "ERROR ";
  if (item.banner === "refused") return "REFUSED ";
  return "";
}

// The text-holding root of an item's frame: the element with a source map.
function sourceRootOf(frame) {
  if (!frame) return null;
  for (const el of frame.querySelectorAll(".md, .text")) if (sourceMapOf(el)) return el;
  return null;
}

// The part of one item a selection covers. side: "start" when the range
// starts in it (it runs to the item's end), "end" when it ends in it, "both".
function partOf(item, frame, range, side) {
  const doc = frame.ownerDocument;
  const clip = doc.createRange();
  if (side === "start" || side === "both") clip.setStart(range.startContainer, range.startOffset);
  else clip.setStart(frame, 0);
  if (side === "end" || side === "both") clip.setEnd(range.endContainer, range.endOffset);
  else clip.setEnd(frame, frame.childNodes.length);
  const root = sourceRootOf(frame);
  const map = root ? sourceMapOf(root) : null;
  if (map) {
    const hit = map.rangeToSource(clip, { wholeSpans: true });
    if (!hit) return "";
    return stripPlanMarkers(map.source.slice(hit.start, hit.end));
  }
  return clip.toString();
}

// The frame (the element carrying data-key) of an item, inside root.
function frameIn(root, key) {
  for (const el of root.querySelectorAll("[data-key]")) {
    if (el.dataset.key === key && el.getAttribute("role") === "article") return el;
  }
  for (const el of root.querySelectorAll("[data-key]")) if (el.dataset.key === key) return el;
  return null;
}

// The selected text, item by item: the two ends from the page through their
// source maps, everything between from the items themselves.
export function copySelection(range, items, root = range?.commonAncestorContainer?.ownerDocument?.body) {
  if (!range || range.collapsed) return "";
  const startKey = keyAtPoint(range.startContainer, range.startOffset, "start");
  const endKey = keyAtPoint(range.endContainer, range.endOffset, "end");
  if (!startKey || !endKey) return range.toString();
  const keys = items.map((item) => item.key);
  const a = keys.indexOf(startKey);
  const b = keys.indexOf(endKey);
  if (a < 0 || b < 0 || b < a) return "";
  const scope = root ?? range.startContainer.ownerDocument.body;
  const parts = [];
  for (let i = a; i <= b; i++) {
    const item = items[i];
    let text;
    if (i === a || i === b) {
      const frame = frameIn(scope, item.key);
      text = frame ? partOf(item, frame, range, a === b ? "both" : i === a ? "start" : "end") : wholeText(item);
    } else text = wholeText(item);
    if (text !== "") parts.push(text);
  }
  return parts.join("\n\n");
}
