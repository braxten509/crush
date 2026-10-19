// Branch rules (desktop-design 3.1, 3.2; PLAN.md:99-101). Pure functions the
// right-click menu's "Branch from here" uses: the new chat's title and its
// first draft, the cut a selection names, and the reasons a selection cannot
// branch.
//
//   branchTitle(text)        whitespace collapsed (demo core.js:259); over 34
//                            characters it is cut to 33 and "…" (core.js:262)
//   quoteDraft(text)         the branch's typing box: "> " + the highlighted
//                            words in full on one line + "\n\n", never cut
//                            (PLAN.md:101, :313; core.js:270)
//   quoteInReply(text)       "Quote in reply": "> " before every line (D14);
//                            the same rule as lib/copy.js quoteLines
//   selectionToCut({messageId, range}, sourceMap)
//                            → {message_id, start, end, text} | {error}: the
//                            selected characters as a range of the stored text
//                            (M.Content().Text), in UTF-16 code units, the
//                            engine's unit (engine-design.md:2151, 2177)
//   cutOfRange(range, items) the same for the timeline's selection, which may
//                            touch two items, a tool row or nothing
//   overlaps(links, start, end)
//                            half-open [s, e): touching ranges are fine
//                            (engine 5.11 step 4)
//   canBranch(message, cut, links, {answered})
//                            → {ok: true} | {reason}, the reasons of 3.1
//   HINTS, branchErrorText   the menu's hint per reason, the notice per error
import { quoteLines } from "./copy.js";
import { stripPlanMarkers } from "./render/plan.js";
import { sourceMapOf } from "./render/sourcemap.js";
import { selectionEnds } from "./selection.js";

export const TITLE_MAX = 34;

// The hint under a disabled "Branch from here" (4.2). "Wait for the reply to
// finish" is the design's; the others are desktop texts: the demo's
// "highlight text first" (core.js:313), the engine's own refusal of tool
// output (message/branch.go, ErrBadCut) and the overlap notice of 3.2.
export const HINTS = {
  unfinished: "Wait for the reply to finish",
  tool_output: "Branch from message text, not tool output",
  overlap: "These words already open a branch",
  empty: "Highlight text first",
  two_messages: "Highlight text in one message",
};

// 3.2 step 3: the notice for a refused branch; the selection stays.
const ERROR_TEXTS = {
  cut_not_finished: "This reply is still being written.",
  highlight_overlaps_link: "These words already open a branch.",
  transport: "Branch not made: lost the connection to Crush.",
  engine_unavailable: "Branch not made: lost the connection to Crush.",
};

// bad_cut and every code without a text of its own show the error's message
// (1.2.2: a new code is never dropped silently).
export function branchErrorText(error) {
  const code = error && typeof error === "object" ? error.code : "";
  if (code && ERROR_TEXTS[code]) return ERROR_TEXTS[code];
  if (error && typeof error === "object" && typeof error.message === "string") return error.message;
  return String(error);
}

// Runs of white space become one space; the ends are trimmed. Plan markers
// (never shown) stay out of the words.
function oneLine(text) {
  return stripPlanMarkers(String(text ?? ""))
    .replace(/\s+/g, " ")
    .trim();
}

// Characters as people count them: a whole emoji (pairs, joined sequences)
// is one, so a cut never splits one.
function characters(text) {
  if (typeof Intl !== "undefined" && typeof Intl.Segmenter === "function") {
    return [...new Intl.Segmenter(undefined, { granularity: "grapheme" }).segment(text)].map((s) => s.segment);
  }
  return Array.from(text);
}

export function branchTitle(text) {
  const title = oneLine(text);
  const chars = characters(title);
  return chars.length > TITLE_MAX ? `${chars.slice(0, TITLE_MAX - 1).join("")}…` : title;
}

export function quoteDraft(text) {
  return `> ${oneLine(text)}\n\n`;
}

export function quoteInReply(text) {
  return quoteLines(text);
}

const SPACE = /\s/;

// The selection inside one message's drawn text (range) through that text's
// source map: the stored characters it shows, whole inline marks included
// when their whole content is selected (CHAT-088's rule), white space at
// either end left out so the link starts and ends on words.
export function selectionToCut(selection, sourceMap) {
  const messageId = selection?.messageId ?? "";
  const range = selection?.range ?? null;
  if (!messageId || !sourceMap || !range || range.collapsed) return { error: "empty" };
  const hit = sourceMap.rangeToSource(range, { wholeSpans: true });
  if (!hit) return { error: "empty" };
  const source = String(sourceMap.source ?? "");
  let { start, end } = hit;
  while (start < end && SPACE.test(source[start])) start += 1;
  while (end > start && SPACE.test(source[end - 1])) end -= 1;
  if (end <= start) return { error: "empty" };
  return { message_id: messageId, start, end, text: source.slice(start, end) };
}

// The item's frame (the element with its key; the article when there are
// several) and the text inside it that has a source map.
function frameFor(doc, key) {
  let first = null;
  for (const el of doc.querySelectorAll("[data-key]")) {
    if (el.dataset.key !== key) continue;
    if (el.getAttribute("role") === "article") return el;
    first ??= el;
  }
  return first;
}

function textRootIn(frame) {
  if (!frame) return null;
  for (const el of frame.querySelectorAll(".md, .text")) if (sourceMapOf(el)) return el;
  return null;
}

// True when the part of range inside frame shows no text: a triple click's
// range ends at the next item's very start, which selects nothing there.
function nothingIn(frame, range, side) {
  if (!frame) return false;
  const part = frame.ownerDocument.createRange();
  if (side === "end") {
    part.setStart(frame, 0);
    part.setEnd(range.endContainer, range.endOffset);
  } else {
    part.setStart(range.startContainer, range.startOffset);
    part.setEnd(frame, frame.childNodes.length);
  }
  return part.toString().trim() === "";
}

const MESSAGE_TEXT = new Set(["user", "reply"]);

// The timeline's selection → a cut, or why it is none: {error: "empty"} (no
// words), "two_messages", "tool_output" (a tool row, a shell, a notice, a
// footer: not a message's own text), "unfinished" (a prompt still on its way).
export function cutOfRange(range, items = []) {
  if (!range || range.collapsed) return { error: "empty" };
  const doc = range.startContainer.ownerDocument;
  let { startKey, endKey } = selectionEnds(range);
  if (!startKey || !endKey) return { error: "empty" };
  if (startKey !== endKey) {
    if (nothingIn(frameFor(doc, endKey), range, "end")) endKey = startKey;
    else if (nothingIn(frameFor(doc, startKey), range, "start")) startKey = endKey;
    else return { error: "two_messages" };
  }
  const item = items.find((it) => it.key === startKey) ?? null;
  if (!item) return { error: "empty" };
  if (item.kind === "pending") return { error: "unfinished" };
  if (!MESSAGE_TEXT.has(item.kind)) return { error: "tool_output" };
  const root = textRootIn(frameFor(doc, startKey));
  const map = root ? sourceMapOf(root) : null;
  if (!map) return { error: "empty", message_id: item.messageId };
  return selectionToCut({ messageId: item.messageId, range }, map);
}

export function overlaps(links, start, end) {
  return (links ?? []).some((link) => link.highlight_start < end && start < link.highlight_end);
}

const partsOf = (m, type) => (m?.parts ?? []).filter((p) => p?.type === type);

// A reply can be cut once it has its finish part and every tool it called has
// its result (engine 5.11 steps 3 and 6). answered(id) says whether a tool
// call has a result; without it the calls count as answered.
function settled(message, answered) {
  if (message.role !== "assistant") return true;
  if (partsOf(message, "finish").length === 0) return false;
  if (!answered) return true;
  const has = typeof answered === "function" ? answered : (id) => answered.has(id);
  return partsOf(message, "tool_call").every((p) => !p.data?.id || has(p.data.id));
}

// message: the stored message the cut is in (or the clicked one when nothing
// is selected); links: the links already on it. The order picks the hint that
// helps first: where the words are, then the message's state, then the words.
export function canBranch(message, cut, links = [], { answered = null } = {}) {
  if (cut?.error === "two_messages") return { reason: "two_messages" };
  if (cut?.error === "tool_output") return { reason: "tool_output" };
  if (message && message.role !== "user" && message.role !== "assistant") return { reason: "tool_output" };
  if (cut?.error === "unfinished" || (message && !settled(message, answered))) return { reason: "unfinished" };
  if (!message || !cut || cut.error || !(cut.end > cut.start) || String(cut.text ?? "").trim() === "") return { reason: "empty" };
  const own = (links ?? []).filter((link) => link.source_message_id === message.id);
  if (overlaps(own, cut.start, cut.end)) return { reason: "overlap" };
  return { ok: true };
}
