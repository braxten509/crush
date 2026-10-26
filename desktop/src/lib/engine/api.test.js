import { beforeEach, describe, expect, it, vi } from "vitest";

// Every route function goes through commands.api (one Tauri command); the mock
// records what each sends.
vi.mock("./commands.js", () => ({
  api: vi.fn(async () => null),
  api_abort: vi.fn(async () => true),
  engine_set_current_session: vi.fn(async () => null),
}));

// The queue store (TYPE-019) loads the event loop, which asks for the window.
vi.mock("@tauri-apps/api/webviewWindow", () => ({ getCurrentWebviewWindow: () => ({ listen: async () => () => {} }) }));

const commands = await import("./commands.js");
const { api, onInterrupt, onSend, setChatFolder, setCurrentSession, uuid } = await import("./api.js");

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

beforeEach(() => {
  setChatFolder(() => null);
  commands.api.mockReset();
  commands.api.mockImplementation(async () => null);
  commands.api_abort.mockClear();
  commands.engine_set_current_session.mockClear();
});

// [method, path, query, body, callId, dir] of the last call
function last() {
  const call = commands.api.mock.calls.at(-1);
  return { method: call[0], path: call[1], query: call[2], body: call[3], callId: call[4], dir: call[5] };
}

describe("paths and bodies (desktop-design 1.5.2, the routes of 1.3)", () => {
  const sid = "S1";
  const cases = [
    ["version", [], "GET", "/v1/version"],
    ["engine", [], "GET", "/v1/engine"],
    ["limits", [], "GET", "/v1/limits"],
    ["refreshLimits", ["claude-code"], "POST", "/v1/limits/claude-code/refresh"],
    ["config", [], "GET", "config"],
    ["providers", [], "GET", "providers"],
    ["agentInfo", [], "GET", "agent"],
    ["gitBranch", [], "GET", "git/branch"],
    ["lsps", [], "GET", "lsps"],
    ["lspDiagnostics", ["gopls"], "GET", "lsps/gopls/diagnostics"],
    ["mcpStates", [], "GET", "mcp/states"],
    ["skills", [], "GET", "skills"],
    ["allUserMessages", [], "GET", "messages/user"],
    ["promptHistory", [], "GET", "/v1/prompt-history"],
    ["addPromptHistory", ["hi"], "POST", "/v1/prompt-history", undefined, { text: "hi" }],
    ["listSessions", [], "GET", "sessions"],
    ["createSession", [{ title: "New Session" }], "POST", "sessions", undefined, { title: "New Session" }],
    ["getSession", [sid], "GET", "sessions/S1"],
    ["saveSession", [{ id: sid, title: "T" }], "PUT", "sessions/S1", undefined, { id: sid, title: "T" }],
    ["deleteSession", [sid], "DELETE", "sessions/S1"],
    ["messages", [sid], "GET", "sessions/S1/messages"],
    ["userMessages", [sid], "GET", "sessions/S1/messages/user"],
    ["review", [sid, "M1"], "GET", "sessions/S1/messages/M1/review"],
    ["history", [sid], "GET", "sessions/S1/history"],
    ["trackedFiles", [sid], "GET", "sessions/S1/filetracker/files"],
    ["setKeptSkill", [sid, { id: "k", name: "K", on: true, toggleable: false, extra: 1 }], "POST", "sessions/S1/kept-skills",
      undefined, { id: "k", name: "K", on: true, toggleable: false }],
    ["setMainAgent", ["plan"], "POST", "agent/main", undefined, { agent_id: "plan" }],
    ["updateAgent", [], "POST", "agent/update"],
    ["agentSession", [sid], "GET", "agent/sessions/S1"],
    ["queuedCount", [sid], "GET", "agent/sessions/S1/prompts/queued"],
    ["queuedSummaries", [sid], "GET", "agent/sessions/S1/prompts/list", { details: "true" }],
    ["cancel", [sid], "POST", "agent/sessions/S1/cancel"],
    ["interrupt", [sid], "POST", "agent/sessions/S1/interrupt"],
    ["clearQueue", [sid], "POST", "agent/sessions/S1/prompts/clear"],
    ["setConfig", ["global", "options.tui.compact_mode", true], "POST", "config/set", undefined,
      { scope: 0, key: "options.tui.compact_mode", value: true }],
    ["setConfig", ["workspace", "k", 1], "POST", "config/set", undefined, { scope: 1, key: "k", value: 1 }],
    ["removeConfig", [1, "k"], "POST", "config/remove", undefined, { scope: 1, key: "k" }],
    ["setHelperModel", [{ provider: "p", model: "m" }], "POST", "config/model", undefined,
      { scope: 0, model_type: "small", model: { provider: "p", model: "m" } }],
    ["lastPick", [], "GET", "last-pick", undefined, undefined],
    ["setLastPick", [{ provider: "p", model: "m" }], "POST", "last-pick", undefined, { provider: "p", model: "m" }],
    ["setPermissionsSkip", [true], "POST", "permissions/skip", undefined, { skip: true }],
    ["setCompact", [true], "POST", "config/compact", undefined, { scope: 0, enabled: true }],
    ["permissionsSkip", [], "GET", "permissions/skip"],
    ["grantPermission", [{ id: "p1" }, "allow_session"], "POST", "permissions/grant", undefined,
      { permission: { id: "p1" }, action: "allow_session" }],
    ["needsInit", [], "GET", "project/needs-init"],
    ["initPrompt", [], "GET", "project/init-prompt"],
    ["initProject", [], "POST", "project/init"],
    ["readSkill", ["s1"], "POST", "skills/read", undefined, { skill_id: "s1" }],
    ["readMcpResource", ["fake", "file:///a"], "POST", "mcp/read-resource", undefined, { name: "fake", uri: "file:///a" }],
    ["lastRead", [sid, "auth/session.go"], "GET", "filetracker/lastread", { session_id: sid, path: "auth/session.go" }],
    ["createBranch", [sid, { message_id: "M1" }], "POST", "sessions/S1/branches", undefined, { message_id: "M1" }],
    ["chatLinks", [], "GET", "chat-links"],
    ["tree", [sid], "GET", "sessions/S1/tree"],
    ["treeMessage", [sid, "M1"], "GET", "sessions/S1/tree/messages/M1"],
    ["labelTree", [sid, { expected_revision: 3 }], "POST", "sessions/S1/tree/label", undefined, { expected_revision: 3 }],
    ["switchTree", [sid, { target: "M1" }], "POST", "sessions/S1/tree/switch", undefined, { target: "M1" }],
    ["copyTree", [sid, { target: "M1", fork: true, undo_id: "u1" }], "POST", "sessions/S1/tree/copy", undefined,
      { target: "M1", fork: true, undo_id: "u1" }],
    ["previewFiles", [sid, { target: "M1", fork: false }], "POST", "sessions/S1/files/preview", undefined, { target: "M1", fork: false }],
    ["previewUndoFiles", [sid], "POST", "sessions/S1/files/undo-preview"],
    ["undoFiles", [sid, "plan-1"], "POST", "sessions/S1/files/undo", undefined, { plan_id: "plan-1" }],
  ];
  for (const [name, args, method, path, query, body] of cases) {
    it(`${name}(${args.map((a) => JSON.stringify(a)).join(", ")}) → ${method} ${path}`, async () => {
      await api[name](...args);
      expect(last()).toEqual({ method, path, query, body, callId: undefined, dir: "" });
    });
  }

  it("lastRead names the chat and the file (getFileLastRead reads both)", async () => {
    await api.lastRead("chat-7", "README.md");
    expect(last().query).toEqual({ session_id: "chat-7", path: "README.md" });
  });

  it("puts ids into one path segment each, as they are: Rust encodes every part (connection.rs Route::target)", async () => {
    // An agent tool's chat id keeps its "$$" (internal/session/session.go:545-547).
    await api.messages("m1$$call_1");
    expect(last().path).toBe("sessions/m1$$call_1/messages");
    await api.messages("x?y#z %");
    expect(last().path).toBe("sessions/x?y#z %/messages");
    await api.getSession("é😀");
    expect(last().path).toBe("sessions/é😀");
    await api.lspDiagnostics("my lsp");
    expect(last().path).toBe("lsps/my lsp/diagnostics");
    // A "/" would make several parts (here GET sessions/{sid}/messages): it
    // goes as "..", which Rust refuses.
    await api.getSession("x/messages");
    expect(last().path).toBe("sessions/..");
    await api.messages("a/../b");
    expect(last().path).toBe("sessions/../messages");
  });

  it("answers with the engine's JSON", async () => {
    commands.api.mockResolvedValueOnce([{ id: "S1" }]);
    expect(await api.listSessions()).toEqual([{ id: "S1" }]);
  });
});

describe("send", () => {
  it("adds a fresh run_id and keeps the message's fields", async () => {
    await api.send({ session_id: "S1", prompt: "hi", submission_id: "sub1", attachments: [] });
    const { method, path, body } = last();
    expect([method, path]).toEqual(["POST", "agent"]);
    expect(body).toMatchObject({ session_id: "S1", prompt: "hi", submission_id: "sub1", attachments: [] });
    expect(body.run_id).toMatch(UUID);
    await api.send({ session_id: "S1", prompt: "again" });
    expect(last().body.run_id).toMatch(UUID);
    expect(last().body.run_id).not.toBe(body.run_id);
  });

  it("keeps a run_id the caller made", async () => {
    await api.send({ session_id: "S1", prompt: "hi", run_id: "run-1", hidden_user_message: true, channel: "c" });
    expect(last().body).toMatchObject({ run_id: "run-1", hidden_user_message: true, channel: "c" });
  });

  it("tells the send watchers the body and the call", async () => {
    const seen = [];
    const off = onSend((body, call) => seen.push([body.run_id, call]));
    const call = api.send({ session_id: "S1", prompt: "x", run_id: "r9" });
    expect(seen).toEqual([["r9", call]]);
    off();
    await api.send({ session_id: "S1", prompt: "y" });
    expect(seen).toHaveLength(1);
  });
});

describe("interrupt", () => {
  it("tells the interrupt watchers before it posts", async () => {
    const order = [];
    commands.api.mockImplementation(async (method, path) => order.push(`${method} ${path}`));
    const off = onInterrupt((sid) => order.push(`watch ${sid}`));
    await api.interrupt("S1");
    off();
    expect(order).toEqual(["watch S1", "POST agent/sessions/S1/interrupt"]);
  });
});

describe("recallQueued (the route requires request_id: internal/server/handlers_agent.go:110-130)", () => {
  it("sends a token, then acknowledges it once the prompt is delivered", async () => {
    commands.api.mockImplementation(async (method, path, query) => (query?.acknowledge ? null : { prompt: "queued text" }));
    expect(await api.recallQueued("S1", "call1")).toEqual({ prompt: "queued text" });
    expect(commands.api.mock.calls).toHaveLength(1);
    expect(commands.api.mock.calls[0][4]).toBe("call1");
    api.acknowledgeRecall("S1");
    const [recall, ack] = commands.api.mock.calls;
    expect(recall.slice(0, 3)).toEqual(["POST", "agent/sessions/S1/prompts/recall", { request_id: expect.stringMatching(UUID) }]);
    expect(ack.slice(0, 3)).toEqual(["POST", "agent/sessions/S1/prompts/recall", { request_id: recall[2].request_id, acknowledge: "true" }]);
  });

  it("retries with the same token after a lost answer, and uses a new one afterwards", async () => {
    commands.api.mockRejectedValueOnce({ code: "transport", message: "broken" });
    await expect(api.recallQueued("S2")).rejects.toMatchObject({ code: "transport" });
    const first = commands.api.mock.calls[0][2].request_id;
    commands.api.mockImplementation(async () => ({ prompt: "p" }));
    await api.recallQueued("S2");
    expect(commands.api.mock.calls[1][2].request_id).toBe(first);
    api.acknowledgeRecall("S2");
    await api.recallQueued("S2");
    expect(commands.api.mock.calls.at(-1)[2].request_id).not.toBe(first);
  });

  it("never turns a failed acknowledgement into an error", async () => {
    commands.api.mockImplementation(async (method, path, query) => {
      if (query?.acknowledge) throw { code: "transport" };
      return { prompt: "p" };
    });
    await expect(api.recallQueued("S3")).resolves.toEqual({ prompt: "p" });
    expect(() => api.acknowledgeRecall("S3")).not.toThrow();
    await Promise.resolve();
  });
});

describe("a recall that outlives its 10 s (TYPE-019)", () => {
  it("timeout, then a late answer, then a retry: the prompt and its attachments come back", async () => {
    // The engine of internal/backend/agent.go:313-348: a token's first recall
    // withdraws the prompt and keeps it; the same token gets it again until
    // acknowledged, and a new token finds the queue empty.
    const waiting = [{ prompt: "held text", attachments: [{ file_name: "a.txt", mime_type: "text/plain", content: "aGk=" }], submission_id: "sub1" }];
    const kept = new Map();
    let late = null;
    commands.api.mockImplementation(async (method, path, query) => {
      if (!path.endsWith("/prompts/recall")) return null;
      if (query.acknowledge) {
        kept.set(query.request_id, null);
        return null;
      }
      if (!kept.has(query.request_id)) kept.set(query.request_id, waiting.shift() ?? null);
      if (kept.size === 1 && !late) return new Promise((resolve) => (late = () => resolve(kept.get(query.request_id))));
      return kept.get(query.request_id);
    });
    commands.api_abort.mockImplementation(async () => false); // the answer was already on its way
    const { recall } = await import("../stores/queue.svelte.js");
    vi.useFakeTimers();
    try {
      const first = recall("SQ");
      const failed = expect(first).rejects.toMatchObject({ code: "timeout" });
      await vi.advanceTimersByTimeAsync(10_000);
      await failed;
      late();
      await vi.advanceTimersByTimeAsync(0);
      const again = await recall("SQ");
      expect(again).toMatchObject({ text: "held text", submissionId: "sub1" });
      expect(again.attachments).toEqual([{ kind: expect.any(String), name: "a.txt", mime: "text/plain", path: "", base64: "aGk=" }]);
    } finally {
      vi.useRealTimers();
    }
  });
});

describe("calls that run inside their request (1.2.4)", () => {
  it("runShell goes with callId = requestId and request_id in the query", async () => {
    await api.runShell("S1", { command: "ls", term_width: 100, is_first_message: true }, "req-1");
    expect(last()).toEqual({
      method: "POST",
      path: "agent/sessions/S1/shell",
      query: { request_id: "req-1" },
      body: { command: "ls", term_width: 100, is_first_message: true },
      callId: "req-1",
      dir: "",
    });
  });

  it("navigateTree passes its callId (a move with a summary, DLG-100)", async () => {
    await api.navigateTree("S1", { expected_revision: 2, target: "M1", summarize: true }, "nav-1");
    expect(last()).toMatchObject({ path: "sessions/S1/tree/navigate", callId: "nav-1", body: { summarize: true } });
    await api.navigateTree("S1", { expected_revision: 3, target: "M2" });
    expect(last().callId).toBeUndefined();
  });

  it("abort is api_abort with that callId", async () => {
    commands.api_abort.mockResolvedValueOnce(false);
    expect(await api.abort("req-1")).toBe(false);
    expect(commands.api_abort).toHaveBeenCalledWith("req-1");
  });
});

describe("setCurrentSession and uuid", () => {
  it("setCurrentSession is engine_set_current_session", async () => {
    await setCurrentSession("");
    expect(commands.engine_set_current_session).toHaveBeenCalledWith("");
  });

  it("uuid makes v4 UUIDs, also without crypto.randomUUID", () => {
    expect(uuid()).toMatch(UUID);
    const real = globalThis.crypto.randomUUID;
    try {
      Object.defineProperty(globalThis.crypto, "randomUUID", { value: undefined, configurable: true, writable: true });
      const ids = new Set(Array.from({ length: 50 }, uuid));
      expect(ids.size).toBe(50);
      for (const id of ids) expect(id).toMatch(UUID);
    } finally {
      Object.defineProperty(globalThis.crypto, "randomUUID", { value: real, configurable: true, writable: true });
    }
  });
});

describe("a chat's calls go to that chat's folder (owner, 2026-10-22: several folders at once)", () => {
  const here = { dir: "/tmp/home/proj", joined: true };
  const other = { dir: "/tmp/home/other", joined: true };
  const folders = { S1: here, S2: other, M1: other };

  beforeEach(() => {
    setChatFolder((sid) => folders[sid] ?? null);
  });

  it("checks provider availability in the chat's folder", async () => {
    await api.providerAvailability("S2");
    expect(last()).toMatchObject({ method: "GET", path: "providers/availability", dir: other.dir });
    await api.providerAvailability();
    expect(last().dir).toBe("");
  });

  it("a chat route carries its chat's folder", async () => {
    await api.messages("S2");
    expect(last()).toMatchObject({ method: "GET", path: "sessions/S2/messages", dir: "/tmp/home/other" });
    await api.interrupt("S2");
    expect(last()).toMatchObject({ method: "POST", path: "agent/sessions/S2/interrupt", dir: "/tmp/home/other" });
    await api.send({ session_id: "S1", prompt: "hi" });
    expect(last()).toMatchObject({ method: "POST", path: "agent", dir: "/tmp/home/proj" });
    await api.saveSession({ id: "S2", title: "T" });
    expect(last()).toMatchObject({ method: "PUT", path: "sessions/S2", dir: "/tmp/home/other" });
    await api.deleteSession("S2");
    expect(last()).toMatchObject({ method: "DELETE", path: "sessions/S2", dir: "/tmp/home/other" });
  });

  it("a chat's sub-agent chat goes to the folder of the chat it belongs to", async () => {
    setChatFolder((sid) => (sid === "A1" ? other : null));
    await api.agentSession("A1");
    expect(last()).toMatchObject({ path: "agent/sessions/A1", dir: "/tmp/home/other" });
  });

  it("a folder-level call carries no folder: it is about the folder the window shows", async () => {
    await api.config();
    expect(last()).toMatchObject({ path: "config", dir: "" });
    await api.limits();
    expect(last().dir).toBe("");
  });

  it("a share's calls carry the chat's folder", async () => {
    await api.startRemote({ session_id: "S2" });
    expect(last()).toMatchObject({ path: "remote/start", dir: "/tmp/home/other" });
    await api.stopRemote("S2");
    expect(last()).toMatchObject({ path: "remote/stop", body: { session_id: "S2" }, dir: "/tmp/home/other" });
    // A window's own new chat ("" session) belongs to the folder it shows.
    await api.startRemote({});
    expect(last().dir).toBe("");
  });

  it("a chat of a folder the window has not joined is read from that folder's saved files", async () => {
    setChatFolder(() => ({ dir: "/tmp/home/gone", joined: false }));
    await api.getSession("S9");
    // Saved files are read through the folder the window shows, which is
    // joined; the chat's own folder need not be.
    expect(last()).toMatchObject({ method: "GET", path: "saved-sessions/S9", query: { dir: "/tmp/home/gone" }, dir: "" });
    await api.messages("S9", { last: 20 });
    expect(last()).toMatchObject({ path: "saved-sessions/S9/messages", query: { last: "20", dir: "/tmp/home/gone" } });
    // A joined folder answers through its own engine.
    setChatFolder(() => other);
    await api.getSession("S9");
    expect(last()).toMatchObject({ path: "sessions/S9", dir: "/tmp/home/other" });
  });
});

// The engine reads the memory limit as typed text (proto.MemoryLimitRequest
// Value string); a JSON number fails with "failed to decode request".
it("sends the memory limit as text", async () => {
  await api.setMemoryLimit(29);
  expect(last()).toMatchObject({ method: "PUT", path: "/v1/memory/limit", body: { value: "29" } });
  await api.setMemoryLimit(0);
  expect(last().body).toEqual({ value: "0" });
});
