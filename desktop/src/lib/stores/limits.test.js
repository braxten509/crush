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

let limits;
let workspace;

beforeEach(async () => {
  vi.resetModules();
  h.routes.clear();
  h.calls.length = 0;
  h.entries.length = 0;
  h.ring = null;
  vi.stubGlobal("requestAnimationFrame", (fn) => setTimeout(() => fn(0), 0));
  const events = await import("../engine/events.js");
  limits = await import("./limits.svelte.js");
  workspace = await import("./workspace.svelte.js");
  await events.startEvents();
  await settle();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const reading = (kind, readAt, used = 10, extra = {}) => ({
  kind,
  state: readAt ? "ready" : "checking",
  ...(readAt ? { read_at: readAt } : {}),
  windows: [
    { name: "5h", used, resets_at: "2099-01-01T00:00:00Z" },
    { name: "Weekly", used: used / 2 },
  ],
  ...extra,
});

const refreshes = () => h.calls.filter((c) => c.method === "POST" && c.path.startsWith("/v1/limits/")).map((c) => c.path);

describe("newest read_at wins per kind (SIDE-007, engine design 5.9)", () => {
  it("loads the start snapshot", async () => {
    h.routes.set("GET /v1/limits", { clis: [reading("claude-code", "2026-10-18T10:44:00Z"), reading("codex-cli", null, 0, { state: "error", error: "codex: not installed" })] });
    await limits.loadLimits();
    expect(limits.limits.byKind["claude-code"].read_at).toBe("2026-10-18T10:44:00Z");
    expect(limits.limits.byKind["codex-cli"].state).toBe("error");
  });

  it("a newer reading replaces the stored one, an older one does not, a tie does", async () => {
    await deliver(ev("limits", "updated", reading("claude-code", "2026-10-18T10:44:00Z", 10)));
    await deliver(ev("limits", "updated", reading("claude-code", "2026-10-18T10:45:00Z", 20)));
    expect(limits.limits.byKind["claude-code"].windows[0].used).toBe(20);
    await deliver(ev("limits", "updated", reading("claude-code", "2026-10-18T10:44:30Z", 99)));
    expect(limits.limits.byKind["claude-code"].windows[0].used).toBe(20);
    await deliver(ev("limits", "updated", reading("claude-code", "2026-10-18T10:45:00Z", 20, { error: "claude login expired" })));
    expect(limits.limits.byKind["claude-code"].error).toBe("claude login expired");
  });

  it("compares nanoseconds and offsets", async () => {
    limits.applyLimits(reading("grok-cli", "2026-10-18T10:44:00.000000002Z", 30));
    limits.applyLimits(reading("grok-cli", "2026-10-18T10:44:00.000000001Z", 40));
    expect(limits.limits.byKind["grok-cli"].windows[0].used).toBe(30);
    limits.applyLimits(reading("grok-cli", "2026-10-18T12:44:00.000000003+02:00", 50));
    expect(limits.limits.byKind["grok-cli"].windows[0].used).toBe(50);
    // 11:44 at +02:00 is 09:44 UTC, older than the stored 10:44 UTC.
    limits.applyLimits(reading("grok-cli", "2026-10-18T11:44:00+02:00", 60));
    expect(limits.limits.byKind["grok-cli"].windows[0].used).toBe(50);
    expect(limits.readTime("2026-10-18T12:44:00-01:30")).toEqual([Date.parse("2026-10-18T14:14:00Z"), 0]);
    expect(limits.readTime("2026-10-18T10:44:00.123456789Z")).toEqual([Date.parse("2026-10-18T10:44:00.123Z"), 456789]);
    expect(limits.readTime("0001-01-01T00:00:00Z")).toBeNull();
    expect(limits.readTime("yesterday")).toBeNull();
  });

  it("a reading without read_at never hides one with it, and replaces one without", async () => {
    limits.applyLimits(reading("agy-cli", null, 0));
    limits.applyLimits(reading("agy-cli", null, 0, { state: "error", error: "no AGY login in the keyring" }));
    expect(limits.limits.byKind["agy-cli"].state).toBe("error");
    limits.applyLimits(reading("agy-cli", "2026-10-18T10:44:00Z", 15));
    limits.applyLimits(reading("agy-cli", null, 0, { state: "checking" }));
    expect(limits.limits.byKind["agy-cli"].state).toBe("ready");
    expect(limits.limits.byKind["agy-cli"].windows[0].used).toBe(15);
  });

  it("a resync reads the snapshot again", async () => {
    h.routes.set("GET /v1/limits", { clis: [reading("claude-code", "2026-10-18T11:00:00Z", 70)] });
    await deliver({ kind: "resync" });
    expect(limits.limits.byKind["claude-code"].windows[0].used).toBe(70);
  });
});

describe("refreshed on model change", () => {
  beforeEach(async () => {
    h.routes.set("GET /v1/limits", { clis: [reading("claude-code", "2026-10-18T10:44:00Z"), reading("codex-cli", "2026-10-18T10:44:00Z")] });
    await limits.loadLimits();
  });

  it("asks once per provider change, and only for kinds that report limits", () => {
    limits.agentProviderChanged("claude-code");
    limits.agentProviderChanged("claude-code");
    expect(refreshes()).toEqual(["/v1/limits/claude-code/refresh"]);
    limits.agentProviderChanged("opencode-cli");
    expect(refreshes()).toEqual(["/v1/limits/claude-code/refresh"]);
    limits.agentProviderChanged("codex-cli");
    expect(refreshes()).toEqual(["/v1/limits/claude-code/refresh", "/v1/limits/codex-cli/refresh"]);
  });

  it("follows the agent the workspace reads", async () => {
    h.routes.set("GET agent", { is_ready: true, is_busy: false, model: { id: "gpt-6-sol" }, model_cfg: { provider: "codex-cli", model: "gpt-6-sol" } });
    await workspace.loadAgent();
    expect(refreshes()).toEqual(["/v1/limits/codex-cli/refresh"]);
    await deliver(ev("config_changed", "updated", { workspace_id: "w" }));
    expect(refreshes()).toEqual(["/v1/limits/codex-cli/refresh"]);
    h.routes.set("GET agent", { is_ready: true, model_cfg: { provider: "claude-code", model: "opus" } });
    h.routes.set("GET config", { models: {} });
    await deliver(ev("config_changed", "updated", { workspace_id: "w" }));
    expect(refreshes()).toEqual(["/v1/limits/codex-cli/refresh", "/v1/limits/claude-code/refresh"]);
  });
});

it("a change before the snapshot waits for it", async () => {
  limits.agentProviderChanged("claude-code");
  expect(refreshes()).toEqual([]);
  h.routes.set("GET /v1/limits", { clis: [reading("claude-code", "2026-10-18T10:44:00Z")] });
  await limits.loadLimits();
  expect(refreshes()).toEqual(["/v1/limits/claude-code/refresh"]);
});

describe("what the meters read", () => {
  it("keeps the windows for every model and for this one (cliagent.Limits)", () => {
    limits.applyLimits({
      kind: "agy-cli",
      state: "ready",
      read_at: "2026-10-18T10:44:00Z",
      windows: [
        { name: "Quota", used: 40, model: "gemini-3.1-pro-high" },
        { name: "Quota", used: 90, model: "claude-opus-4-6-thinking" },
        { name: "Weekly", used: 5 },
      ],
    });
    const shown = limits.limitsFor("agy-cli", "gemini-3.1-pro-high");
    expect(shown.windows.map((w) => `${w.name}:${w.used}`)).toEqual(["Quota:40", "Weekly:5"]);
    expect(limits.limitsFor("codex-cli", "x")).toBeNull();
  });

  it("checking… until a reading: a kind without read_at gives null", () => {
    limits.applyLimits(reading("claude-code", null));
    expect(limits.limitsFor("claude-code", "opus")).toBeNull();
  });

  it("a window past its reset is whole again (Limit.Left)", () => {
    const now = Date.parse("2026-10-18T12:00:00Z");
    expect(limits.leftPercent({ used: 85, resets_at: "2026-10-18T11:00:00Z" }, now)).toBe(100);
    expect(limits.leftPercent({ used: 85, resets_at: "2026-10-18T13:00:00Z" }, now)).toBe(15);
    expect(limits.leftPercent({ used: 120 }, now)).toBe(0);
  });
});
