// The project's config, agent, providers, LSPs, MCPs, skills, modified files
// and Git branch (desktop-design 1.5.3). Each part is read through its route
// and read again on the events that change it and on every resync; a failed
// read keeps what the store had.
import { api } from "../engine/api.js";
import { onEvent, onResync, withHeld } from "../engine/events.js";
import { agentProviderChanged } from "./limits.svelte.js";

export const ws = $state({
  config: null, // pickConfig(GET config)
  agent: null, // AgentInfo
  providers: [], // catwalk providers without keys, endpoints or headers
  lsps: {}, // name → LSPClientInfo plus diagnostics {error, warning, information, hint} (SIDE-009)
  mcps: {}, // name → MCPClientInfo (SIDE-010)
  skills: [], // SkillInfo[] (SIDE-011)
  files: {}, // sid → File[] of the shown chat, as GET …/history sorts them (SIDE-008)
  branch: "",
});

// Keeps models, recent_models, options, permissions, and for each mcp and lsp
// entry only {type, disabled}; drops providers, env, hooks, tools and every
// other field (internal/config/config.go:85-111), so no key or secret header
// sits in a store, the probe or a recording. MCP entries carry headers, env
// and urls (internal/config/mcp.go:37-67), so only the two named fields leave.
export function pickConfig(config) {
  if (!config || typeof config !== "object") return null;
  const out = {};
  for (const key of ["models", "recent_models", "options", "permissions"]) {
    if (config[key] !== undefined && config[key] !== null) out[key] = structuredClone(config[key]);
  }
  for (const key of ["mcp", "lsp"]) {
    const entries = config[key];
    if (!entries || typeof entries !== "object") continue;
    out[key] = {};
    for (const [name, entry] of Object.entries(entries)) {
      const kept = { disabled: entry?.disabled === true };
      if (typeof entry?.type === "string") kept.type = entry.type;
      out[key][name] = kept;
    }
  }
  return out;
}

// A provider as the screens may keep it: the catwalk Provider without its
// api_key, api_endpoint and default_headers (catwalk@v0.52.49
// pkg/catwalk/provider.go), so only names, types and models stay.
export function pickProvider(provider) {
  if (!provider || typeof provider !== "object") return null;
  const { api_key: _key, api_endpoint: _endpoint, default_headers: _headers, ...rest } = provider;
  return structuredClone(rest);
}

export async function loadConfig() {
  ws.config = pickConfig(await api.config());
}

export async function loadAgent() {
  const agent = await api.agentInfo();
  ws.agent = agent ?? null;
  if (agent?.is_ready) agentProviderChanged(agent.model_cfg?.provider ?? "");
}

export async function loadProviders() {
  const list = await api.providers();
  ws.providers = Array.isArray(list) ? list.map(pickProvider).filter(Boolean) : [];
}

export async function loadSkills() {
  const list = await api.skills();
  ws.skills = Array.isArray(list) ? list : [];
}

export async function loadBranch() {
  const answer = await api.gitBranch();
  ws.branch = typeof answer === "string" ? answer : (answer?.branch ?? "");
}

// LSP rows (SIDE-009), as internal/ui/model/lsp.go:45-84: each read takes
// the states and then every server's diagnostics counted by severity (a failed
// count is all zeros, as ClientWorkspace.LSPGetDiagnosticCounts gives,
// internal/workspace/client_workspace.go:509-530). Reads never overlap: an
// event during one runs one more after it.
const SEVERITY = { 1: "error", 2: "warning", 3: "information", 4: "hint" };

async function countDiagnostics(name) {
  const counts = { error: 0, warning: 0, information: 0, hint: 0 };
  let byFile = null;
  try {
    byFile = await api.lspDiagnostics(name);
  } catch {
    return counts;
  }
  for (const list of Object.values(byFile ?? {})) {
    for (const d of Array.isArray(list) ? list : []) {
      const key = SEVERITY[d?.severity];
      if (key) counts[key] += 1;
    }
  }
  return counts;
}

const lspRead = { running: false, again: false };
export async function loadLsps() {
  if (lspRead.running) {
    lspRead.again = true;
    return;
  }
  lspRead.running = true;
  try {
    do {
      lspRead.again = false;
      const states = (await api.lsps()) ?? {};
      const names = Object.keys(states);
      const counts = await Promise.all(names.map(countDiagnostics));
      ws.lsps = Object.fromEntries(names.map((name, i) => [name, { ...states[name], diagnostics: counts[i] }]));
    } while (lspRead.again);
  } finally {
    lspRead.running = false;
  }
}

// MCP rows (SIDE-010); a failed read keeps the old rows.
const mcpRead = { running: false, again: false };
export async function loadMcps() {
  if (mcpRead.running) {
    mcpRead.again = true;
    return;
  }
  mcpRead.running = true;
  try {
    do {
      mcpRead.again = false;
      ws.mcps = (await api.mcpStates()) ?? {};
    } while (mcpRead.again);
  } finally {
    mcpRead.running = false;
  }
}

// The shown chat's file history (SIDE-008 data; lib/modified.js, P3.8,
// counts it). Sorted as the engine lists it: version, then created_at
// (internal/db/sql/files.sql:13-17).
function byVersion(a, b) {
  return (a.version ?? 0) - (b.version ?? 0) || (a.created_at ?? 0) - (b.created_at ?? 0);
}

export async function loadFiles(sid) {
  const list = await api.history(sid);
  ws.files[sid] = (Array.isArray(list) ? list : []).slice().sort(byVersion);
  return ws.files[sid];
}

export function setFiles(sid, list) {
  ws.files[sid] = (Array.isArray(list) ? list : []).slice().sort(byVersion);
}

export function dropFiles(sid) {
  delete ws.files[sid];
}

// A `file` event updates the list of a chat whose history is held, by id.
onEvent("file", (kind, file) => {
  const list = file?.session_id ? ws.files[file.session_id] : null;
  if (!list || !file.id) return;
  const rest = list.filter((f) => f.id !== file.id);
  ws.files[file.session_id] = kind === "deleted" ? rest : [...rest, file].sort(byVersion);
});

function quietly(promise, what) {
  return Promise.resolve(promise).catch((error) => console.error(`${what} read failed`, error));
}

onEvent("config_changed", () => {
  quietly(loadConfig(), "config");
  quietly(loadAgent(), "agent");
});
onEvent("lsp_event", () => quietly(loadLsps(), "LSP"));
onEvent("mcp_event", () => quietly(loadMcps(), "MCP"));
onEvent("skills_event", () => quietly(loadSkills(), "skills"));

// The shown chat id, given by the sessions store at start (no import, so the
// two stores never load each other).
let shownChat = () => "";

// The events that change what these reads give wait until the reads land
// (1.5.4), so a read the engine answered before an event never overwrites it,
// and the read an event starts lands after the resync's own. LSP and MCP
// reads never overlap anyway (loadLsps, loadMcps).
onResync(() =>
  withHeld(["file", "skills_event", "config_changed"], async () => {
    const sid = shownChat();
    await Promise.all([
      quietly(loadConfig(), "config"),
      quietly(loadAgent(), "agent"),
      quietly(loadLsps(), "LSP"),
      quietly(loadMcps(), "MCP"),
      quietly(loadSkills(), "skills"),
      sid ? quietly(loadFiles(sid), "files") : null,
    ]);
  }),
);

// The workspace reads of START-007 and the two timers: the LSP backstop
// (5 s, lsp.go:19-26, which covers events lost across a reconnect) and the
// Git branch, read every 5 s while the window is visible (SIDE-013). Both
// read only while connected(), which the connection store gives.
const timers = [];
export function startWorkspace({ currentChat = () => "", connected = () => true } = {}) {
  shownChat = currentChat;
  stopWorkspace();
  timers.push(
    setInterval(() => {
      if (connected()) quietly(loadLsps(), "LSP");
    }, 5000),
  );
  timers.push(
    setInterval(() => {
      if (connected() && globalThis.document?.visibilityState !== "hidden") quietly(loadBranch(), "Git branch");
    }, 5000),
  );
  return Promise.all([
    quietly(loadConfig(), "config"),
    quietly(loadAgent(), "agent"),
    quietly(loadProviders(), "providers"),
    quietly(loadLsps(), "LSP"),
    quietly(loadMcps(), "MCP"),
    quietly(loadSkills(), "skills"),
    quietly(loadBranch(), "Git branch"),
  ]);
}

export function stopWorkspace() {
  for (const timer of timers.splice(0)) clearInterval(timer);
}
