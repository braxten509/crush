// Branch links between chats (desktop-design 3.3; engine 5.11). Every link the
// engine has, by id; the links each message shows (the renderer's branch-link
// decorations); each branch's origin link (the list's nesting and the head's
// "branched from"). A branch brings two kinds: its origin link (origin: true,
// shown in the parent) and copies of the links on the messages it copied
// (origin: false, shown in the branch, opening the same chats), so sibling
// branches stay reachable from inside a branch (PLAN.md:104).
//
// The engine sends `chat_link` created events only; links go away through
// database cascades when a chat or message is deleted, so the whole list is
// read again after a `session` deleted event, on a resync and when the window
// joins (engine-design.md:2310). One read at a time; calls meanwhile become
// one more read after it.
import { api } from "../engine/api.js";
import { onEvent, onResync, withHeld } from "../engine/events.js";
import { chatOf } from "./messages.svelte.js";

export const links = $state({ byId: {}, byParentMessage: {}, originOf: {} }); // originOf: branch session id → origin ChatLink

// The two indexes, rebuilt from byId after every change (a project has tens
// of links, not thousands). byParentMessage lists a message's links by start.
function reindex() {
  const byParentMessage = {};
  const originOf = {};
  for (const link of Object.values(links.byId)) {
    (byParentMessage[link.source_message_id] ??= []).push(link);
    if (link.origin) originOf[link.session_id] = link;
  }
  for (const list of Object.values(byParentMessage)) list.sort((a, b) => a.highlight_start - b.highlight_start);
  links.byParentMessage = byParentMessage;
  links.originOf = originOf;
}

const FIELDS = ["session_id", "parent_session_id", "source_message_id", "highlight_text", "highlight_start", "highlight_end", "origin", "created_at"];

function valid(link) {
  return Boolean(link?.id && link.session_id && link.source_message_id) && Number.isInteger(link.highlight_start) && Number.isInteger(link.highlight_end);
}

function plain(link) {
  const out = { id: link.id };
  for (const key of FIELDS) out[key] = link[key];
  out.origin = link.origin === true;
  return out;
}

const same = (a, b) => Boolean(a) && FIELDS.every((key) => a[key] === b[key]);

// chat_link created, and the link in a branch call's answer. Applying the
// same link twice changes nothing. Kept even when its chats are not listed
// yet: the list and the renderer look links up each time they draw (1.5.3).
export function upsertLink(link) {
  if (!valid(link)) return;
  const next = plain(link);
  if (same(links.byId[next.id], next)) return;
  links.byId[next.id] = next;
  reindex();
}

function replaceAll(list) {
  const byId = {};
  for (const link of list) if (valid(link)) byId[link.id] = plain(link);
  links.byId = byId;
  reindex();
}

let reading = null;
let readAgain = false;

// GET chat-links, with chat_link events held so none lands before the list
// (and none is lost). Resolves true when the newest read landed.
export function loadLinks() {
  if (reading) {
    readAgain = true;
    return reading;
  }
  reading = (async () => {
    let landed = false;
    try {
      do {
        readAgain = false;
        try {
          await withHeld(["chat_link"], async () => {
            const list = await api.chatLinks();
            replaceAll(Array.isArray(list) ? list : []);
          });
          landed = true;
        } catch (error) {
          landed = false;
          console.error("chat links read failed", error?.message ?? error);
        }
      } while (readAgain);
    } finally {
      reading = null;
    }
    return landed;
  })();
  return reading;
}

// Decorations for the renderer: [{kind: "branch-link", start, end, data: link}].
// A link shows only while its message is on a loaded chat's current path: a
// tree_changed reload that takes the message off the path hides it, and
// bringing the message back shows it again (3.3, PLAN.md:104).
export function linksFor(messageId) {
  const list = links.byParentMessage[messageId];
  if (!list?.length || !chatOf(messageId)) return [];
  return list.map((link) => ({ kind: "branch-link", start: link.highlight_start, end: link.highlight_end, data: link }));
}

// The origin link's parent_session_id, or null (top level). A deleted parent
// takes its links with it (engine 5.11), so its branches answer null.
export function parentOf(sessionId) {
  return links.originOf[sessionId]?.parent_session_id || null;
}

// The link a branch was made from, or null (the head's "branched from").
export function originOf(sessionId) {
  return links.originOf[sessionId] ?? null;
}

onEvent("chat_link", (kind, link) => {
  if (kind === "deleted") {
    if (link?.id && links.byId[link.id]) {
      delete links.byId[link.id];
      reindex();
    }
    return;
  }
  upsertLink(link);
});

onEvent("session", (kind) => {
  if (kind === "deleted") loadLinks();
});

// The first join reads the links; a rejoin's resync reads them again below.
onEvent("connection", (_kind, ev) => {
  if (ev?.state === "connected" && ev.resync !== true) loadLinks();
});

onResync(() => loadLinks());
