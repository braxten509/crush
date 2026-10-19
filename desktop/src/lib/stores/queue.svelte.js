// Queued prompts per chat (desktop-design 1.5.3; TYPE-019, TYPE-044, TYPE-045,
// TYPE-050). The engine's queue behind the running turn, as GET …/prompts/queued
// and …/prompts/list?details=true give it, read again when this window sends,
// stops or recalls, when the shown chat's run ends or a user message lands in
// it, on a resync, and every 2 s while the shown chat is busy or has a queue
// (the terminal's backstop, internal/ui/model/workspace_cache.go; the
// composer store's startComposer runs that timer). One read of a chat runs at
// a time; whatever asks during it gets one more read after it.
import { api, uuid } from "../engine/api.js";
import { onEvent, onResync } from "../engine/events.js";
import { kindOf } from "../composer/paste.js";
import { queueLabel, visibleQueue } from "../composer/trayrows.js";
import { finishPending, heldFor, messages } from "./messages.svelte.js";
import { sessions } from "./sessions.svelte.js";

export const queue = $state({ bySession: {} }); // sid → { count, items: [{ text, attachments, label }], loaded }

const RECALL_LIMIT_MS = 10_000;

const reads = new Map(); // sid → {again, promise}: the read in flight
const gens = new Map(); // sid → a number every change of this window bumps, so an older answer is dropped

function bump(sid) {
  gens.set(sid, (gens.get(sid) ?? 0) + 1);
}

function entryOf(sid) {
  return queue.bySession[sid] ?? { count: 0, items: [], loaded: false };
}

async function readQueue(sid) {
  const gen = gens.get(sid) ?? 0;
  const [count, list] = await Promise.all([api.queuedCount(sid), api.queuedSummaries(sid)]);
  if ((gens.get(sid) ?? 0) !== gen) return false;
  const entries = Array.isArray(list) ? list : [];
  queue.bySession[sid] = {
    count: typeof count === "number" ? count : (count?.count ?? entries.length),
    items: entries.map((entry) => ({ text: entry?.prompt ?? "", attachments: [], label: queueLabel(entry?.prompt ?? ""), submissionId: entry?.submission_id ?? "" })),
    loaded: true,
  };
  return true;
}

// GET …/prompts/queued and …/prompts/list?details=true
export function refresh(sid) {
  if (!sid) return Promise.resolve();
  const running = reads.get(sid);
  if (running) {
    running.again = true;
    return running.promise;
  }
  const read = { again: false, promise: null };
  read.promise = (async () => {
    try {
      do {
        read.again = false;
        try {
          if (!(await readQueue(sid))) read.again = true;
        } catch (error) {
          console.error("queue read failed", error?.message ?? error);
        }
      } while (read.again && sessions.byId[sid]);
    } finally {
      reads.delete(sid);
    }
  })();
  reads.set(sid, read);
  return read.promise;
}

// TYPE-050 (…/prompts/clear): the queue is empty at once (clearPromptQueue,
// internal/ui/model/send.go:318-326). This window's held prompts the engine
// had taken go with it; one whose send has no answer yet stays, since the
// engine queues it after the clear.
export async function clear(sid) {
  if (!sid) return;
  bump(sid);
  queue.bySession[sid] = { ...entryOf(sid), count: 0, items: [], loaded: true };
  for (const item of heldFor(sid)) if (item.acknowledged) finishPending(item.submissionId, false);
  try {
    await api.clearQueue(sid);
  } finally {
    refresh(sid);
  }
}

// AgentMessage attachments (content base64) as the field's Attachment.
function attachmentOf(wire) {
  const mime = wire?.mime_type ?? "";
  return { kind: kindOf(mime), name: wire?.file_name ?? "", mime, path: wire?.file_path ?? "", base64: wire?.content ?? "" };
}

// At the limit the request is stopped, as the terminal's context deadline
// stops it (internal/client/agent.go:131-139): its token stays, so an answer
// that was already on its way comes back on the next try.
function withLimit(promise, ms, callId) {
  let timer;
  const limit = new Promise((_, reject) => {
    timer = setTimeout(() => {
      api.abort(callId).catch(() => {});
      reject({ code: "timeout", message: "context deadline exceeded" });
    }, ms);
  });
  return Promise.race([promise, limit]).finally(() => clearTimeout(timer));
}

// TYPE-019 (…/prompts/recall) → { text, attachments, submissionId } or null
// when nothing was queued; rejects with the engine's error. 10 s at most
// (recallQueuedPrompt, internal/ui/model/history.go:106-119). The caller
// calls recalled(sid) once a prompt is back in the composer.
export async function recall(sid) {
  bump(sid);
  const callId = uuid();
  try {
    const prompt = await withLimit(api.recallQueued(sid, callId), RECALL_LIMIT_MS, callId);
    if (!prompt) {
      api.acknowledgeRecall(sid);
      return null;
    }
    return {
      text: prompt.prompt ?? "",
      attachments: (prompt.attachments ?? []).map(attachmentOf),
      submissionId: prompt.submission_id ?? "",
    };
  } finally {
    refresh(sid);
  }
}

// The recalled prompt is in the composer: the engine may drop its copy.
export function recalled(sid) {
  api.acknowledgeRecall(sid);
}

// The chat's shown user messages that carry a submission id, newest first;
// queued prompts are recent, so the last 200 messages are enough.
function shownIds(sid) {
  const chat = messages.bySession[sid];
  const ids = new Set();
  if (chat?.status !== "ready") return ids;
  for (let i = chat.ids.length - 1, seen = 0; i >= 0 && seen < 200; i -= 1, seen += 1) {
    const m = chat.byId.get(chat.ids[i]);
    if (m?.role !== "user") continue;
    const text = (m.parts ?? []).find((p) => p?.type === "text");
    if (text?.data?.submission_id) ids.add(text.data.submission_id);
  }
  return ids;
}

// The prompts the tray lists for chat sid (TYPE-044, TYPE-045): the engine's
// queue without the prompts the timeline shows, then this window's held
// prompts the engine does not list yet; queueRows (lib/composer/trayrows.js)
// makes them rows.
export function visibleItems(sid) {
  if (!sid) return [];
  const items = entryOf(sid).items;
  const held = heldFor(sid).map((p) => ({ text: p.text, submissionId: p.submissionId }));
  if (items.length === 0 && held.length === 0) return [];
  const entries = items.map((item) => ({ prompt: item.text, submission_id: item.submissionId }));
  return visibleQueue({ entries, shownIds: shownIds(sid), held });
}

// A run's end and a user message in the shown chat change its queue.
onEvent("run_complete", (_kind, done) => {
  if (done?.session_id && done.session_id === sessions.currentId) refresh(done.session_id);
});
onEvent("message", (_kind, m) => {
  if (m?.role === "user" && m.session_id && m.session_id === sessions.currentId) refresh(m.session_id);
});
onResync(() => {
  if (sessions.currentId) return refresh(sessions.currentId);
});
