import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The model menu over the model button (ModelMenu.svelte). Mounted in a page
// model with the engine's routes answered from a fake config and catalog, so a
// click on a model runs the real choice (lib/models/choose.js) and what comes
// back can be checked.
const state = vi.hoisted(() => ({ routes: {}, posts: [] }));
const orders = vi.hoisted(() => ({ config: [] }));

vi.mock("@tauri-apps/api/core", () => ({ invoke: vi.fn(async () => null) }));
vi.mock("@tauri-apps/api/webviewWindow", () => ({
  getCurrentWebviewWindow: () => ({ listen: async () => () => {} }),
}));
vi.mock("../../lib/engine/commands.js", () => ({
  api: vi.fn(async (method, path, _query, body) => {
    if (method === "POST") state.posts.push({ path, body: body ?? {} });
    const key = `${method} ${path}`;
    // A queued answer serves the config reads in order, so a choice's own
    // refresh sees the model the engine just saved.
    if (key === "GET config" && orders.config.length > 0) return orders.config.shift();
    return state.routes[key] ?? null;
  }),
  events_take: vi.fn(async () => []),
  api_abort: vi.fn(async () => true),
}));

let svelte;
let ModelMenu;
let uiStore;
let ui;
let host;

// A catalog with two Anthropic models: one with effort levels, one without.
const PROVIDERS = [
  {
    id: "anthropic",
    name: "Anthropic",
    models: [
      { id: "claude-a", name: "Claude A", reasoning_levels: ["low", "high"], default_reasoning_effort: "low" },
      { id: "claude-b", name: "Claude B" },
    ],
  },
];

// GET …/config as the engine answers it: the coder's model type, the chosen
// model (none until one is picked) and the set-up provider.
function config(selected) {
  return {
    agents: { coder: { model: "large" } },
    models: selected ? { large: selected } : {},
    recent_models: {},
    providers: { anthropic: { id: "anthropic", name: "Anthropic", models: [] } },
  };
}

beforeEach(async () => {
  vi.resetModules();
  svelte = await import("svelte");
  state.posts = [];
  state.routes = {
    "GET providers": PROVIDERS,
    "GET agent": null,
    "GET agent/capabilities": { kind: "anthropic" },
  };
  orders.config = [];
  uiStore = await import("../../lib/stores/ui.svelte.js");
  ui = uiStore.ui;
  // The key router, so the menu's Escape binding is reachable like in the app.
  (await import("../../lib/keys.js")).startKeys(window);
  ModelMenu = (await import("../models/ModelMenu.svelte")).default;
});

// jsdom has no Web Animations: the menu's unfold transition gets a stub that
// finishes at once, as a browser without animation would.
if (typeof Element.prototype.animate !== "function") {
  Element.prototype.animate = function animate() {
    const animation = {
      cancel() {},
      finish() {},
      onfinish: null,
      oncancel: null,
      playState: "finished",
    };
    setTimeout(() => animation.onfinish?.(), 0);
    return animation;
  };
}

afterEach(() => {
  if (host) svelte.unmount(host);
  host = null;
  document.body.replaceChildren();
});

// The menu drawn while ui.menu holds the model button's, as OverlayHost draws it.
function mount() {
  const anchor = document.createElement("button");
  document.body.appendChild(anchor);
  uiStore.openMenu({ kind: "model", anchor });
  host = svelte.mount(ModelMenu, { target: document.body, props: { menu: ui.menu } });
}

const modelRows = () => [...document.querySelectorAll('.model-menu [role="option"].model')];
const levelButtons = () => [...document.querySelectorAll('.model-menu [data-column="effort"] .level')];

// Waits for the data reads and the choice's own writes to land: a microtask
// turn for each promise, then the effects that follow it.
async function settle(turns = 40) {
  for (let i = 0; i < turns; i += 1) {
    await Promise.resolve();
    svelte.flushSync();
  }
}

describe("the model menu (ModelMenu.svelte)", () => {
  it("reads the engine's config and catalog and shows the models", async () => {
    state.routes["GET config"] = config();
    mount();
    await settle();
    expect(modelRows().map((row) => row.textContent.trim())).toEqual(["Claude A", "Claude B"]);
    expect(levelButtons()).toEqual([]);
  });

  it("keeps the menu open after a choice, so the effort level can be picked too", async () => {
    // The first read has nothing chosen; after the choice the engine answers
    // with the saved model, as the engine's config write would.
    orders.config = [config(), config({ provider: "anthropic", model: "claude-a", reasoning_effort: "low" })];
    mount();
    await settle();
    expect(levelButtons()).toEqual([]);

    modelRows()[0].click();
    await settle();

    // The picker stays open (owner, 2026-10-22): the chosen row keeps its
    // check and the model's effort levels are under the list.
    expect(ui.menu?.kind).toBe("model");
    expect(modelRows()[0].getAttribute("aria-selected")).toBe("true");
    expect(levelButtons().map((level) => level.textContent.trim())).toEqual(["Low", "High"]);
    expect(levelButtons()[0].getAttribute("aria-checked")).toBe("true");

    // The choice was saved for the coder's model type.
    expect(state.posts.find((post) => post.path === "config/model").body).toEqual({
      scope: 0,
      model_type: "large",
      model: { model: "claude-a", provider: "anthropic", reasoning_effort: "low" },
    });

    // An effort level is picked in the same open menu and saved with the
    // model now in force.
    state.routes["GET config"] = config({ provider: "anthropic", model: "claude-a", reasoning_effort: "low" });
    levelButtons()[1].click();
    await settle();

    expect(ui.menu?.kind).toBe("model");
    expect(state.posts.filter((post) => post.path === "config/model").at(-1).body.model.reasoning_effort).toBe("high");
    expect(levelButtons()[1].getAttribute("aria-checked")).toBe("true");
  });

  it("the arrow keys reach the effort levels, so one is picked with the keyboard too", async () => {
    state.routes["GET config"] = config({ provider: "anthropic", model: "claude-a", reasoning_effort: "low" });
    mount();
    await settle();

    modelRows()[0].focus();
    const press = (key) => window.dispatchEvent(new window.KeyboardEvent("keydown", { key, bubbles: true, cancelable: true }));
    press("ArrowRight");
    expect(document.activeElement).toBe(levelButtons()[0]); // the level in force

    levelButtons()[1].click();
    await settle();
    expect(state.posts.filter((post) => post.path === "config/model").at(-1).body.model.reasoning_effort).toBe("high");
    expect(ui.menu?.kind).toBe("model");
  });

  it("a click outside, or Escape, closes it", async () => {
    state.routes["GET config"] = config();
    mount();
    await settle();
    document.body.dispatchEvent(new window.MouseEvent("pointerdown", { bubbles: true }));
    expect(ui.menu).toBe(null);

    mount();
    await settle();
    window.dispatchEvent(new window.KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }));
    expect(ui.menu).toBe(null);
  });
});
