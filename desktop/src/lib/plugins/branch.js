// Branching (desktop-design 3.1 to 3.3, 4.2, 4.8; PLAN.md:99-104): the
// right-click menu's "Branch from here" (order 10), the branch links drawn
// over a message's words and what a click on one does, the list's
// `chat.close` action, and the probe's `branch` section.
//
// Branch from here: the timeline's selection becomes a cut of one message's
// stored text (lib/branch.js); a cut that cannot branch disables the entry
// with its reason's hint. The new chat opens at once with the words quoted in
// its typing box, the caret at the end; a refused branch shows a notice and
// leaves the selection as it was.
import { tick } from "svelte";
import { registerAction } from "../actions.js";
import { HINTS, branchErrorText, branchTitle, canBranch, cutOfRange, quoteDraft } from "../branch.js";
import { registerDecorationHandler, registerDecorationSource } from "../decorations.js";
import { api } from "../engine/api.js";
import { registerMenuItem } from "../menu.js";
import { probeSection } from "../probe.js";
import { sourceMapOf } from "../render/sourcemap.js";
import { closeChat, closed, isClosed, reopen } from "../stores/closed.svelte.js";
import { caretToEdge, focusField, setText } from "../stores/composer.svelte.js";
import { links, linksFor, parentOf, upsertLink } from "../stores/links.svelte.js";
import { messages } from "../stores/messages.svelte.js";
import { openChat, sessions, upsertSession } from "../stores/sessions.svelte.js";
import { notify } from "../stores/ui.svelte.js";

// -- Branch from here --

// Every tool call a tool message of the chat answered.
function answeredIn(chat) {
  const answered = new Set();
  for (const m of chat?.byId.values() ?? []) {
    for (const part of m?.parts ?? []) if (part?.type === "tool_result" && part.data?.tool_call_id) answered.add(part.data.tool_call_id);
  }
  return answered;
}

// The menu's context → {ok, cut, message} or {reason}. Without a selection
// the clicked message speaks: a reply still being written says so, anything
// else asks for words.
function verdictOf(ctx) {
  const chat = messages.bySession[ctx?.sessionId ?? ""];
  const cut = ctx?.range ? cutOfRange(ctx.range, ctx.items ?? []) : { error: "empty" };
  const mid = cut.message_id ?? (cut.error === "empty" ? (ctx?.message?.id ?? "") : "");
  const message = mid ? (chat?.byId.get(mid) ?? null) : null;
  const verdict = canBranch(message, cut, linksOnMessage(mid), { answered: answeredIn(chat) });
  return verdict.ok ? { ok: true, cut, message } : verdict;
}

// Every link on a message (not only the ones shown): the overlap rule counts them all.
const linksOnMessage = (mid) => (mid ? (links.byParentMessage[mid] ?? []) : []);

let making = false;

async function branchFrom(ctx) {
  const verdict = verdictOf(ctx);
  if (!verdict.ok || making) return;
  const { cut } = verdict;
  making = true;
  let branch;
  try {
    branch = await api.createBranch(ctx.sessionId, {
      message_id: cut.message_id,
      highlight_text: cut.text,
      highlight_start: cut.start,
      highlight_end: cut.end,
      title: branchTitle(cut.text),
    });
  } catch (error) {
    notify("error", branchErrorText(error));
    return;
  } finally {
    making = false;
  }
  const sid = branch?.session?.id;
  if (!sid) return;
  upsertSession(branch.session);
  if (branch.link) upsertLink(branch.link);
  globalThis.getSelection?.()?.removeAllRanges();
  if (!(await openChat(sid))) return;
  setText(quoteDraft(cut.text));
  await tick();
  focusField();
  caretToEdge("end");
}

registerMenuItem({
  id: "branch",
  label: "Branch from here",
  order: 10,
  enabled: (ctx) => verdictOf(ctx).ok === true,
  hint: (ctx) => HINTS[verdictOf(ctx).reason] ?? "",
  run: (ctx) => {
    branchFrom(ctx).catch((error) => console.error("branch failed", error));
  },
});

// -- Links (3.3, 4.8) --

registerDecorationSource((messageId) => linksFor(messageId));

// A closed branch is listed again first (demo core.js:52-60).
export async function openBranch(sid) {
  if (!sid) return false;
  if (isClosed(sid) && !(await reopen(sid))) return false;
  return openChat(sid);
}

registerDecorationHandler("branch-link", (decoration) => {
  openBranch(decoration?.data?.session_id).catch((error) => console.error("opening a branch failed", error));
});

// The link's decoration and the branch it opens, from one of its drawn spans.
function decorationOf(span) {
  const root = span?.closest?.(".md, .text");
  const map = root ? sourceMapOf(root) : null;
  return map?.decorations?.[Number(span.dataset.deco)] ?? null;
}

const branchName = (sid) => sessions.byId[sid]?.title || "branch";

export function linkTitle(sid) {
  return `${isClosed(sid) ? "Reopen" : "Open"} ${branchName(sid)}`;
}

// The title a link shows on hover (4.8), set as the pointer reaches it.
if (typeof document !== "undefined") {
  document.addEventListener("mouseover", (event) => {
    const span = event.target?.closest?.(".md-deco-branch-link");
    const sid = span ? decorationOf(span)?.data?.session_id : "";
    if (sid) span.title = linkTitle(sid);
  });
}

// -- Closing (4.8): the action phase 5's palette lists; its key is the 5-6 design's --

// The focused list row's chat, else the open chat (the selected row).
function closeTarget() {
  const row = globalThis.document?.activeElement?.closest?.("nav.chats .chat[data-id]");
  return row?.dataset.id || sessions.currentId || "";
}

registerAction({
  id: "chat.close",
  title: "Close Chat",
  enabled: () => Boolean(closeTarget()),
  run: () => closeChat(closeTarget()),
});

// -- The probe (1.9) --

function rectOf(el) {
  if (!el) return null;
  const r = el.getBoundingClientRect();
  return { x: r.x, y: r.y, w: r.width, h: r.height };
}

const rectsOf = (el) => [...el.getClientRects()].filter((r) => r.width && r.height).map((r) => ({ x: r.x, y: r.y, w: r.width, h: r.height }));

// The text as drawn, not its box (a cut title ends at its "…").
function inkRect(el) {
  if (!el) return null;
  const range = document.createRange();
  range.selectNodeContents(el);
  const r = range.getBoundingClientRect();
  return r.width ? { x: r.x, y: r.y, w: r.width, h: r.height } : null;
}

// One text node's letters (without its outer white space), or its first
// letter alone: the look checks' text run and reference glyph.
function lettersRect(node, firstOnly = false) {
  const at = node?.data.search(/\S/) ?? -1;
  if (at < 0) return null;
  const range = document.createRange();
  range.setStart(node, at);
  range.setEnd(node, firstOnly ? at + 1 : node.data.trimEnd().length);
  const r = range.getBoundingClientRect();
  return r.width ? { x: r.x, y: r.y, w: r.width, h: r.height } : null;
}

// The drawn links: one entry per decoration, with all its spans' rects.
function shownLinks() {
  const out = new Map();
  for (const span of document.querySelectorAll(".md-deco-branch-link")) {
    const root = span.closest(".md, .text");
    const decoration = decorationOf(span);
    if (!root || !decoration) continue;
    const key = `${decoration.data?.id}\u0000${span.dataset.deco}`;
    let entry = out.get(key);
    if (!entry) {
      const frame = span.closest("[data-key]");
      const sid = decoration.data?.session_id ?? "";
      // title: what a hover sets now; title_attr: what the last hover left on the span.
      entry = { link_id: decoration.data?.id ?? "", session_id: sid, mid: decoration.data?.source_message_id ?? "", item: frame?.dataset.key ?? "", text: "", title: linkTitle(sid), title_attr: span.title, rects: [] };
      out.set(key, entry);
    }
    entry.text += span.textContent;
    entry.rects.push(...rectsOf(span));
  }
  return [...out.values()];
}

probeSection(
  "branch",
  () => ({
    links: Object.values(links.byId).map((l) => ({ id: l.id, session_id: l.session_id, parent_session_id: l.parent_session_id, mid: l.source_message_id, text: l.highlight_text, start: l.highlight_start, end: l.highlight_end, origin: l.origin })),
    closed: [...closed.ids],
    parent: parentOf(sessions.currentId) ?? "",
    making,
  }),
  () => {
    const nav = document.querySelector("nav.chats");
    const header = document.querySelector("header.head");
    const head = header?.querySelector(".path");
    const words = head?.firstChild?.nodeType === Node.TEXT_NODE ? head.firstChild : null;
    const footer = nav?.querySelector(".footer");
    return {
      shown: shownLinks(),
      rows: [...(nav?.querySelectorAll(".scroll .chat[data-id]") ?? [])].map((li) => {
        const close = li.querySelector(":scope > .close");
        return {
          id: li.dataset.id,
          row: rectOf(li.querySelector(".row")),
          title: rectOf(li.querySelector(".title")),
          title_text: inkRect(li.querySelector(".title")),
          time: rectOf(li.querySelector(".row > .time")),
          chevron: rectOf(li.querySelector(".chevron")),
          close: rectOf(close),
          close_glyph: rectOf(close?.querySelector("svg")),
          close_shown: Boolean(close) && getComputedStyle(close).visibility === "visible",
          close_label: close?.getAttribute("aria-label") ?? "",
        };
      }),
      // ink, day: the footer's letters and the day label's, which share a left edge.
      footer: footer ? { text: footer.textContent.trim(), rect: rectOf(footer), ink: inkRect(footer), day: inkRect(nav.querySelector(".day")) } : null,
      // words, glyph: "Branched from" and its first letter; link_text: the parent's title.
      head: head
        ? {
            text: head.textContent.trim(),
            rect: rectOf(head),
            title: rectOf(header.querySelector(".title")),
            link: rectOf(head.querySelector(".parent")),
            link_text: inkRect(head.querySelector(".parent")),
            words: lettersRect(words),
            glyph: lettersRect(words, true),
          }
        : null,
    };
  },
);
