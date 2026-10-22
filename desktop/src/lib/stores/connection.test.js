import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The real event loop (events.js) with fake Tauri calls (see prompts.test.js),
// plus app_info, engine_open and engine_retry.
const h = vi.hoisted(() => ({
  routes: new Map(),
  calls: [],
  entries: [],
  ring: null,
  current: [],
  notices: [],
  info: null,
  open: null,
  retries: 0,
  storage: {},
  opened: [],
  shown: [],
}));

vi.mock("@tauri-apps/api/webviewWindow", () => ({
  getCurrentWebviewWindow: () => ({
    listen: async (_name, fn) => {
      h.ring = fn;
      return () => {};
    },
  }),
}));

vi.mock("../engine/commands.js", () => ({
  api: async (method, path, query, body, callId, dir) => {
    h.calls.push({ method, path, query, body, callId, dir });
    const route = h.routes.get(`${method} ${path}`);
    if (typeof route === "function") return route({ query, body, callId });
    if (route !== undefined) return route;
    if (method === "GET") return Promise.reject({ code: "not_found", message: `no answer for GET ${path}` });
    return null;
  },
  api_abort: async () => true,
  engine_set_current_session: async (sid) => {
    h.current.push(sid);
    return null;
  },
  events_take: async () => h.entries.splice(0),
  app_info: async () => h.info,
  engine_open: async (cwd) => h.open(cwd),
  engine_open_folder: async (cwd) => {
    h.opened.push(cwd);
    return opened({ cwd, client_id: `client-${cwd}`, workspace: { id: `ws-${cwd}`, path: cwd } });
  },
  engine_show_folder: async (cwd) => {
    h.shown.push(cwd);
    return opened({ cwd, client_id: `client-${cwd}`, workspace: { id: `ws-${cwd}`, path: cwd } });
  },
  engine_retry: async () => {
    h.retries += 1;
    return null;
  },
  lastClipboardWrite: () => null,
}));

const tick = () => new Promise((resolve) => setTimeout(resolve, 0));
async function settle(rounds = 10) {
  for (let i = 0; i < rounds; i += 1) await tick();
}
async function deliver(...entries) {
  h.entries.push(...entries);
  h.ring?.();
  await settle();
}
const ev = (type, event, payload) => ({ kind: "event", type, event, payload });

let C;
let S;
let W;
let L;
let probe;

const workspace = (extra = {}) => ({ id: "ws-1", path: "/tmp/home/proj", yolo: true, data_dir: "/tmp/home/proj/.crush", version: "v0.70.0", ...extra });
const opened = (extra = {}) => ({
  client_id: "client-1",
  workspace: workspace(extra.workspace),
  engine: { address: "unix:///run/crush.sock", socket: "/run/crush.sock", protocol: 1, version: "v0.70.0", pid: 42, data_dir: "/tmp/home/proj/.crush", started: true },
  version: { version: "v0.70.0", protocol: 1 },
  sent_yolo: true,
  cwd: "/tmp/home/proj",
  ...extra,
  ...(extra.workspace ? { workspace: workspace(extra.workspace) } : {}),
});

beforeEach(async () => {
  vi.resetModules();
  h.routes.clear();
  h.calls.length = 0;
  h.entries.length = 0;
  h.ring = null;
  h.current.length = 0;
  h.notices.length = 0;
  h.retries = 0;
  h.storage = {};
  h.opened.length = 0;
  h.shown.length = 0;
  h.info = { version: "0.1.0", probe: false, fixture: false, cwd: "/tmp/home/proj", session: "", continue_last: false, ui_debug: false, platform: "linux" };
  h.open = async () => opened();
  vi.stubGlobal("requestAnimationFrame", (fn) => setTimeout(() => fn(0), 0));
  vi.stubGlobal("localStorage", {
    getItem: (key) => h.storage[key] ?? null,
    setItem: (key, value) => {
      h.storage[key] = value;
    },
    addEventListener: () => {},
  });
  vi.doMock("./ui.svelte.js", async (importOriginal) => {
    const real = await importOriginal();
    return {
      ...real,
      notify: (kind, text, ...rest) => {
        h.notices.push({ kind, text });
        return real.notify(kind, text, ...rest);
      },
    };
  });
  const events = await import("../engine/events.js");
  C = await import("./connection.svelte.js");
  S = await import("./sessions.svelte.js");
  W = await import("./workspace.svelte.js");
  L = await import("./limits.svelte.js");
  probe = await import("../probe.js");
  await events.startEvents();
  await settle();
});

afterEach(() => {
  W.stopWorkspace();
  L.stopLimitsPoll();
  vi.unstubAllGlobals();
});

describe("joining (1.2.6, 2.9)", () => {
  it("app_info, then engine_open(cwd), then the stores load (START-007)", async () => {
    let asked = "";
    h.open = async (cwd) => {
      asked = cwd;
      return opened();
    };
    h.routes.set("GET sessions", [{ id: "s1", title: "One", updated_at: 3 }]);
    h.routes.set("GET skills", [{ name: "crush-config" }]);
    h.routes.set("GET /v1/limits", { clis: [{ kind: "claude-code", state: "ready", read_at: "2026-10-18T10:44:00Z", windows: [] }] });
    h.routes.set("GET git/branch", { branch: "main" });
    // config, agent, providers, LSPs and MCP states fail: each fails alone.
    expect(await C.start()).toBe(true);
    expect(asked).toBe("/tmp/home/proj");
    expect(C.conn.state).toBe("connected");
    expect(C.conn.clientId).toBe("client-1");
    expect(C.conn.project).toEqual({ cwd: "/tmp/home/proj", path: "/tmp/home/proj", dataDir: "/tmp/home/proj/.crush", name: "proj" });
    expect(S.sessions.loaded).toBe(true);
    expect(S.sessions.order).toEqual(["s1"]);
    expect(W.ws.skills).toEqual([{ name: "crush-config" }]);
    expect(W.ws.branch).toBe("main");
    expect(L.limits.byKind["claude-code"].state).toBe("ready");
    expect(h.notices).toEqual([]);
  });

  it("start runs once", async () => {
    const open = vi.fn(async () => opened());
    h.open = open;
    await Promise.all([C.start(), C.start()]);
    expect(open).toHaveBeenCalledTimes(1);
  });

  it("keeps the workspace as Rust gave it", async () => {
    await C.start();
    expect(C.conn.workspace).toEqual(workspace());
    expect(Object.keys(C.conn.workspace)).not.toContain("env");
  });

  it("a join that fails leaves the error for the gate, and Try again joins again", async () => {
    h.open = async () => Promise.reject({ code: "crush_too_old", message: "Crush Desktop needs crush v0.70 or newer." });
    expect(await C.start()).toBe(false);
    expect(C.conn.state).toBe("failed");
    expect(C.conn.error).toEqual({ code: "crush_too_old", message: "Crush Desktop needs crush v0.70 or newer." });
    expect(h.notices).toEqual([]); // the gate shows it, not a notice
    h.open = async () => opened();
    expect(await C.retry()).toBe(true);
    expect(C.conn.state).toBe("connected");
    expect(C.conn.error).toBeNull();
    expect(h.retries).toBe(0);
  });

  it("is slow only after 400 ms of connecting", async () => {
    vi.useFakeTimers();
    try {
      let finish;
      h.open = () => new Promise((resolve) => (finish = resolve));
      const started = C.start();
      await vi.advanceTimersByTimeAsync(399);
      expect(C.conn.state).toBe("connecting");
      expect(C.conn.slow).toBe(false);
      await vi.advanceTimersByTimeAsync(2);
      expect(C.conn.slow).toBe(true);
      finish(opened());
      await vi.advanceTimersByTimeAsync(0);
      await started;
      expect(C.conn.slow).toBe(false);
    } finally {
      vi.useRealTimers();
    }
  });

  it("-s and -C reach the chat list (START-006)", async () => {
    h.info = { ...h.info, continue_last: true };
    h.routes.set("GET sessions", [{ id: "s2", title: "Newest", updated_at: 9 }, { id: "s1", title: "Older", updated_at: 1 }]);
    h.routes.set("GET sessions/s2", { id: "s2", title: "Newest", updated_at: 9 });
    h.routes.set("GET sessions/s2/history", []);
    h.routes.set("GET sessions/s2/messages", []);
    await C.start();
    await settle();
    expect(C.conn.launch).toEqual({ session: "", continueLast: true });
    expect(S.sessions.currentId).toBe("s2");
  });
});

describe("the 5.2 notices (engine-design.md:545-550)", () => {
  it("a folder that differs from the one asked for", async () => {
    h.info = { ...h.info, cwd: "/tmp/home/proj/sub" };
    h.open = async () => opened({ cwd: "/tmp/home/proj/sub" });
    await C.start();
    expect(h.notices).toEqual([{ kind: "info", text: "The AI works in /tmp/home/proj, the folder the first window on this project opened." }]);
    expect(C.conn.project.cwd).toBe("/tmp/home/proj/sub");
    expect(C.conn.project.path).toBe("/tmp/home/proj");
  });

  it("YOLO on when this window asked for it off, and off when asked on", async () => {
    h.open = async () => opened({ sent_yolo: false });
    await C.start();
    expect(h.notices.map((n) => n.text)).toEqual(["Crush skips permission prompts on this project, as the first window on it chose."]);
  });

  it("YOLO off when this window asked for it on", async () => {
    h.open = async () => opened({ workspace: { yolo: false } });
    await C.start();
    expect(h.notices.map((n) => n.text)).toEqual(["Crush asks before using tools on this project, as the first window on it chose."]);
  });

  it("both show, one after the other", async () => {
    vi.useFakeTimers();
    try {
      h.info = { ...h.info, cwd: "/tmp/home/proj/sub" };
      h.open = async () => opened({ cwd: "/tmp/home/proj/sub", sent_yolo: false });
      await C.start();
      expect(h.notices.map((n) => n.text)).toEqual(["The AI works in /tmp/home/proj, the folder the first window on this project opened."]);
      await vi.advanceTimersByTimeAsync(5000);
      expect(h.notices.map((n) => n.text)).toEqual([
        "The AI works in /tmp/home/proj, the folder the first window on this project opened.",
        "Crush skips permission prompts on this project, as the first window on it chose.",
      ]);
      expect(C.conn.notices.map((n) => n.kind)).toEqual(["info", "info"]);
    } finally {
      vi.useRealTimers();
    }
  });
});

describe("losing the engine (SIDE-030)", () => {
  let joinNotices = [];
  beforeEach(async () => {
    await C.start();
    // Rust's first connected event (resync false) follows the join's answer.
    await deliver(ev("connection", "updated", { state: "connected", attempt: 0, resync: false, fresh_engine: false }));
    joinNotices = [...h.notices];
    h.notices.length = 0;
  });

  it("reconnecting, then reconnected, then the shown chat reloads", async () => {
    S.upsertSession({ id: "s1", title: "One", updated_at: 1 });
    h.routes.set("GET sessions/s1", { id: "s1", title: "One", updated_at: 1 });
    h.routes.set("GET sessions/s1/history", []);
    h.routes.set("GET sessions/s1/messages", []);
    await S.openChat("s1");
    await deliver(ev("connection", "updated", { state: "reconnecting", attempt: 1 }));
    expect(C.conn.state).toBe("reconnecting");
    expect(h.notices).toEqual([{ kind: "warn", text: "Lost connection to the Crush server — reconnecting…" }]);
    const reads = () => h.calls.filter((c) => c.path === "sessions/s1/messages").length;
    const before = reads();
    h.routes.set("GET sessions", [{ id: "s1", title: "One", updated_at: 1 }]);
    await deliver(ev("connection", "updated", { state: "connected", attempt: 0, resync: true, fresh_engine: true, workspace: workspace({ id: "ws-2" }) }));
    expect(C.conn.state).toBe("connected");
    expect(C.conn.freshEngine).toBe(true);
    expect(C.conn.workspace.id).toBe("ws-2");
    expect(h.notices.at(-1)).toEqual({ kind: "success", text: "Reconnected to the Crush server." });
    expect(reads()).toBe(before + 1);
  });

  it("a chat that was opening is opened again", async () => {
    S.upsertSession({ id: "s1", title: "One", updated_at: 1 });
    let fail;
    h.routes.set("GET sessions/s1", { id: "s1", title: "One", updated_at: 1 });
    h.routes.set("GET sessions/s1/history", []);
    h.routes.set("GET sessions/s1/messages", () => new Promise((_resolve, reject) => (fail = reject)));
    S.openChat("s1");
    await settle();
    h.routes.set("GET sessions", [{ id: "s1", title: "One", updated_at: 1 }]);
    h.routes.set("GET sessions/s1/messages", []);
    await deliver(ev("connection", "updated", { state: "reconnecting", attempt: 1 }), ev("connection", "updated", { state: "connected", resync: true }));
    fail({ code: "transport", message: "socket closed" }); // the first read died with the old connection
    await settle();
    expect(S.sessions.currentId).toBe("s1");
    expect(h.calls.filter((c) => c.path === "sessions/s1/messages")).toHaveLength(2);
  });

  it("failed: the error notice for a minute with Try again, which asks Rust to reconnect", async () => {
    await deliver(ev("connection", "updated", { state: "failed", attempt: 3, error: { code: "engine_unavailable", message: "ensure failed" } }));
    expect(C.conn.state).toBe("failed");
    expect(C.conn.error).toEqual({ code: "engine_unavailable", message: "ensure failed" });
    expect(h.notices).toEqual([{ kind: "error", text: "Can't restore the connection to the Crush server. Restart Crush to recover." }]);
    const { ui } = await import("./ui.svelte.js");
    expect(ui.notice.action.label).toBe("Try again");
    await ui.notice.action.run();
    expect(h.retries).toBe(1);
  });

  it("the first connected event shows no notice", async () => {
    expect(joinNotices).toEqual([]);
    expect(C.conn.notices).toEqual([]);
  });
});

it("the limits poll runs while connected and asks only while the shown chat is idle (SIDE-007)", async () => {
  vi.useFakeTimers({ now: Date.parse("2026-10-18T10:50:00Z") });
  const flush = async () => {
    for (let i = 0; i < 10; i += 1) await vi.advanceTimersByTimeAsync(0);
  };
  const send = async (...entries) => {
    h.entries.push(...entries);
    h.ring?.();
    await flush();
  };
  const minutes = (n) => vi.advanceTimersByTimeAsync(n * 60_000);
  const asks = () => h.calls.filter((c) => c.method === "POST" && c.path === "/v1/limits/claude-code/refresh").length;
  try {
    h.routes.set("GET /v1/limits", { clis: [{ kind: "claude-code", state: "ready", read_at: "2026-10-18T10:40:00Z", windows: [] }] });
    h.routes.set("GET agent", { is_ready: true, is_busy: false, model_cfg: { provider: "claude-code", model: "opus" } });
    await C.start();
    await flush();
    expect(asks()).toBe(1); // the provider's first read
    await minutes(3); // 10:53
    expect(asks()).toBe(2);
    await send(ev("connection", "updated", { state: "reconnecting", attempt: 1 }));
    await minutes(10); // stopped while not connected
    expect(asks()).toBe(2);
    await send(ev("connection", "updated", { state: "connected", attempt: 0, resync: true }));
    S.sessions.currentId = "s1";
    S.sessions.live.s1 = true; // the shown chat runs
    await minutes(5);
    expect(asks()).toBe(2);
    delete S.sessions.live.s1;
    await minutes(1);
    expect(asks()).toBe(3);
  } finally {
    L.stopLimitsPoll();
    W.stopWorkspace();
    vi.useRealTimers();
  }
});

it("the LSP and Git 5 s reads run only while connected (SIDE-009, SIDE-013)", async () => {
  vi.useFakeTimers();
  const flush = async () => {
    for (let i = 0; i < 10; i += 1) await vi.advanceTimersByTimeAsync(0);
  };
  const reads = () => h.calls.filter((c) => c.path === "lsps" || c.path === "git/branch").length;
  try {
    await C.start();
    await flush();
    const joined = reads();
    await vi.advanceTimersByTimeAsync(5000);
    expect(reads()).toBe(joined + 2);
    h.entries.push(ev("connection", "updated", { state: "reconnecting", attempt: 1 }));
    h.ring?.();
    await flush();
    const lost = reads();
    await vi.advanceTimersByTimeAsync(20_000);
    expect(reads()).toBe(lost);
  } finally {
    L.stopLimitsPoll();
    W.stopWorkspace();
    vi.useRealTimers();
  }
});

it("the probe's connection section has the pinned fields (1.9)", async () => {
  // probeSection only registers; read it the way probe.js's report does.
  const sections = [];
  const real = probe.probeSection;
  expect(typeof real).toBe("function");
  vi.resetModules();
  vi.doMock("../probe.js", () => ({ probeSection: (name, light) => sections.push({ name, light }) }));
  const fresh = await import("./connection.svelte.js");
  const section = sections.find((s) => s.name === "connection");
  fresh.conn.state = "connected";
  fresh.conn.clientId = "client-1";
  fresh.conn.workspace = workspace();
  fresh.conn.version = { version: "v0.70.0", protocol: 1 };
  fresh.conn.notices = [{ kind: "info", text: "hello", at: 1 }];
  expect(section.light()).toEqual({
    state: "connected",
    attempt: 0,
    error: null,
    workspace_id: "ws-1",
    client_id: "client-1",
    folders: [],
    engine_version: "v0.70.0",
    fresh_engine: false,
    engine: { socket: "", pid: 0 },
    notices: [{ kind: "info", text: "hello" }],
  });
  vi.doUnmock("../probe.js");
});

it("the probe's engine is the last connected event's socket and pid (1.9)", async () => {
  const sections = [];
  vi.resetModules();
  vi.doMock("../probe.js", () => ({ probeSection: (name, light) => sections.push({ name, light }) }));
  const events = await import("../engine/events.js");
  await import("./connection.svelte.js");
  await events.startEvents();
  await settle();
  const engine = () => sections.find((s) => s.name === "connection").light().engine;
  const ensured = (pid) => ({ address: "unix:///run/crush.sock", socket: "/run/crush.sock", protocol: 1, version: "v0.70.0", pid, data_dir: "/d", started: true });
  expect(engine()).toEqual({ socket: "", pid: 0 });
  await deliver(ev("connection", "updated", { state: "connected", attempt: 0, resync: false, fresh_engine: false, engine: ensured(42) }));
  expect(engine()).toEqual({ socket: "/run/crush.sock", pid: 42 });
  await deliver(ev("connection", "updated", { state: "reconnecting", attempt: 1 }));
  expect(engine()).toEqual({ socket: "/run/crush.sock", pid: 42 });
  // A new engine after a restart: same socket, another pid.
  await deliver(ev("connection", "updated", { state: "connected", attempt: 1, resync: true, fresh_engine: true, engine: ensured(77) }));
  expect(engine()).toEqual({ socket: "/run/crush.sock", pid: 77 });
  vi.doUnmock("../probe.js");
});

describe("several folders at once (owner, 2026-10-22)", () => {
  it("showing another folder costs no engine restart", async () => {
    await C.start();
    expect(C.conn.state).toBe("connected");
    h.entries.length = 0;
    expect(await C.switchFolder("/tmp/home/other")).toBe(true);
    await settle();
    expect(h.shown).toEqual(["/tmp/home/other"]);
    expect(C.conn.project.cwd).toBe("/tmp/home/other");
    expect(C.conn.project.name).toBe("other");
    expect(C.conn.state).toBe("connected");
    expect(C.inFolder("/tmp/home/other")).toBe(true);
    expect(C.joinedFolder("/tmp/home/other")).toBe(true);
    // Nothing left: no project_enter, no join, no failed gate.
    expect(C.conn.error).toBeNull();
    expect(h.notices).toEqual([]);
  });

  it("every folder in the left list joins after the first join", async () => {
    (await import("./listfolders.svelte.js")).keepFolders(["/tmp/home/proj", "/tmp/home/two", "/tmp/home/three"]);
    await C.start();
    await settle();
    expect(h.opened).toEqual(["/tmp/home/three", "/tmp/home/two"]);
    expect(C.joinedFolder("/tmp/home/two")).toBe(true);
    expect(C.joinedFolder("/tmp/home/three")).toBe(true);
    expect(C.conn.folders["/tmp/home/two"]).toMatchObject({ clientId: "client-/tmp/home/two" });
    // The folder it shows is not joined twice.
    expect(h.opened).not.toContain("/tmp/home/proj");
  });

  it("keeps at most FOLDER_CAP folders joined, the shown one first", async () => {
    expect(C.FOLDER_CAP).toBe(4);
    (await import("./listfolders.svelte.js")).keepFolders(["/tmp/home/a", "/tmp/home/b", "/tmp/home/c", "/tmp/home/d", "/tmp/home/e"]);
    await C.start();
    await settle();
    // Three others at most: the newest of the list.
    expect(h.opened).toEqual(["/tmp/home/e", "/tmp/home/d", "/tmp/home/c"]);
  });

  it("a chat's calls go to its folder once that folder is joined", async () => {
    const api = await import("../engine/api.js");
    const seen = [];
    api.setChatFolder((sid) => (sid === "S2" ? { dir: "/tmp/home/two", joined: true } : null));
    expect(seen).toEqual([]);
    const { api: routes } = api;
    h.routes.set("GET sessions/S2", { id: "S2" });
    await routes.getSession("S2");
    const call = h.calls.filter((c) => c.path === "sessions/S2").at(-1);
    expect(call?.dir).toBe("/tmp/home/two");
  });
});
