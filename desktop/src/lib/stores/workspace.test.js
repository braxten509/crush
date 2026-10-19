import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The real event loop (events.js) with fake Tauri calls (see prompts.test.js).
const h = vi.hoisted(() => ({ routes: new Map(), calls: [], entries: [], ring: null }));

vi.mock("@tauri-apps/api/webviewWindow", () => ({
  getCurrentWebviewWindow: () => ({
    listen: async (_name, fn) => {
      h.ring = fn;
      return () => {};
    },
  }),
}));

vi.mock("../engine/commands.js", () => ({
  api: async (method, path, query, body, callId) => {
    h.calls.push({ method, path, query, body, callId });
    const route = h.routes.get(`${method} ${path}`);
    if (typeof route === "function") return route({ query, body, callId });
    if (route !== undefined) return route;
    if (method === "GET") return Promise.reject({ code: "not_found", message: `no answer for GET ${path}` });
    return null;
  },
  api_abort: async () => true,
  engine_set_current_session: async () => null,
  events_take: async () => h.entries.splice(0),
  lastClipboardWrite: () => null,
}));

const tick = () => new Promise((resolve) => setTimeout(resolve, 0));
async function settle(rounds = 8) {
  for (let i = 0; i < rounds; i += 1) await tick();
}
async function deliver(...entries) {
  h.entries.push(...entries);
  h.ring?.();
  await settle();
}
const ev = (type, event, payload) => ({ kind: "event", type, event, payload });

let workspace;

beforeEach(async () => {
  vi.resetModules();
  h.routes.clear();
  h.calls.length = 0;
  h.entries.length = 0;
  h.ring = null;
  vi.stubGlobal("requestAnimationFrame", (fn) => setTimeout(() => fn(0), 0));
  const events = await import("../engine/events.js");
  workspace = await import("./workspace.svelte.js");
  await events.startEvents();
  await settle();
});

afterEach(() => {
  workspace.stopWorkspace();
  vi.unstubAllGlobals();
});

// A merged config as GET config answers it: every secret-bearing field holds
// a marker that must not survive pickConfig.
const SECRETS = ["sk-provider-key", "x-provider-header", "env-secret-value", "hook-command-secret", "mcp-header-token", "mcp-env-token", "mcp-url-secret", "lsp-env-secret", "tool-secret"];
const fullConfig = () => ({
  $schema: "https://charm.land/crush.json",
  models: { large: { provider: "claude-code", model: "opus", reasoning_effort: "high" }, small: { provider: "claude-code", model: "haiku" } },
  recent_models: { large: [{ provider: "codex-cli", model: "gpt-6-sol" }] },
  providers: {
    anthropic: { id: "anthropic", api_key: "sk-provider-key", extra_headers: { "X-Key": "x-provider-header" }, oauth: { access_token: "sk-provider-key" }, models: [{ id: "claude" }] },
  },
  mcp: {
    github: { type: "http", url: "https://mcp-url-secret.example", headers: { Authorization: "mcp-header-token" }, env: { TOKEN: "mcp-env-token" }, command: "gh", args: ["mcp"], disabled: false },
    docker: { type: "stdio", command: "docker", env: { DOCKER: "mcp-env-token" }, disabled: true },
  },
  lsp: { gopls: { command: "gopls", env: { GOFLAGS: "lsp-env-secret" }, disabled: true }, ts: { command: "tsserver" } },
  options: { tui: { compact_mode: true, composer_footer: ["model"] }, notifications: "disabled", data_directory: ".crush" },
  permissions: { allowed_tools: ["view"] },
  tools: { ls: { secret: "tool-secret" } },
  hooks: { PreToolUse: [{ command: "hook-command-secret" }] },
  env: { OPENAI_API_KEY: "env-secret-value" },
});

describe("pickConfig keeps no provider, env, hook or MCP header field", () => {
  it("keeps models, recent_models, options, permissions and each mcp and lsp entry's type and disabled", () => {
    const picked = workspace.pickConfig(fullConfig());
    expect(Object.keys(picked).sort()).toEqual(["lsp", "mcp", "models", "options", "permissions", "recent_models"]);
    expect(picked.models.large).toEqual({ provider: "claude-code", model: "opus", reasoning_effort: "high" });
    expect(picked.recent_models.large[0].model).toBe("gpt-6-sol");
    expect(picked.options.tui.compact_mode).toBe(true);
    expect(picked.permissions.allowed_tools).toEqual(["view"]);
    expect(picked.mcp).toEqual({ github: { type: "http", disabled: false }, docker: { type: "stdio", disabled: true } });
    expect(picked.lsp).toEqual({ gopls: { disabled: true }, ts: { disabled: false } });
  });

  it("no secret marker survives anywhere in the result", () => {
    const text = JSON.stringify(workspace.pickConfig(fullConfig()));
    for (const secret of SECRETS) expect(text).not.toContain(secret);
    for (const key of ["providers", "env", "hooks", "tools", "headers", "api_key", "extra_headers", "url", "command", "args"]) {
      expect(text).not.toContain(`"${key}"`);
    }
  });

  it("is a copy: changing the result never touches the engine's object", () => {
    const config = fullConfig();
    const picked = workspace.pickConfig(config);
    picked.models.large.model = "changed";
    expect(config.models.large.model).toBe("opus");
  });

  it("gives null for no config", () => {
    expect(workspace.pickConfig(null)).toBeNull();
    expect(workspace.pickConfig("text")).toBeNull();
  });

  it("the store keeps only the picked config", async () => {
    h.routes.set("GET config", fullConfig());
    await workspace.loadConfig();
    const text = JSON.stringify(workspace.ws.config);
    for (const secret of SECRETS) expect(text).not.toContain(secret);
    expect(workspace.ws.config.options.notifications).toBe("disabled");
  });

  it("providers lose their keys, endpoints and headers", async () => {
    h.routes.set("GET providers", [
      { id: "openai", name: "OpenAI", type: "openai", api_key: "sk-provider-key", api_endpoint: "https://mcp-url-secret.example", default_headers: { A: "x-provider-header" }, models: [{ id: "gpt" }] },
    ]);
    await workspace.loadProviders();
    expect(workspace.ws.providers).toEqual([{ id: "openai", name: "OpenAI", type: "openai", models: [{ id: "gpt" }] }]);
  });
});

describe("events and reads", () => {
  it("config_changed reads the config and the agent again", async () => {
    h.routes.set("GET config", { models: { large: { provider: "claude-code", model: "opus" } } });
    h.routes.set("GET agent", { is_ready: true, is_busy: false, model_cfg: { provider: "claude-code", model: "opus" } });
    await deliver(ev("config_changed", "updated", { workspace_id: "w" }));
    expect(workspace.ws.config.models.large.model).toBe("opus");
    expect(workspace.ws.agent.is_ready).toBe(true);
  });

  it("file events update the held history in the engine's order (SIDE-008)", async () => {
    h.routes.set("GET sessions/s1/history", [
      { id: "f2", session_id: "s1", path: "/p/a.go", content: "b", version: 1, created_at: 20 },
      { id: "f1", session_id: "s1", path: "/p/a.go", content: "a", version: 0, created_at: 10 },
    ]);
    await workspace.loadFiles("s1");
    expect(workspace.ws.files.s1.map((f) => f.id)).toEqual(["f1", "f2"]);
    await deliver(ev("file", "created", { id: "f3", session_id: "s1", path: "/p/b.go", content: "", version: 0, created_at: 30 }));
    expect(workspace.ws.files.s1.map((f) => f.id)).toEqual(["f1", "f3", "f2"]);
    await deliver(ev("file", "updated", { id: "f3", session_id: "s1", path: "/p/b.go", content: "x", version: 0, created_at: 30 }));
    expect(workspace.ws.files.s1.find((f) => f.id === "f3").content).toBe("x");
    expect(workspace.ws.files.s1).toHaveLength(3);
    await deliver(ev("file", "deleted", { id: "f1", session_id: "s1" }));
    expect(workspace.ws.files.s1.map((f) => f.id)).toEqual(["f3", "f2"]);
    await deliver(ev("file", "created", { id: "g1", session_id: "other", path: "/p/c.go", version: 0 }));
    expect(workspace.ws.files.other).toBeUndefined();
  });

  it("LSP rows count diagnostics by severity; a failed count is zeros", async () => {
    h.routes.set("GET lsps", { gopls: { name: "gopls", state: 2 }, ts: { name: "ts", state: 1 } });
    h.routes.set("GET lsps/gopls/diagnostics", { "file:///a.go": [{ severity: 1 }, { severity: 2 }, { severity: 2 }], "file:///b.go": [{ severity: 4 }, { severity: 3 }, { severity: 3 }, { severity: 3 }] });
    await workspace.loadLsps();
    expect(workspace.ws.lsps.gopls.diagnostics).toEqual({ error: 1, warning: 2, information: 3, hint: 1 });
    expect(workspace.ws.lsps.ts.diagnostics).toEqual({ error: 0, warning: 0, information: 0, hint: 0 });
    expect(workspace.ws.lsps.gopls.state).toBe(2);
  });

  it("LSP events during a read cause one more read, not overlapping ones", async () => {
    let answer;
    let reads = 0;
    h.routes.set("GET lsps", () => {
      reads += 1;
      return new Promise((resolve) => {
        answer = resolve;
      });
    });
    const first = workspace.loadLsps();
    await settle(2);
    await deliver(ev("lsp_event", "updated", {}), ev("lsp_event", "updated", {}));
    expect(reads).toBe(1);
    answer({});
    await settle();
    expect(reads).toBe(2);
    answer({});
    await first;
    expect(reads).toBe(2);
  });

  it("a failed MCP read keeps the old rows (SIDE-010)", async () => {
    h.routes.set("GET mcp/states", { github: { name: "github", state: "connected", tool_count: 3 } });
    await workspace.loadMcps();
    h.routes.set("GET mcp/states", () => Promise.reject({ code: "transport", message: "down" }));
    await deliver(ev("mcp_event", "updated", { type: "state_changed", name: "github" }));
    expect(workspace.ws.mcps.github.tool_count).toBe(3);
  });

  describe("an older read never overwrites a newer one", () => {
    const older = { models: { large: { provider: "claude-code", model: "opus" } } };
    const newer = { models: { large: { provider: "codex-cli", model: "gpt-6-sol" } } };
    const f1 = { id: "f1", session_id: "s1", path: "/p/a.go", version: 0, created_at: 10 };
    const f2 = { id: "f2", session_id: "s1", path: "/p/b.go", version: 0, created_at: 20 };
    // Each read of the route waits until the test answers it, in either order.
    function slow(route) {
      const reads = [];
      h.routes.set(route, () => new Promise((resolve) => reads.push(resolve)));
      return reads;
    }
    async function answer(reads, order, old, now) {
      const [first, second] = order === "the newer read lands first" ? [[1, now], [0, old]] : [[0, old], [1, now]];
      reads[first[0]](first[1]);
      await settle();
      reads[second[0]](second[1]);
      await settle();
    }

    // The answer is checked and stored with nothing in between: whatever runs
    // after any number of microtasks of a landing read sees it stored or not.
    const micro = async (n) => {
      for (let i = 0; i < n; i += 1) await null;
    };

    it("a list dropped while its read lands stays dropped, at every point of the landing", async () => {
      for (let k = 0; k < 12; k += 1) {
        const reads = slow("GET sessions/s1/history");
        const loading = workspace.loadFiles("s1");
        reads[0]([f1]);
        await micro(k);
        workspace.dropFiles("s1");
        await loading;
        await settle();
        expect(workspace.ws.files.s1, `dropped after ${k} microtasks`).toBeUndefined();
      }
    });

    const fields = [
      { field: "config", route: "GET config", load: () => workspace.loadConfig(), value: () => workspace.ws.config, old: older, now: newer },
      { field: "files", route: "GET sessions/s1/history", load: () => workspace.loadFiles("s1"), value: () => workspace.ws.files.s1, old: [f1], now: [f1, f2] },
    ];
    for (const { field, route, load, value, old, now } of fields) {
      it(`${field}: once a newer read starts, an older one never stores its answer, at every point of its landing`, async () => {
        for (let k = 0; k < 12; k += 1) {
          const reads = slow(route);
          const first = load();
          reads[0](old);
          await micro(k);
          const before = value();
          const second = load(); // a newer read starts
          await first;
          await settle();
          expect(value(), `newer read started after ${k} microtasks`).toBe(before);
          reads[1](now);
          await second;
        }
      });
    }

    for (const order of ["the newer read lands first", "the older read lands first"]) {
      it(`config: a config_changed read, then a resync's read; ${order}`, async () => {
        const reads = slow("GET config");
        await deliver(ev("config_changed", "updated", { workspace_id: "w" }));
        await deliver({ kind: "resync" });
        expect(reads).toHaveLength(2);
        await answer(reads, order, older, newer);
        expect(workspace.ws.config.models.large.provider).toBe("codex-cli");
      });

      it(`files: two overlapping resyncs; ${order}`, async () => {
        workspace.startWorkspace({ currentChat: () => "s1" });
        const reads = slow("GET sessions/s1/history");
        await deliver({ kind: "resync" });
        await deliver({ kind: "resync" });
        expect(reads).toHaveLength(2);
        await answer(reads, order, [f1], [f1, f2]);
        expect(workspace.ws.files.s1.map((f) => f.id)).toEqual(["f1", "f2"]);
      });
    }
  });

  it("a resync holds file, skills and config events until its reads land, so the newer state wins", async () => {
    h.routes.set("GET sessions/s1/history", []);
    await workspace.startWorkspace({ currentChat: () => "s1" });
    await workspace.loadFiles("s1");
    const wait = () => {
      let resolve;
      const promise = new Promise((yes) => (resolve = yes));
      return { promise, resolve };
    };
    const history = wait();
    const skills = wait();
    const config = wait();
    h.routes.set("GET sessions/s1/history", () => history.promise);
    h.routes.set("GET skills", () => skills.promise);
    h.routes.set("GET config", () => config.promise);
    await deliver({ kind: "resync" });
    // Meanwhile a file is saved, a skill is added and the model changes.
    h.routes.set("GET skills", [{ name: "new-skill" }]);
    h.routes.set("GET config", { models: { large: { provider: "codex-cli", model: "gpt-6-sol" } } });
    await deliver(
      ev("file", "created", { id: "f2", session_id: "s1", path: "/p/b.go", version: 0, created_at: 20 }),
      ev("skills_event", "updated", {}),
      ev("config_changed", "updated", { workspace_id: "w" }),
    );
    // The resync's reads answer with what the engine had before those events.
    history.resolve([{ id: "f1", session_id: "s1", path: "/p/a.go", version: 0, created_at: 10 }]);
    skills.resolve([{ name: "old-skill" }]);
    config.resolve({ models: { large: { provider: "claude-code", model: "opus" } } });
    await settle();
    expect(workspace.ws.files.s1.map((f) => f.id)).toEqual(["f1", "f2"]);
    expect(workspace.ws.skills).toEqual([{ name: "new-skill" }]);
    expect(workspace.ws.config.models.large.provider).toBe("codex-cli");
  });

  it("skills_event reads the skills again", async () => {
    h.routes.set("GET skills", [{ name: "crush-config" }]);
    await deliver(ev("skills_event", "updated", {}));
    expect(workspace.ws.skills).toEqual([{ name: "crush-config" }]);
  });

  it("the Git branch answer is {branch}", async () => {
    h.routes.set("GET git/branch", { branch: "main" });
    await workspace.loadBranch();
    expect(workspace.ws.branch).toBe("main");
  });

  it("the Git branch is read every 5 s while visible, and LSPs as a backstop (SIDE-013, SIDE-009)", async () => {
    vi.useFakeTimers();
    try {
      h.routes.set("GET git/branch", { branch: "main" });
      h.routes.set("GET lsps", {});
      workspace.startWorkspace();
      const count = (path) => h.calls.filter((c) => c.path === path).length;
      const branchReads = count("git/branch");
      await vi.advanceTimersByTimeAsync(5000);
      expect(count("git/branch")).toBe(branchReads + 1);
      expect(count("lsps")).toBeGreaterThanOrEqual(2);
      workspace.stopWorkspace();
      await vi.advanceTimersByTimeAsync(10000);
      expect(count("git/branch")).toBe(branchReads + 1);
    } finally {
      vi.useRealTimers();
    }
  });

  it("the two 5 s reads run only while connected", async () => {
    vi.useFakeTimers();
    try {
      h.routes.set("GET git/branch", { branch: "main" });
      h.routes.set("GET lsps", {});
      let up = false;
      workspace.startWorkspace({ connected: () => up });
      const reads = () => h.calls.filter((c) => c.path === "git/branch" || c.path === "lsps").length;
      const atStart = reads();
      await vi.advanceTimersByTimeAsync(15000);
      expect(reads()).toBe(atStart);
      up = true;
      await vi.advanceTimersByTimeAsync(5000);
      expect(h.calls.filter((c) => c.path === "git/branch")).toHaveLength(2);
      expect(h.calls.filter((c) => c.path === "lsps")).toHaveLength(2);
    } finally {
      workspace.stopWorkspace();
      vi.useRealTimers();
    }
  });

  it("each start-up read may fail alone (START-007)", async () => {
    h.routes.set("GET skills", [{ name: "a" }]);
    h.routes.set("GET git/branch", { branch: "dev" });
    await workspace.startWorkspace();
    workspace.stopWorkspace();
    expect(workspace.ws.skills).toEqual([{ name: "a" }]);
    expect(workspace.ws.branch).toBe("dev");
    expect(workspace.ws.config).toBeNull();
  });
});
