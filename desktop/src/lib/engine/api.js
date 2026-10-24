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

// A chat of another folder (owner, 2026-10-22: several folders are open at
// once; every listed chat stays drawn) is read from that folder's saved files
// only while this window does not keep that folder joined; a joined folder's
// chats are read and changed through its own engine. The open chats store
// tells each chat's folder and whether it is joined.
let chatFolder = () => null;
export function setChatFolder(fn) {
  chatFolder = fn;
}

// The folder a chat's calls go to ("" for the shown folder).
const folderOf = (sid) => (sid ? (chatFolder(sid)?.dir ?? "") : "");
// That folder's saved files, for a folder this window has not joined.
const unjoined = (sid) => {
  const folder = chatFolder(sid);
  return folder?.dir && !folder.joined ? folder.dir : "";
};

// The chat a call is about, from its path, query or body: every call about a
// chat goes to that chat's folder's engine. A saved-sessions read stays on the
// folder the window shows: that engine reads any folder's files, and the chat's
// own folder may not be joined at all.
function chatOf(path, query, body) {
  const parts = String(path ?? "").split("/");
  const at = (n) => (parts.length > n && parts[n] ? parts[n] : "");
  if (parts[0] === "sessions") return at(1);
  if (parts[0] === "agent" && parts[1] === "sessions") return at(2);
  if (parts[0] === "saved-sessions") return "";
  return String(body?.session_id ?? query?.session_id ?? "");
}

const get = (path, query) => commands.api("GET", path, query, undefined, undefined, folderOf(chatOf(path, query)));
const post = (path, body, query, callId) =>
  commands.api("POST", path, query, body, callId, folderOf(chatOf(path, query, body)));
const saved = (sid) => `saved-sessions/${seg(sid)}`;

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
  // Use the chat's folder when a chat is shown (rpc.ProviderUsable).
  providerAvailability: (sid) => commands.api("GET", "providers/availability", undefined, undefined, undefined, folderOf(sid)),
  lsps: () => get("lsps"),
  // One server's diagnostics by file (rpc/routes.go:79), counted per severity for SIDE-009.
  lspDiagnostics: (name) => get(`lsps/${seg(name)}/diagnostics`),
  mcpStates: () => get("mcp/states"),
  skills: () => get("skills"),
  allUserMessages: () => get("messages/user"),

  listSessions: () => get("sessions"),
  createSession: (session) => post("sessions", session),
  getSession: (sid) => {
    const dir = unjoined(sid);
    return dir ? get(saved(sid), { dir }) : get(chat(sid));
  },
  // The model one chat runs on (internal/rpc/routes.go SetSessionModel): a
  // model picked in a chat never reaches another chat.
  setSessionModel: (sid, model) => commands.api("POST", `${chat(sid)}/model`, undefined, { model }, undefined, folderOf(sid)),
  // The whole record as last read (engine design 5.1(a)).
  saveSession: (session) => commands.api("PUT", chat(session.id), undefined, session, undefined, folderOf(session.id)),
  deleteSession: (sid) => commands.api("DELETE", chat(sid), undefined, undefined, undefined, folderOf(sid)),
  // `last` reads about the newest N, `before` only those older than a message
  // (internal/server/handlers_session.go pageMessages).
  messages: (sid, { last = 0, before = "" } = {}) => {
    const query = {};
    if (last > 0) query.last = String(last);
    if (before) query.before = before;
    const dir = unjoined(sid);
    if (dir) return get(`${saved(sid)}/messages`, { ...query, dir });
    return get(`${chat(sid)}/messages`, Object.keys(query).length ? query : undefined);
  },
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
  // OpenRouter's catalog, the account's preferences applied (the model menu's "+").
  openRouterCatalog: () => get("openrouter/catalog"),
  removeConfig: (scope, key) => post("config/remove", { scope: scopeOf(scope), key }),
  // model_type: "large" | "small"; model: {provider, model, …} (config.SelectedModel).
  setModel: (scope, model_type, model) => post("config/model", { scope: scopeOf(scope), model_type, model }),
  // Accounts and the fallback order (internal/server/endpoints_fallback.go).
  fallback: () => get("fallback"),
  setFallbackOrder: (order) => commands.api("PUT", "fallback/order", undefined, order),
  addAccount: (kind) => post("accounts", { kind }),
  renameAccount: (id, name) => commands.api("PUT", `accounts/${seg(id)}`, undefined, { name }),
  removeAccount: (id) => commands.api("DELETE", `accounts/${seg(id)}`),
  signInAccount: (id) => post(`accounts/${seg(id)}/login`),
  useRefill: (kind, account) => post("/v1/limits/refill", { kind, account }),
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

  // Phases 5 and 6 (desktop-design-5-6 6.5; scaffold S5). The G-n routes are G56's.
  // A secret (a key, a secure form's answers) goes only through apiSecret:
  // secure-forms/{fid}/submit, config/provider-key and auth/verify-key.
  apiSecret: (path, body) => commands.apiSecret(path, body),

  customCommands: () => get("commands"),
  mcpPrompts: () => get("mcp/prompts"),
  getMcpPrompt: ({ client_id, prompt_id, args }) => post("mcp/get-prompt", { client_id, prompt_id, args }),
  // Runs inside the request: no time limit, and abort(callId) stops it.
  summarize: (sid, callId) => post(`${run(sid)}/summarize`, undefined, undefined, callId),
  capabilities: () => get("agent/capabilities"),
  refreshOAuth: (scope, provider_id) => post("config/refresh-oauth", { scope: scopeOf(scope), provider_id }),
  importCopilot: () => post("config/import-copilot"),
  initAgent: () => post("agent/init", { interactive: true }),
  refreshProviders: () => post("/v1/providers/refresh"),

  startAuthFlow: ({ platform }) => post("auth/flows", { platform }),
  sendAuthCode: (flow_id, code) => post(`auth/flows/${seg(flow_id)}/code`, { code }),
  cancelAuthFlow: (flow_id) => post(`auth/flows/${seg(flow_id)}/cancel`),

  // [session.Session] with PascalCase keys; selected: the shown chat's id.
  // folders: also the chats of that many most recently used folders.
  savedSessions: (selected, folders = 0) => {
    const query = { ...(selected ? { selected } : {}), ...(folders > 0 ? { folders: String(folders) } : {}) };
    return get("saved-sessions", Object.keys(query).length ? query : undefined);
  },
  // session.CatalogChange {DiscardEmpty, Session, Remove}.
  changeSavedSession: (change) => post("saved-sessions", change),

  answerQuestions: (answer) => post("questions/answer", answer),
  cancelQuestions: () => post("questions/cancel"),
  pendingQuestion: (sid) => get("questions/pending", { session_id: sid }),
  secureForms: () => get("secure-forms"),
  cancelSecureForm: (form_id) => post(`secure-forms/${seg(form_id)}/cancel`),

  installedSkills: () => get("skills/installed"),
  // callId: abort(callId) stops the work (the engine uses the request's context).
  manageSkill: (request, callId) => post("skills/manage", request, undefined, callId),
  searchSkills: (q, callId) => commands.api("GET", "skills/search", { q }, undefined, callId),
  mcpDisabled: () => get("mcp/disabled"),
  setMcpDisabled: ({ name, disabled }) => post("mcp/disabled", { name, disabled }),
  mcpEnabled: () => get("mcp/enabled"),
  setMcpConfigDisabled: ({ name, disabled }) => post("mcp/config-disabled", { name, disabled }),
  mcpPendingAuth: () => get("mcp/pending-auth"),
  mcpAuthUrl: (name) => get("mcp/auth-url", { name }),
  // Runs inside the request: no time limit, and abort(callId) stops it.
  mcpAuth: (name, callId) => post("mcp/auth", { name }, undefined, callId),
  dockerMcp: (enable) => post(enable ? "mcp/docker/enable" : "mcp/docker/disable"),

  memory: () => get("/v1/memory"),
  setMemoryLimit: (value) => commands.api("PUT", "/v1/memory/limit", undefined, { value }),
  // The engine claims its current episode (internal/server/handlers_live.go:94);
  // the caller names the episode it saw.
  claimMemoryNotice: (_episode) => post("/v1/memory/claim-notice"),
  setCpuAffinity: (enabled) => post("/v1/cpu-affinity", { enabled }),

  themes: () => get("/v1/themes"),
  theme: (name) => get(`/v1/themes/${seg(name)}`),
  resolveTheme: ({ base, palette }) => post("/v1/themes/resolve", { base, palette }),
  saveTheme: (name, { base, palette }) => commands.api("PUT", `/v1/themes/${seg(name)}`, undefined, { base, palette }),
  createTheme: ({ name, base }) => post("/v1/themes", { name, base }),
  renameTheme: (name, new_name) => post(`/v1/themes/${seg(name)}/rename`, { new_name }),
  deleteTheme: (name) => commands.api("DELETE", `/v1/themes/${seg(name)}`),
  revertTheme: (name) => post(`/v1/themes/${seg(name)}/revert`),

  helpers: (sid) => get(`${chat(sid)}/helpers`),
  stopHelper: (sid, hid) => post(`${chat(sid)}/helpers/${seg(hid)}/stop`),
  jobs: (sid) => get(`${chat(sid)}/jobs`),
  // ref: a JobRef {job_id?, pid?}.
  killJob: (sid, ref) => post(`${chat(sid)}/jobs/kill`, ref),
  background: (sid) => post(`${chat(sid)}/background`),
  servers: () => get("servers"),

  // One share per chat (owner, 2026-10-21); session_id "" is this window's
  // new chat.
  startRemote: ({ session_id = "", launch_id } = {}) => post("remote/start", launch_id ? { session_id, launch_id } : { session_id }),
  stopRemote: (sessionId = "") => post("remote/stop", { session_id: sessionId }),
  remoteShares: () => get("remote"),
  remotePresence: (presence) => post("remote/presence", presence),
  remoteActionDone: (aid, done) => post(`remote/actions/${seg(aid)}/done`, done),

  cliUpdates: (manual = false) => get("/v1/updates/clis", { manual: String(manual) }),
  declineCliUpdates: (bins) => post("/v1/updates/clis/decline", { bins }),
  installCliUpdates: (bins) => post("/v1/updates/clis/install", { bins }),
  crushUpdate: (manual = false) => get("/v1/updates/crush", { manual: String(manual) }),
  declineCrushUpdate: (tag) => post("/v1/updates/crush/decline", { tag }),
};

// engine_set_current_session; "" clears it (the landing page).
export function setCurrentSession(sid) {
  return commands.engine_set_current_session(sid);
}
