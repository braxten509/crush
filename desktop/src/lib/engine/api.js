// One function per engine route (desktop-design 1.5.2), each a promise of the
// engine's JSON. Paths are relative to the window's workspace ("sessions" is
// /v1/workspaces/{id}/sessions) or absolute ("/v1/limits"); Rust adds this
// window's client_id to every call and refuses routes outside its fixed list
// (1.3). Failures reject with the AppError of commands.js.
import * as commands from "./commands.js";

// One path segment. Rust splits the path at "/" and percent-encodes every
// part itself (connection.rs Route::find and Route::target), so a value goes
// as it is: encoding it here would encode it twice. A value with "/" would be
// read as several parts, which could name another route, so it goes as "..",
// a part Rust always refuses (route_not_allowed), as it refuses "" and ".".
const seg = (value) => (String(value).includes("/") ? ".." : String(value));
const chat = (sid) => `sessions/${seg(sid)}`;
const run = (sid) => `agent/sessions/${seg(sid)}`;

const get = (path, query) => commands.api("GET", path, query);
const post = (path, body, query, callId) => commands.api("POST", path, query, body, callId);

// config.Scope travels as a number (internal/config/scope.go:6-13): global 0,
// workspace 1. The functions below also take the two names.
export const SCOPE = { global: 0, workspace: 1 };
const scopeOf = (scope) => (typeof scope === "number" ? scope : (SCOPE[scope] ?? SCOPE.global));

// A random UUID (v4). crypto.randomUUID needs a secure context, which tauri://
// is inferred to be; getRandomValues works everywhere (1.5.2).
export function uuid() {
  const c = globalThis.crypto;
  if (typeof c?.randomUUID === "function") {
    try {
      return c.randomUUID();
    } catch {
      // Not a secure context: build it below.
    }
  }
  const b = c.getRandomValues(new Uint8Array(16));
  b[6] = (b[6] & 0x0f) | 0x40;
  b[8] = (b[8] & 0x3f) | 0x80;
  const h = Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}

// Stores watch this window's own sends and stops without wrapping the calls:
// onSend(fn) gets (body, call) for every send (body has its run_id; call is
// the send's promise), onInterrupt(fn) gets the chat id just before the
// interrupt is posted. Each returns a function that unregisters it.
const sendWatchers = new Set();
const interruptWatchers = new Set();
export function onSend(fn) {
  sendWatchers.add(fn);
  return () => sendWatchers.delete(fn);
}
export function onInterrupt(fn) {
  interruptWatchers.add(fn);
  return () => interruptWatchers.delete(fn);
}
function tell(watchers, ...args) {
  for (const fn of watchers) {
    try {
      fn(...args);
    } catch (error) {
      console.error("a watcher failed", error);
    }
  }
}

// The recall token per chat, kept until the engine answers, so a retry after a
// lost answer gets the same prompt back instead of withdrawing another one
// (internal/client/agent.go:127-148; the route requires it,
// internal/server/handlers_agent.go:110-130).
const recallTokens = new Map();

export const api = {
  version: () => get("/v1/version"),
  engine: () => get("/v1/engine"),
  limits: () => get("/v1/limits"),
  refreshLimits: (kind) => post(`/v1/limits/${seg(kind)}/refresh`),

  config: () => get("config"),
  providers: () => get("providers"),
  agentInfo: () => get("agent"),
  // The engine reads provider_id (internal/server/handlers_agent.go:102-104).
  defaultSmallModel: (providerId) => get("agent/default-small-model", providerId ? { provider_id: providerId } : undefined),
  gitBranch: () => get("git/branch"),
  lsps: () => get("lsps"),
  // One server's diagnostics by file (rpc/routes.go:79), counted per severity for SIDE-009.
  lspDiagnostics: (name) => get(`lsps/${seg(name)}/diagnostics`),
  mcpStates: () => get("mcp/states"),
  skills: () => get("skills"),
  allUserMessages: () => get("messages/user"),

  listSessions: () => get("sessions"),
  createSession: (session) => post("sessions", session),
  getSession: (sid) => get(chat(sid)),
  // The whole record as last read (engine design 5.1(a)).
  saveSession: (session) => commands.api("PUT", chat(session.id), undefined, session),
  deleteSession: (sid) => commands.api("DELETE", chat(sid)),
  messages: (sid) => get(`${chat(sid)}/messages`),
  userMessages: (sid) => get(`${chat(sid)}/messages/user`),
  review: (sid, mid) => get(`${chat(sid)}/messages/${seg(mid)}/review`),
  history: (sid) => get(`${chat(sid)}/history`),
  trackedFiles: (sid) => get(`${chat(sid)}/filetracker/files`),
  setKeptSkill: (sid, { id, name, on, toggleable }) => post(`${chat(sid)}/kept-skills`, { id, name, on, toggleable }),

  // Adds a fresh run_id unless the caller gave one; the 202 means accepted, and
  // the run's end is the run_complete with that run_id.
  send({ session_id, prompt, attachments, submission_id, run_id, hidden_user_message, channel }) {
    const body = { session_id, prompt, attachments, submission_id, run_id: run_id || uuid(), hidden_user_message, channel };
    const call = post("agent", body);
    tell(sendWatchers, body, call);
    return call;
  },
  setMainAgent: (agent_id) => post("agent/main", { agent_id }),
  // Rebuilds the agent's models from the saved config (rpc/routes.go:98), as the
  // terminal does after saving a model (internal/ui/model/model_select.go:91-99).
  updateAgent: () => post("agent/update"),

  agentSession: (sid) => get(run(sid)),
  queuedCount: (sid) => get(`${run(sid)}/prompts/queued`),
  queuedSummaries: (sid) => get(`${run(sid)}/prompts/list`, { details: "true" }),
  // Esc posts interrupt every time (TYPE-046). cancel stays for a Stop item in a
  // chat menu, which no phase 3-4 surface has.
  cancel: (sid) => post(`${run(sid)}/cancel`),
  interrupt(sid) {
    tell(interruptWatchers, sid);
    return post(`${run(sid)}/interrupt`);
  },
  clearQueue: (sid) => post(`${run(sid)}/prompts/clear`),
  // The newest editable queued prompt (an AgentMessage), or null. The token
  // stays until acknowledgeRecall, so an answer given up on comes back on the
  // next try. Sent with callId: abort(callId) stops it (1.2.4).
  recallQueued(sid, callId) {
    if (!recallTokens.has(sid)) recallTokens.set(sid, uuid());
    return post(`${run(sid)}/prompts/recall`, undefined, { request_id: recallTokens.get(sid) }, callId);
  },
  // The recalled answer is safely in the composer: the engine may drop its
  // copy. A failed acknowledgement keeps the engine's copy and never turns
  // the answer into an error.
  acknowledgeRecall(sid) {
    const token = recallTokens.get(sid);
    if (!token) return;
    recallTokens.delete(sid);
    post(`${run(sid)}/prompts/recall`, undefined, { request_id: token, acknowledge: "true" }).catch(() => {});
  },
  // Sent with callId = requestId: no time limit, and abort(requestId) stops it (1.2.4).
  runShell: (sid, { command, term_width, is_first_message }, requestId) =>
    post(`${run(sid)}/shell`, { command, term_width, is_first_message }, { request_id: requestId }, requestId),
  // api_abort: closes that call, which stops its work in the engine (a `!`
  // command, TYPE-021; a move with a summary, DLG-100); false when it already ended.
  abort: (callId) => commands.api_abort(callId),

  setConfig: (scope, key, value) => post("config/set", { scope: scopeOf(scope), key, value }),
  removeConfig: (scope, key) => post("config/remove", { scope: scopeOf(scope), key }),
  // model_type: "large" | "small"; model: {provider, model, …} (config.SelectedModel).
  setModel: (scope, model_type, model) => post("config/model", { scope: scopeOf(scope), model_type, model }),
  setPermissionsSkip: (skip) => post("permissions/skip", { skip }),
  // Saved globally, as the terminal does (internal/ui/model/config_write.go:73-80).
  setCompact: (enabled) => post("config/compact", { scope: SCOPE.global, enabled }),
  permissionsSkip: () => get("permissions/skip"),
  // action: "allow" | "allow_session" | "deny" → {resolved}.
  grantPermission: (permission, action) => post("permissions/grant", { permission, action }),

  needsInit: () => get("project/needs-init"),
  initPrompt: () => get("project/init-prompt"),
  initProject: () => post("project/init"),
  readSkill: (skill_id) => post("skills/read", { skill_id }),
  readMcpResource: (name, uri) => post("mcp/read-resource", { name, uri }),
  // getFileLastRead reads both (internal/server/handlers_session.go:182-185).
  lastRead: (sid, path) => get("filetracker/lastread", { session_id: sid, path }),

  // Phase 4 (built here so phase 4 pieces never edit this file). Every tree
  // action resolves to the engine's TreeResult or TreeCopyResult.
  createBranch: (sid, req) => post(`${chat(sid)}/branches`, req),
  chatLinks: () => get("chat-links"),
  tree: (sid) => get(`${chat(sid)}/tree`),
  treeMessage: (sid, mid) => get(`${chat(sid)}/tree/messages/${seg(mid)}`),
  labelTree: (sid, req) => post(`${chat(sid)}/tree/label`, req),
  switchTree: (sid, req) => post(`${chat(sid)}/tree/switch`, req),
  // callId: a move with a summary runs inside the request; abort(callId) stops it (DLG-100).
  navigateTree: (sid, req, callId) => post(`${chat(sid)}/tree/navigate`, req, undefined, callId),
  // req is a TreeCopyRequest, including the optional undo_id (1.5.5).
  copyTree: (sid, req) => post(`${chat(sid)}/tree/copy`, req),
  previewFiles: (sid, req) => post(`${chat(sid)}/files/preview`, req),
  previewUndoFiles: (sid) => post(`${chat(sid)}/files/undo-preview`),
  undoFiles: (sid, plan_id) => post(`${chat(sid)}/files/undo`, { plan_id }),
};

// engine_set_current_session; "" clears it (the landing page).
export function setCurrentSession(sid) {
  return commands.engine_set_current_session(sid);
}
