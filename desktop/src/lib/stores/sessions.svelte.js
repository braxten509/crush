// The chat list and the shown chat (desktop-design 1.5.3; LIST-012, LIST-033 to
// LIST-037, TYPE-041, TYPE-049). The list holds the top-level chats, newest
// first, as the engine lists them (internal/db/sql/sessions.sql:36-40); agent
// tools' and title chats are only remembered as children of their chat.
import { api, onSend, setCurrentSession } from "../engine/api.js";
import { onEvent, onResync, withHeld } from "../engine/events.js";
import { chatOf, isFinished, load, messages, parseAgentChatId, readyChat, returnDraft, unload } from "./messages.svelte.js";
import { keepPromptsFor, seedMarks } from "./prompts.svelte.js";
import { clearNotice, notify } from "./ui.svelte.js";
import { dropFiles, setFiles, startFilesRead, ws } from "./workspace.svelte.js";

export const sessions = $state({
  byId: {},
  order: [],
  currentId: "",
  loaded: false,
  live: {}, // sid → true while a chat this window has not loaded is replying (1.5.3)
  opening: null, // null | {id, title, startedAt}: the chat a switch is opening (LIST-034)
  sent: {}, // sid → {run_id: true}: this window's sends without their run_complete yet (busy (b))
  activity: {}, // sid → updated_at of its newest message when newer than the record's (the list place)
});

// The four fields only a read fills (engine design 5.1(a), engine-design.md:193).
const PROTECTED = { is_busy: false, attached_clients: 0, has_unfinished_work: false, pending_prompt: "" };

// A read and the events that arrive while it is on its way: a read started
// before a run_complete (busy), a prompt_pending (prompt) or a live-flag change
// (live) must not undo them with what it computed before. readStart() marks
// the read; stamp() marks the event.
let clock = 0;
const stamps = new Map();
export function readStart() {
  clock += 1;
  return clock;
}
function stamp(sid, field) {
  clock += 1;
  stamps.set(`${field}:${sid}`, clock);
}
function changedSince(sid, field, since) {
  return (stamps.get(`${field}:${sid}`) ?? 0) > since;
}

const childParent = new Map(); // a child chat's id → its parent's id (session events with a parent)
const lastSource = new Map(); // sid → "event" | "read": where the stored record's other fields came from

function storeSession(s, { fromEvent, since }) {
  const stored = sessions.byId[s.id];
  let next;
  if (fromEvent) {
    next = { ...s };
    for (const [key, empty] of Object.entries(PROTECTED)) next[key] = stored ? stored[key] : empty;
    lastSource.set(s.id, "event");
  } else {
    // An event newer than this read (or as new: events win ties, as
    // applyLoadedSession keeps the newer record, session_load.go:274-277)
    // keeps its fields; the four computed ones are the read's.
    const storedAt = stored?.updated_at ?? 0;
    const readAt = s.updated_at ?? 0;
    const keep = Boolean(stored) && (storedAt > readAt || (storedAt === readAt && lastSource.get(s.id) === "event"));
    next = keep ? { ...stored } : { ...s };
    if (!keep) lastSource.set(s.id, "read");
    next.attached_clients = s.attached_clients ?? 0;
    next.has_unfinished_work = s.has_unfinished_work === true;
    next.is_busy = changedSince(s.id, "busy", since) ? (stored?.is_busy ?? false) : s.is_busy === true;
    if (changedSince(s.id, "prompt", since)) {
      next.pending_prompt = stored?.pending_prompt ?? "";
    } else {
      next.pending_prompt = s.pending_prompt ?? "";
      seedMarks(s.id, next.pending_prompt);
    }
    // The live flag of a chat not loaded becomes the read's is_busy (the
    // list's and each chat's reads alike, so a chat that ends with no event
    // stops working).
    if (messages.bySession[s.id]?.status !== "ready" && !changedSince(s.id, "live", since)) {
      if (s.is_busy === true) sessions.live[s.id] = true;
      else delete sessions.live[s.id];
    }
  }
  sessions.byId[s.id] = next;
}

// fromEvent keeps is_busy, attached_clients, has_unfinished_work and
// pending_prompt: session events never carry them (1.5.3). A read (the
// default) brings them; since (readStart()) guards what arrived meanwhile.
export function upsertSession(s, { fromEvent = false, since = Infinity } = {}) {
  if (!s?.id) return;
  if (s.parent_session_id) {
    childParent.set(s.id, s.parent_session_id);
    return;
  }
  storeSession(s, { fromEvent, since });
  place(s.id);
}

export function removeSession(id) {
  childParent.delete(id);
  if (!sessions.byId[id]) return;
  delete sessions.byId[id];
  sessions.order = sessions.order.filter((x) => x !== id);
  delete sessions.live[id];
  delete sessions.sent[id];
  delete sessions.activity[id];
}

// Newest first by the record's updated_at or its newest message's.
function placeKey(id) {
  return Math.max(sessions.byId[id]?.updated_at ?? 0, sessions.activity[id] ?? 0);
}
function place(id) {
  const key = placeKey(id);
  const rest = sessions.order.filter((x) => x !== id);
  let i = 0;
  while (i < rest.length && placeKey(rest[i]) > key) i += 1;
  if (sessions.order[i] === id && sessions.order.length === rest.length + 1) return;
  sessions.order = [...rest.slice(0, i), id, ...rest.slice(i)];
}

// The top-level chat a chat belongs to ("" when this window cannot place it):
// a helper's or title chat's parent from its session record, an agent tool's
// chat from the loaded message that started it.
export function rootOf(sid) {
  let id = sid;
  for (let hops = 0; id && hops < 16; hops += 1) {
    if (sessions.byId[id]) return id;
    const parsed = parseAgentChatId(id);
    const parent = childParent.get(id) || (parsed ? chatOf(parsed.messageId) : "");
    if (!parent) return "";
    id = parent;
  }
  return "";
}

// -- Busy (1.5.3) --

// True when (a) the last read said is_busy, (b) this window sent and has no
// run_complete for that run yet, (c) the chat is loaded and has an assistant
// message a live event showed without a finish part, or (d) the live flag.
export function busy(id) {
  if (!id) return false;
  if (sessions.byId[id]?.is_busy) return true;
  const sent = sessions.sent[id];
  if (sent && Object.keys(sent).length > 0) return true;
  const chat = messages.bySession[id];
  if (chat?.status === "ready" && chat.active.size > 0) return true;
  return sessions.live[id] === true;
}

function clearSent(sid, runId) {
  const sent = sessions.sent[sid];
  if (!sent) return;
  if (!runId) {
    delete sessions.sent[sid];
    return;
  }
  if (!(runId in sent)) return;
  const rest = { ...sent };
  delete rest[runId];
  if (Object.keys(rest).length > 0) sessions.sent[sid] = rest;
  else delete sessions.sent[sid];
}

// (b): every send of this window, until its run_complete; a send the engine
// did not take starts no run.
onSend((body, call) => {
  const sid = body?.session_id;
  const runId = body?.run_id;
  if (!sid || !runId) return;
  sessions.sent[sid] = { ...(sessions.sent[sid] ?? {}), [runId]: true };
  Promise.resolve(call).catch(() => clearSent(sid, runId));
});

// A run_complete clears (a), (b) for its run, and (d); the queue can start
// the next turn, whose messages make the chat busy again.
onEvent("run_complete", (_kind, done) => {
  const sid = done?.session_id;
  if (!sid || !sessions.byId[sid]) return;
  stamp(sid, "busy");
  stamp(sid, "live");
  if (sessions.byId[sid].is_busy) sessions.byId[sid].is_busy = false;
  clearSent(sid, done.run_id);
  delete sessions.live[sid];
});

// An interrupt with nothing to send next: the chat is idle at once (CHAT-026,
// TYPE-046; interruptAgent sets the busy state to `continuing`, send.go:284-287).
export function settleBusy(sid) {
  if (!sid) return;
  stamp(sid, "busy");
  stamp(sid, "live");
  if (sessions.byId[sid]?.is_busy) sessions.byId[sid].is_busy = false;
  delete sessions.sent[sid];
  delete sessions.live[sid];
}

// A message of a known chat: a newer one moves the chat up the list; in a
// chat this window has not loaded, an assistant message without finish sets
// the live flag and one with finish clears it (1.5.3).
export function noteChatMessage(sid, kind, m, { loaded = false } = {}) {
  const record = sessions.byId[sid];
  if (!record || kind === "deleted") return;
  const at = m.updated_at ?? 0;
  if (at > Math.max(record.updated_at ?? 0, sessions.activity[sid] ?? 0)) {
    sessions.activity[sid] = at;
    place(sid);
  }
  if (loaded || m.role !== "assistant") return;
  stamp(sid, "live");
  if (!isFinished(m)) sessions.live[sid] = true;
  else delete sessions.live[sid];
}

// The prompt_pending mark of a root chat (the prompts store keeps both kinds).
export function markPromptPending(root, value) {
  const record = sessions.byId[root];
  if (!record) return;
  stamp(root, "prompt");
  if (record.pending_prompt !== value) record.pending_prompt = value;
}

// -- Session events --

const readOnce = new Set();

async function readChat(id) {
  const since = readStart();
  try {
    const s = await api.getSession(id);
    if (s?.id === id && sessions.byId[id]) upsertSession(s, { since });
  } catch (error) {
    console.error("chat read failed", error);
  }
}

// A record from an event; a chat this window has not seen is read once, which
// brings what the events never carry (1.5.3). An `updated` for an unseen chat
// is read the same way (its `created` came before this window listened).
onEvent("session", (kind, s) => {
  if (!s?.id) return;
  if (kind === "deleted") {
    chatDeleted(s.id);
    return;
  }
  if (s.parent_session_id) {
    childParent.set(s.id, s.parent_session_id);
    return;
  }
  const seen = Boolean(sessions.byId[s.id]);
  upsertSession(s, { fromEvent: true });
  if (!seen && !readOnce.has(s.id)) {
    readOnce.add(s.id);
    readChat(s.id);
  }
});

const ownDeletes = new Set();

// Phase 5's delete goes through here, so this window's own delete shows no
// "deleted elsewhere" notice.
export async function deleteChat(id) {
  ownDeletes.add(id);
  try {
    return await api.deleteSession(id);
  } catch (error) {
    ownDeletes.delete(id);
    throw error;
  }
}

// LIST-034: a chat deleted while it opens is abandoned. LIST-037: the shown
// chat deleted elsewhere leaves the window on the landing state with a notice
// (the terminal starts a new chat, internal/ui/model/update_workspace.go:119-126).
function chatDeleted(id) {
  if (!sessions.byId[id]) {
    childParent.delete(id);
    return;
  }
  if (sessions.opening?.id === id) abandonOpen();
  const shown = sessions.currentId === id;
  const own = ownDeletes.delete(id);
  removeSession(id);
  if (shown) {
    showLanding();
    if (!own) notify("info", "This chat was deleted elsewhere.");
  }
  unload(id);
  dropFiles(id);
}

// -- The list --

function applyList(list, since) {
  const listed = [];
  for (const s of list) {
    if (!s?.id || s.parent_session_id || listed.includes(s.id)) continue;
    listed.push(s.id);
    storeSession(s, { fromEvent: false, since });
  }
  const kept = new Set(listed);
  for (const id of Object.keys(sessions.byId)) {
    if (!kept.has(id) && id !== sessions.currentId && id !== sessions.opening?.id) removeSession(id);
  }
  const extra = sessions.order.filter((id) => !kept.has(id) && sessions.byId[id]);
  sessions.order = listed;
  for (const id of extra) place(id);
}

// GET sessions, with session events held so none lands before the list.
export async function loadSessions({ since = readStart() } = {}) {
  await withHeld(["session"], async () => {
    const list = await api.listSessions();
    applyList(Array.isArray(list) ? list : [], since);
  });
  sessions.loaded = true;
}

// After a reconnect: sends from before it have their run_complete or not, and
// the list's is_busy says which chats still run; a chat that was opening is
// opened again (SIDE-030; internal/ui/model/agent_events.go:52-62).
onResync(async () => {
  const since = readStart();
  sessions.sent = {};
  await loadSessions({ since });
  if (sessions.opening) openChat(sessions.opening.id);
});

// -- Showing a chat --

function report(sid) {
  setCurrentSession(sid).catch((error) => console.error("current session report failed", error));
}

function errorText(error) {
  return error?.message ?? String(error);
}

let openSeq = 0;
let deferred = []; // sends made while a switch was pending (TYPE-049)

// A send while another chat opens is meant for that chat: it waits for the
// open and goes to it, or returns to the field when the open fails
// (deferSendDuringSwitch, internal/ui/model/session_load.go:155-193). send is
// {run(sid), text, attachments, hidden, shell}. True when it was held.
export function deferSend(send) {
  if (!sessions.opening || sessions.opening.id === sessions.currentId) return false;
  deferred.push(send);
  return true;
}

function runDeferred(sid) {
  const sends = deferred;
  deferred = [];
  for (const send of sends) {
    Promise.resolve()
      .then(() => send.run(sid))
      .catch((error) => console.error("a held send failed", error));
  }
}

// restoreDeferredSends (session_load.go:176-193): a shell command is not run;
// a prompt goes back to the field of the chat still shown.
function restoreDeferred() {
  const sends = deferred;
  deferred = [];
  for (const send of sends) {
    if (send.shell) notify("warn", "Shell command not run: the conversation did not open");
    else if (!send.hidden) returnDraft(sessions.currentId, { text: send.text ?? "", attachments: send.attachments ?? [] });
  }
}

// Drops the switch in flight, so its late result is ignored, and gives its
// held sends back (abandonSessionLoad, session_load.go:144-153).
function abandonOpen() {
  if (!sessions.opening) return;
  const id = sessions.opening.id;
  sessions.opening = null;
  openSeq += 1;
  restoreDeferred();
  if (id !== sessions.currentId) unload(id);
  report(sessions.currentId);
}

// Leaving a chat: its messages go (switching back loads them again), and a
// reply it was writing stays visible in the list through the live flag.
function leave(sid) {
  const chat = messages.bySession[sid];
  if (chat?.status === "ready" && chat.active.size > 0 && sessions.byId[sid]) {
    stamp(sid, "live");
    sessions.live[sid] = true;
  }
  unload(sid);
  dropFiles(sid);
}

// Exported for closing the last listed chat (closed.svelte.js, P4.1), which
// leaves a busy chat too.
export function showLanding() {
  abandonOpen();
  const old = sessions.currentId;
  sessions.currentId = "";
  keepPromptsFor("");
  report("");
  if (old) leave(old);
}

// LIST-034, LIST-035, TYPE-049 (loadSession and applyLoadedSession,
// internal/ui/model/session.go:86-133, session_load.go:265-353): the chat's
// record, file history and messages are read at once; a newer switch wins; a
// failure keeps the window where it was. Resolves true when the chat shows.
// File events wait until the chat's file history is set, as in the resync
// (1.5.4): a history the engine read before an event never overwrites it, and
// an event for a chat with no list yet is not dropped.
export async function openChat(id) {
  if (!id) return false;
  let filesSet;
  withHeld(["file"], () => new Promise((resolve) => (filesSet = resolve)));
  try {
    return await showChat(id);
  } finally {
    filesSet();
  }
}

// A failed file history read: the chat opens with an empty FILES list, and the
// warning stays with a way to try again (owner, 2026-10-20).
const HISTORY_FAILED = "Couldn't read this chat's file history, so its changed files are not listed";
function noteHistoryFailed(id) {
  notify("warn", HISTORY_FAILED, 0, { label: "Try again", run: () => retryHistory(id) });
}
async function retryHistory(id) {
  const read = startFilesRead(id);
  try {
    const list = await api.history(id);
    if (setFiles(id, list, read)) clearNotice(HISTORY_FAILED);
  } catch (error) {
    console.error("file history read failed", errorText(error));
    noteHistoryFailed(id);
  }
}

async function showChat(id) {
  openSeq += 1;
  const seq = openSeq;
  if (id !== sessions.currentId) {
    if (sessions.opening?.id !== id) sessions.opening = { id, title: sessions.byId[id]?.title ?? "", startedAt: Date.now() };
  } else if (sessions.opening) {
    const other = sessions.opening.id;
    sessions.opening = null;
    if (other !== id) unload(other);
  }
  report(id);
  const since = readStart();
  // The file history only fills FILES (SIDE-008): unlike the terminal's load,
  // which fails as a whole (session.go:115-117), a failed history read opens
  // the chat with no files and a warning.
  let historyFailed = false;
  const filesRead = startFilesRead(id);
  const history = api.history(id).catch((error) => {
    console.error("file history read failed", errorText(error));
    historyFailed = true;
    return [];
  });
  const [read, files, loaded] = await Promise.allSettled([api.getSession(id), history, load(id)]);
  if (seq !== openSeq) {
    if (id !== sessions.currentId && sessions.opening?.id !== id) unload(id);
    return false;
  }
  const failed = [read, loaded].find((r) => r.status === "rejected");
  if (failed || !read.value?.id) {
    if (sessions.opening?.id === id) sessions.opening = null;
    report(sessions.currentId);
    keepPromptsFor(sessions.currentId);
    restoreDeferred();
    if (id !== sessions.currentId) unload(id);
    const error = failed ? failed.reason : new Error(`session not found: ${id}`);
    notify("error", `Couldn't load this chat: ${errorText(error)}`, 5000, { label: "Try again", run: () => openChat(id) });
    return false;
  }
  if (loaded.value !== true) return false; // the chat went away while it loaded
  upsertSession(read.value, { since });
  setFiles(id, files.value, filesRead);
  if (historyFailed) noteHistoryFailed(id);
  const old = sessions.currentId;
  sessions.currentId = id;
  if (sessions.opening?.id === id) sessions.opening = null;
  stamp(id, "live");
  delete sessions.live[id]; // its messages answer for it now (1.5.3)
  keepPromptsFor(id);
  if (old && old !== id) leave(old);
  restoreModel(id);
  runDeferred(id);
  return true;
}

// LIST-033 (newSessionFromKey and newSession, internal/ui/model/session.go:
// 317-420): the landing state, where the next send makes the chat (TYPE-041).
export async function newChat() {
  const sid = sessions.currentId;
  if (!sid) return false;
  if (busy(sid)) {
    notify("warn", "Agent is busy, please wait before starting a new session...");
    return false;
  }
  showLanding();
  return true;
}

let creating = null;

// The shown chat's id; on the landing page it makes "New Session" first
// (ensureSession, internal/ui/model/send.go:33-55). A second call while the
// first is on its way gets the same chat.
export async function ensureSession() {
  if (sessions.currentId) return sessions.currentId;
  creating ??= (async () => {
    try {
      const created = await api.createSession({ title: "New Session" });
      if (!created?.id) throw new Error("the engine made no chat");
      upsertSession(created);
      readyChat(created.id);
      setFiles(created.id, []);
      sessions.currentId = created.id;
      keepPromptsFor(created.id);
      report(created.id);
      return created.id;
    } finally {
      creating = null;
    }
  })();
  return creating;
}

// START-006: after joining, `-s ID` opens that chat and `-C` the newest one
// (loadInitialSession, internal/ui/model/session.go:330-351). An ID that is
// not a whole chat id (a hash or a prefix) and the error texts are START-021's
// (lib/sessionid.js, P3.9). Nothing happens once the user opened a chat.
export async function openLaunchChat({ session = "", continueLast = false } = {}) {
  if (sessions.currentId || sessions.opening) return false;
  if (session) return sessions.byId[session] ? openChat(session) : false;
  if (continueLast && sessions.order.length > 0) return openChat(sessions.order[0]);
  return false;
}

// -- LIST-035: the chat's last model (restoreModelFromSession,
// internal/ui/model/model_select.go:40-101) --

// config.IsModelAvailable (internal/config/config.go:225-250).
function modelAvailable(config, provider, model) {
  const p = config?.providers?.[provider];
  if (!p || p.disable === true) return false;
  return [p.models, p.chatgpt_models, p.grok_models].some((list) => Array.isArray(list) && list.some((m) => m?.id === model));
}

async function restoreModel(sid) {
  const chat = messages.bySession[sid];
  if (!chat) return;
  let last = null;
  for (let i = chat.ids.length - 1; i >= 0 && !last; i -= 1) {
    const m = chat.byId.get(chat.ids[i]);
    if (m?.role === "assistant" && !m.is_summary_message) last = m;
  }
  if (!last?.provider || !last.model) return;
  const shown = ws.config?.models?.large;
  if (shown?.provider === last.provider && shown?.model === last.model) return;
  let available = false;
  let hasSmall = false;
  try {
    // The whole config holds the providers' keys: it is read here and never kept.
    const config = await api.config();
    const large = config?.models?.large;
    if (large?.provider === last.provider && large?.model === last.model) return;
    available = modelAvailable(config, last.provider, last.model);
    hasSmall = Boolean(config?.models?.small);
  } catch (error) {
    console.error("model restore: config read failed", errorText(error));
    return;
  }
  if (!available || sessions.currentId !== sid) return;
  const model = { provider: last.provider, model: last.model };
  try {
    await api.setModel("global", "large", model);
  } catch (error) {
    console.error("Failed to restore model from session", errorText(error));
    return;
  }
  if (!hasSmall) {
    try {
      await api.setModel("global", "small", await api.defaultSmallModel(model.provider));
    } catch (error) {
      console.error("Failed to set small model during session restore", errorText(error));
    }
  }
  try {
    await api.updateAgent();
  } catch (error) {
    notify("error", errorText(error)); // util.ReportError, model_select.go:91-95
  }
}
