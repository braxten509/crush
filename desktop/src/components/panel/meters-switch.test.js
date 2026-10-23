import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The right panel's meters after the agent changes model in the middle of a
// chat (a fallback switch, config_changed): they show the account the agent
// runs on now, never the one it left.
const h = vi.hoisted(() => ({ routes: new Map(), calls: [], entries: [], ring: null }));

vi.mock("@tauri-apps/api/webviewWindow", () => ({
  getCurrentWebviewWindow: () => ({
    listen: async (_name, fn) => {
      h.ring = fn;
      return () => {};
    },
  }),
}));

vi.mock("../../lib/engine/commands.js", () => ({
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

const reading = (kind, used, account = "") => ({
  kind,
  account,
  state: "ready",
  read_at: "2026-10-22T10:00:00Z",
  windows: [{ name: "5h", used, resets_at: "2099-01-01T00:00:00Z" }],
});

const agent = (provider, account = "") => ({
  is_ready: true,
  is_busy: false,
  model: { id: "opus" },
  model_cfg: { provider, model: "opus", account },
});

const fiveHourText = (meters) => meters.find((m) => m.label === "5-HOUR")?.text;

let limits;
let workspace;
let panel;

beforeEach(async () => {
  vi.resetModules();
  h.routes.clear();
  h.calls.length = 0;
  h.entries.length = 0;
  h.ring = null;
  vi.stubGlobal("requestAnimationFrame", (fn) => setTimeout(() => fn(0), 0));
  const events = await import("../../lib/engine/events.js");
  limits = await import("../../lib/stores/limits.svelte.js");
  workspace = await import("../../lib/stores/workspace.svelte.js");
  panel = await import("./panel.svelte.js");
  await events.startEvents();
  h.routes.set("GET config", { options: {}, models: {} });
  h.routes.set("GET providers", []);
  h.routes.set("GET /v1/limits", {
    clis: [reading("claude-code", 10), reading("claude-code", 60, "claude-2"), reading("codex-cli", 80)],
  });
  await settle();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("the meters follow the agent after a model change", () => {
  it("a sub-agent's meters follow its own account and later fallback events", async () => {
    h.routes.set("GET agent", agent("claude-code"));
    await workspace.loadAgent();
    workspace.ws.providers = [{ id: "claude-code", type: "claude-code", models: [{ id: "opus" }] }];
    await limits.loadLimits();
    const { sessions } = await import("../../lib/stores/sessions.svelte.js");
    const { helperChats, startHelperChats } = await import("../../lib/stores/helperchats.svelte.js");
    sessions.currentId = "child";
    helperChats.byId.child = { id: "child", parent: "parent", cli: "claude", model: "opus", account: "claude-2", running: true };
    startHelperChats();
    expect(fiveHourText(panel.meterNow().meters)).toBe("40% left");
    await deliver(ev("helper", "updated", { child_session_id: "child", status: "running", cli: "claude", model: "opus", account: "" }));
    expect(fiveHourText(panel.meterNow().meters)).toBe("90% left");
  });
  it("a fallback to another provider shows that provider's usage", async () => {
    h.routes.set("GET agent", agent("claude-code"));
    await workspace.loadAgent();
    await workspace.loadConfig();
    await workspace.loadProviders();
    await limits.loadLimits();
    expect(fiveHourText(panel.meterNow().meters)).toBe("90% left");

    h.routes.set("GET agent", agent("codex-cli"));
    await deliver(ev("config_changed", "updated", { workspace_id: "w" }));
    expect(fiveHourText(panel.meterNow().meters)).toBe("20% left");
  });

  it("a fallback to another account of the same provider shows that account's usage", async () => {
    h.routes.set("GET agent", agent("claude-code"));
    await workspace.loadAgent();
    await workspace.loadConfig();
    await workspace.loadProviders();
    await limits.loadLimits();
    expect(fiveHourText(panel.meterNow().meters)).toBe("90% left");

    h.routes.set("GET agent", agent("claude-code", "claude-2"));
    await deliver(ev("config_changed", "updated", { workspace_id: "w" }));
    expect(fiveHourText(panel.meterNow().meters)).toBe("40% left");
  });
});
