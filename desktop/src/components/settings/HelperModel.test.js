import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The helper model row in Settings (HelperModel.svelte) and the picker it
// opens. Mounted in a page model with the engine's routes answered from a
// fake config and catalog, so a choice runs the real lib/models/choose.js.
const state = vi.hoisted(() => ({ routes: {}, posts: [] }));

vi.mock("@tauri-apps/api/core", () => ({ invoke: vi.fn(async () => null) }));
vi.mock("@tauri-apps/api/webviewWindow", () => ({
  getCurrentWebviewWindow: () => ({ listen: async () => () => {} }),
}));
vi.mock("../../lib/engine/commands.js", () => ({
  api: vi.fn(async (method, path, _query, body) => {
    if (method === "POST") state.posts.push({ path, body: body ?? {} });
    const route = state.routes[`${method} ${path}`];
    return typeof route === "function" ? route(body) : route ?? null;
  }),
  events_take: vi.fn(async () => []),
  api_abort: vi.fn(async () => true),
}));

// jsdom has no Web Animations: a stub that finishes at once.
if (typeof Element.prototype.animate !== "function") {
  Element.prototype.animate = function animate() {
    const animation = { cancel() {}, finish() {}, onfinish: null, oncancel: null, playState: "finished" };
    setTimeout(() => animation.onfinish?.(), 0);
    return animation;
  };
}

// Nor ResizeObserver, which the sheet's key footer uses: one that never fires.
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

let svelte;
let dialogs;
const hosts = [];

beforeEach(async () => {
  vi.resetModules();
  svelte = await import("svelte");
  state.posts = [];
  state.routes = {
    "GET providers": [{ id: "anthropic", name: "Anthropic", models: [{ id: "claude-a", name: "Claude A" }, { id: "claude-b", name: "Claude B" }] }],
    "GET agent": null,
    "GET last-pick": { provider: "anthropic", model: "claude-a" },
    "GET config": {
      models: { small: { provider: "anthropic", model: "claude-b" } },
      recent_models: {},
      providers: { anthropic: { id: "anthropic", name: "Anthropic", models: [] } },
    },
  };
  dialogs = await import("../../lib/dialogs.js");
  dialogs.registerDialog("models", (await import("../models/ModelPicker.svelte")).default);
});

afterEach(() => {
  while (hosts.length) svelte.unmount(hosts.pop());
  document.body.replaceChildren();
});

const mount = (component, props = {}) => hosts.push(svelte.mount(component, { target: document.body, props }));

async function settle(turns = 40) {
  for (let i = 0; i < turns; i += 1) {
    await Promise.resolve();
    svelte.flushSync();
  }
}

describe("the helper model in Settings (HelperModel.svelte)", () => {
  it("saves a pick as the helper model only, never as a chat's model", async () => {
    mount((await import("./HelperModel.svelte")).default);
    await settle();
    expect(document.body.textContent).toContain("Helper model (titles)");
    expect(document.body.textContent).toContain("Claude B");

    [...document.querySelectorAll("button")].find((b) => b.textContent.includes("Change")).click();
    expect(dialogs.dialogs.map((d) => [d.id, d.props])).toEqual([["models", { helper: true }]]);
    mount(dialogs.dialogs[0].component ?? (await import("../models/ModelPicker.svelte")).default, dialogs.dialogs[0].props);
    await settle();
    const row = [...document.querySelectorAll('[role="option"]')].find((r) => r.textContent.includes("Claude A"));
    row.click();
    row.click();
    await settle();

    const modelWrites = state.posts.filter((p) => p.path.endsWith("model") || p.path === "last-pick");
    expect(modelWrites).toEqual([{ path: "config/model", body: { scope: 0, model_type: "small", model: { provider: "anthropic", model: "claude-a" } } }]);
  });
});
