import { beforeEach, expect, it, vi } from "vitest";

// What the shown chat's model can do (lib/models/capabilities.svelte.js), read
// from the engine per chat (GET agent/capabilities?session_id=…), with fake
// Tauri calls. It lives here to run in the page model, where Svelte's effects
// (the watches that follow the shown chat) run.
const h = vi.hoisted(() => ({ routes: new Map(), calls: [] }));

vi.mock("@tauri-apps/api/webviewWindow", () => ({
  getCurrentWebviewWindow: () => ({ listen: async () => () => {} }),
}));
vi.mock("../../lib/engine/commands.js", () => ({
  api: async (method, path, query, body) => {
    h.calls.push({ method, path, query });
    const route = h.routes.get(`${method} ${path}`);
    return typeof route === "function" ? route({ query, body }) : (route ?? null);
  },
  api_abort: async () => true,
  events_take: async () => [],
}));

let svelte;
beforeEach(async () => {
  vi.resetModules();
  h.routes.clear();
  h.calls.length = 0;
  svelte = await import("svelte");
});

async function settle(turns = 20) {
  for (let i = 0; i < turns; i += 1) {
    await Promise.resolve();
    svelte.flushSync();
  }
}

it("capabilities follow the shown chat and drop a late reply for another", async () => {
  const { sessions } = await import("../../lib/stores/sessions.svelte.js");
  const { capabilities } = await import("../../lib/models/capabilities.svelte.js");
  const { startWatches } = await import("../../lib/live/helpers.svelte.js");
  const { registerAction } = await import("../../lib/actions.js");
  const { systemEntries } = await import("../../lib/palette/entries.js");
  registerAction({ id: "model.fast", title: "Toggle FAST mode", run: () => {} });

  sessions.byId.a = { id: "a", model: { provider: "codex-cli", model: "gpt-6", service_tier: "fast" } };
  sessions.byId.b = { id: "b", model: { provider: "claude-code", model: "opus" } };
  let finishA;
  h.routes.set("GET agent/capabilities", ({ query }) =>
    query?.session_id === "a" ? new Promise((resolve) => (finishA = resolve)) : { kind: "claude-code", supports_fast: false },
  );
  const limits = await import("../../lib/stores/limits.svelte.js");
  h.routes.set("GET /v1/limits", { clis: ["codex-cli", "claude-code"].map((kind) => ({ kind, state: "ready", read_at: "2026-10-24T10:00:00Z", windows: [] })) });
  await limits.loadLimits();
  startWatches();
  sessions.currentId = "a";
  await settle();
  sessions.currentId = "b";
  await settle();
  finishA({ kind: "codex-cli", supports_fast: true });
  await settle();

  const asked = h.calls.filter((c) => c.path === "agent/capabilities").map((c) => c.query?.session_id);
  expect(asked).toEqual(["a", "b"]);
  expect(capabilities.kind).toBe("claude-code");
  const refreshes = () => h.calls.filter((c) => c.method === "POST" && c.path.startsWith("/v1/limits/")).map((c) => c.path);
  expect(refreshes()).toEqual(["/v1/limits/codex-cli/refresh", "/v1/limits/claude-code/refresh"], "the limits follow the shown chat");
  expect(capabilities.supports_fast).toBe(false);
  const fast = systemEntries({}).find((entry) => entry.id === "toggle_fast_mode");
  expect(fast.shown()).toBe(false);

  // Chat B's own model moves to codex: its answer is read again.
  h.routes.set("GET agent/capabilities", { kind: "codex-cli", supports_fast: true });
  sessions.byId.b = { ...sessions.byId.b, model: { provider: "codex-cli", model: "gpt-6" } };
  await settle();
  expect(capabilities.supports_fast).toBe(true);
  expect(fast.shown()).toBe(true);
  expect(fast.label).toBe("Enable FAST Mode");
});
