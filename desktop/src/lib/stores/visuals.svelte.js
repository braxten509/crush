// The chat's figures for the VISUALS section (desktop-design 1.5.3, 1.5.6
// "Figure ids", 4.3). Every assistant message of the shown chat goes through
// extractFigures (P3.4) in idle-time slices of at most 4 ms of work (1.6 item
// 5); the result is kept with setFigures, so a chip and its figure share the
// id "<message id>:<n>". A message's figures are replaced as a whole: a retry
// that rewrites earlier text, or a block that closes, swaps that message's
// list and leaves the others alone.
//
//   visuals                       { bySession: sid → {order, byId}, marked }
//   setFigures(sid, mid, figures) that message's figures, replaced as a whole
//   figuresFor(sid)               Figure[] in chat order: message order, then index
//   revealFigure(id)              a chip's click: marks the figure 1.2 s and asks
//                                 VISUALS to scroll to it (reveal.seq)
//   reveal                        { id, seq }: the newest reveal; VisualsSection
//                                 scrolls once per seq, when the figure is there
//   syncFigures(sid)              queues the chat's changed assistant messages and
//                                 drops the figures of messages that left it; the
//                                 effect below runs it for the shown chat
import { untrack } from "svelte";
import { extractFigures } from "../render/figures.js";
import { messages } from "./messages.svelte.js";
import { sessions } from "./sessions.svelte.js";

export const visuals = $state({ bySession: {}, marked: "" }); // sid → { order: [figureId], byId: { figureId: Figure } }; marked: the figure with the 1.2 s bar
export const reveal = $state({ id: "", seq: 0 });

export const MARK_MS = 1200;
const SLICE_MS = 4;

// sid → Map(mid → figure ids in index order): which ids each message holds.
const held = new Map();

const figureKey = (f) => [f.id, f.kind, f.title, f.source, f.src ?? "", f.host ?? ""].join("\u0000");

function sameFigures(ids, entry, list) {
  if (ids.length !== list.length) return false;
  for (let i = 0; i < list.length; i++) {
    const old = entry?.byId?.[ids[i]];
    if (!old || figureKey(old) !== figureKey(list[i])) return false;
  }
  return true;
}

// Message order from the chat's ids; messages the chat does not list (yet)
// follow in the order their figures came.
function orderOf(sessionId, byMessage) {
  const ids = messages.bySession[sessionId]?.ids ?? [];
  const out = [];
  const placed = new Set();
  for (const mid of ids) {
    const list = byMessage.get(mid);
    if (!list || placed.has(mid)) continue;
    placed.add(mid);
    out.push(...list);
  }
  for (const [mid, list] of byMessage) if (!placed.has(mid)) out.push(...list);
  return out;
}

// Replaces that message's figures (Figure[] in source order).
export function setFigures(sessionId, messageId, figures) {
  if (!sessionId || !messageId) return;
  const list = (Array.isArray(figures) ? figures : []).filter((f) => f?.id).sort((a, b) => (a.index ?? 0) - (b.index ?? 0));
  let byMessage = held.get(sessionId);
  const before = byMessage?.get(messageId) ?? [];
  const entry = visuals.bySession[sessionId];
  if (sameFigures(before, entry, list)) return;
  if (!byMessage) held.set(sessionId, (byMessage = new Map()));
  const byId = { ...(entry?.byId ?? {}) };
  for (const id of before) delete byId[id];
  for (const f of list) byId[f.id] = f;
  if (list.length) byMessage.set(messageId, list.map((f) => f.id));
  else byMessage.delete(messageId);
  if (!byMessage.size) {
    held.delete(sessionId);
    delete visuals.bySession[sessionId];
    return;
  }
  visuals.bySession[sessionId] = { order: orderOf(sessionId, byMessage), byId };
}

// Figure[] in chat order: message order, then index.
export function figuresFor(sessionId) {
  const entry = visuals.bySession[sessionId];
  if (!entry) return [];
  return entry.order.map((id) => entry.byId[id]).filter(Boolean);
}

// A chip's click: scroll VISUALS to the figure, mark it 1.2 s (4.3).
let markTimer = null;
export function revealFigure(figureId) {
  if (!figureId) return;
  visuals.marked = figureId;
  reveal.id = figureId;
  reveal.seq += 1;
  clearTimeout(markTimer);
  markTimer = setTimeout(() => {
    markTimer = null;
    if (visuals.marked === figureId) visuals.marked = "";
  }, MARK_MS);
}

// -- Extraction in idle time (1.6 item 5) --

const seen = new Map(); // sid → Map(mid → the message object last queued)
const inputs = new Map(); // sid → Map(mid → "finished\0text" last extracted)
const waiting = new Map(); // sid → Set(mid) to extract
let scheduled = false;

// Message.Content() is the first text part, and a message is finished once it
// has a finish part (internal/proto/message.go); a stopped run gets one too
// (internal/agent/agent.go, FinishReasonCanceled).
const textOf = (m) => (m?.parts ?? []).find((p) => p?.type === "text")?.data?.text ?? "";
const finishedOf = (m) => (m?.parts ?? []).some((p) => p?.type === "finish");

function schedule() {
  if (scheduled) return;
  scheduled = true;
  if (typeof requestIdleCallback === "function") requestIdleCallback(drain, { timeout: 1000 });
  else setTimeout(drain, 0);
}

function extractOne(sessionId, mid) {
  const chat = messages.bySession[sessionId];
  const m = chat?.byId?.get(mid);
  if (!m || m.role !== "assistant") return;
  const text = textOf(m);
  const finished = finishedOf(m);
  let last = inputs.get(sessionId);
  if (!last) inputs.set(sessionId, (last = new Map()));
  const input = `${finished ? 1 : 0}\u0000${text}`;
  if (last.get(mid) === input) return;
  last.set(mid, input);
  setFigures(sessionId, mid, text ? extractFigures(sessionId, mid, text, { finished }) : []);
}

function drain() {
  scheduled = false;
  const start = performance.now();
  for (const [sessionId, mids] of waiting) {
    for (const mid of mids) {
      mids.delete(mid);
      extractOne(sessionId, mid);
      if (performance.now() - start >= SLICE_MS) {
        if (!mids.size) waiting.delete(sessionId);
        if (waiting.size) schedule();
        return;
      }
    }
    waiting.delete(sessionId);
  }
}

function forget(sessionId) {
  seen.delete(sessionId);
  inputs.delete(sessionId);
  waiting.delete(sessionId);
  held.delete(sessionId);
  if (sessionId in visuals.bySession) delete visuals.bySession[sessionId];
}

export function syncFigures(sessionId) {
  const loaded = new Set(Object.keys(messages.bySession));
  const chat = sessionId ? messages.bySession[sessionId] : null;
  const ids = chat?.ids ?? [];
  const shown = [];
  for (const mid of ids) {
    const m = chat.byId.get(mid);
    if (m?.role === "assistant") shown.push([mid, m]);
  }
  untrack(() => {
    // Figures of chats no longer loaded go (sessions.svelte.js unloads a
    // chat the window leaves).
    for (const sid of new Set([...Object.keys(visuals.bySession), ...seen.keys()])) {
      if (!loaded.has(sid)) forget(sid);
    }
    if (!chat) return;
    let known = seen.get(sessionId);
    if (!known) seen.set(sessionId, (known = new Map()));
    let mids = waiting.get(sessionId);
    const kept = new Set();
    for (const [mid, m] of shown) {
      kept.add(mid);
      if (known.get(mid) === m) continue;
      known.set(mid, m);
      if (!mids) waiting.set(sessionId, (mids = new Set()));
      mids.add(mid);
    }
    for (const mid of [...known.keys()]) {
      if (kept.has(mid)) continue;
      known.delete(mid);
      inputs.get(sessionId)?.delete(mid);
      mids?.delete(mid);
      setFigures(sessionId, mid, []);
    }
    // The chat's order can change without any figure changing (a message
    // placed by its time between two others).
    const byMessage = held.get(sessionId);
    const entry = visuals.bySession[sessionId];
    if (byMessage && entry) {
      const order = orderOf(sessionId, byMessage);
      if (order.length !== entry.order.length || order.some((id, i) => id !== entry.order[i])) entry.order = order;
    }
    if (mids?.size) schedule();
  });
}

// The shown chat, kept in step (a no-op in Node tests, which call syncFigures).
$effect.root(() => {
  $effect(() => {
    syncFigures(sessions.currentId);
  });
});
