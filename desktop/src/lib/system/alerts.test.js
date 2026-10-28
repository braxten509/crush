import { beforeEach, describe, expect, it, vi } from "vitest";

// A toast names its chat and folder (owner, 2026-10-28: clicking one opens
// Crush on that chat), so the click can bring that chat up.
const state = vi.hoisted(() => ({ handlers: {}, notified: [] }));

vi.mock("@tauri-apps/api/webviewWindow", () => ({ getCurrentWebviewWindow: () => ({ label: "main" }) }));
vi.mock("../engine/api.js", () => ({
  api: { agentSession: async () => ({ is_busy: false }), queuedCount: async () => 0 },
}));
vi.mock("../engine/commands.js", () => ({
  alertHandler: async () => "main",
  alertNotify: vi.fn(async (...args) => state.notified.push(args)),
  alertSound: vi.fn(async () => null),
}));
vi.mock("../engine/events.js", () => ({ onEvent: (name, handler) => (state.handlers[name] = handler) }));
vi.mock("../stores/composer.svelte.js", () => ({ projectDir: () => "/home/me/proj" }));
vi.mock("../stores/sessions.svelte.js", () => ({
  isHelperChat: () => false,
  sessions: { currentId: "shown", byId: { s1: { title: "Fix the toasts" } } },
}));
vi.mock("../stores/workspace.svelte.js", () => ({ ws: { config: { options: { notifications: "auto" } } } }));
vi.mock("./focus.js", () => ({ windowFocus: { focused: false } }));
vi.mock("./badge.svelte.js", () => ({ markDone: () => {}, markError: () => {} }));

const settle = async () => {
  for (let i = 0; i < 20; i += 1) await Promise.resolve();
};

beforeEach(async () => {
  vi.resetModules();
  state.handlers = {};
  state.notified = [];
  (await import("./alerts.js")).startAlerts();
});

describe("alerts (alerts.js)", () => {
  it("a finished chat's toast carries the chat and its folder", async () => {
    state.handlers.agent_event("created", { type: "agent_finished", session_id: "s1" });
    await settle();
    expect(state.notified).toEqual([["Crush finished", '"Fix the toasts" is done', { sessionId: "s1", projectDir: "/home/me/proj" }]]);
  });
});
