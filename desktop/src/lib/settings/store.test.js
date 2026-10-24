import { beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({ writes: [], notifications: [] }));

vi.mock("../busy.js", () => ({ windowBusy: () => false }));
vi.mock("../engine/api.js", () => ({
  api: { setConfig: async (...args) => state.writes.push(args), setCpuAffinity: async () => ({ cpus: "0" }) },
  SCOPE: { global: 0 },
}));
vi.mock("../compaction.js", () => ({
  ZEN_FREE_DEEPSEEK_FULL_CONTEXT: "opencode_zen_free_deepseek_full_context",
  zenFreeDeepSeekFullContext: () => false,
}));
vi.mock("../stores/composer.svelte.js", () => ({ focusField: vi.fn() }));
vi.mock("../stores/ui.svelte.js", () => ({
  notify: (...args) => state.notifications.push(args),
  panelSetting: { off: false },
  setPanelShown: vi.fn(),
}));
vi.mock("../stores/workspace.svelte.js", () => ({ ws: { config: { options: {} } } }));

let settings;

beforeEach(async () => {
  vi.resetModules();
  state.writes = [];
  state.notifications = [];
  settings = await import("./store.svelte.js");
});

describe("tool-call interruption setting", () => {
  it("saves a nonnegative call limit, including zero to turn it off", async () => {
    expect(await settings.setInterruptAfterToolCalls("21")).toBe(true);
    expect(await settings.setInterruptAfterToolCalls("0")).toBe(true);
    expect(state.writes).toEqual([
      [0, "options.interrupt_after_tool_calls", 21],
      [0, "options.interrupt_after_tool_calls", 0],
    ]);
  });

  it("rejects blank, fractional, and negative limits", async () => {
    for (const text of ["", "2.5", "-1"]) {
      expect(await settings.setInterruptAfterToolCalls(text)).toBe(false);
    }
    expect(state.writes).toEqual([]);
    expect(state.notifications).toHaveLength(3);
  });
});
