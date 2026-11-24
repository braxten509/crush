import { beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  busy: true,
  helperCalls: [],
  configCalls: [],
  chatCalls: [],
  notifications: [],
  updates: 0,
}));

vi.mock("../busy.js", () => ({ windowBusy: () => state.busy }));
vi.mock("../dialogs.js", () => ({
  closeDialog: vi.fn(),
  hasDialog: () => false,
  openDialog: vi.fn(),
}));
vi.mock("../engine/api.js", () => ({
  api: {
    setHelperModel: async (model) => state.helperCalls.push(model),
    setConfig: async (...args) => state.configCalls.push(args),
    updateAgent: async () => {
      state.updates += 1;
    },
    initAgent: async () => {},
  },
  SCOPE: { global: 0 },
}));
vi.mock("../stores/workspace.svelte.js", () => ({ loadAgent: vi.fn() }));
vi.mock("../stores/ui.svelte.js", () => ({
  notify: (...args) => state.notifications.push(args),
}));
vi.mock("./providers.js", () => ({
  modelInfo: () => ({ name: "Gemma 4 31B" }),
  needsKeyFirst: () => false,
}));
vi.mock("./store.svelte.js", () => ({
  modelData: {
    cfg: { providers: new Map([["tinfoil-2", { has_api_key: true }]]) },
    catalog: [],
  },
  refreshModels: vi.fn(),
}));
vi.mock("./toggles.js", () => ({
  saveModelChoice: async (model) => state.chatCalls.push(model),
}));

import { chooseModel } from "./choose.js";

const pick = (modelType) => ({
  provider: { id: "tinfoil-2", name: "Tinfoil" },
  model: { provider: "tinfoil-2", model: "gemma4-31b" },
  modelType,
});

beforeEach(() => {
  state.busy = true;
  state.helperCalls = [];
  state.configCalls = [];
  state.chatCalls = [];
  state.notifications = [];
  state.updates = 0;
});

describe("chooseModel while a chat is busy", () => {
  it("allows changing the helper and compaction models", async () => {
    expect(await chooseModel(pick("small"))).toBe(true);
    expect(await chooseModel(pick("compact"))).toBe(true);
    expect(state.helperCalls).toEqual([{ provider: "tinfoil-2", model: "gemma4-31b" }]);
    expect(state.configCalls).toEqual([[0, "models.compact", { provider: "tinfoil-2", model: "gemma4-31b" }]]);
    expect(state.updates).toBe(1);
  });

  it("still blocks changing the active chat's own model", async () => {
    expect(await chooseModel(pick("large"))).toBe(false);
    expect(state.chatCalls).toEqual([]);
    expect(state.notifications[0]?.[0]).toBe("warn");
  });
});
