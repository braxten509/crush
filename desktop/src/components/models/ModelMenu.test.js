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
  api: vi.fn(async (method, path, _query, body, _callId, dir) => {
    if (method === "POST") state.posts.push({ path, body: body ?? {} });
    if (method === "POST" && path === "config/model") {
      const cfg = state.routes["GET config"] ?? {};
      state.routes["GET config"] = { ...cfg, models: { ...cfg.models, [body.model_type]: body.model } };
    }
    // The engine answers the next read with the pick it saved.
    if (method === "POST" && path === "last-pick") state.routes["GET last-pick"] = body;
    const key = `${method} ${path}`;
    // A queued answer serves the config reads in order, so a choice's own
    // refresh sees the model the engine just saved.
    if (key === "GET config" && orders.config.length > 0) return orders.config.shift();
    const route = state.routes[key];
    return typeof route === "function" ? route(body, dir) : route ?? null;
  }),
  events_take: vi.fn(async () => []),
  api_abort: vi.fn(async () => true),
}));

let svelte;
let ModelMenu;
let availabilityStore;
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

// GET …/config as the engine answers it: the helper model and the set-up
// provider. Every chat owns its model; with no chat open the menu shows the
// last pick (GET …/last-pick), which selected stands for here.
function config(selected) {
  state.routes["GET last-pick"] = selected ?? null;
  return {
    models: { small: { provider: "anthropic", model: "claude-b" } },
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
  availabilityStore = (await import("../../lib/stores/availability.svelte.js")).availability;
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

  it("dims a provider that cannot run in this folder, and says why", async () => {
    state.routes["GET config"] = config();
    availabilityStore.byProvider = { anthropic: "Moonshot AI is not usable here: this folder is not a git project." };
    state.routes["GET providers/availability"] = { providers: availabilityStore.byProvider };
    mount();
    await settle();
    const rows = modelRows();
    expect(rows.every((row) => row.disabled)).toBe(true);
    expect(rows[0].textContent).toContain("this folder is not a git project.");
    availabilityStore.byProvider = {};
  });

  it("checks the shown chat and ignores a late home-folder reply", async () => {
    state.routes["GET config"] = config();
    const { setChatFolder } = await import("../../lib/engine/api.js");
    const { loadAvailability } = await import("../../lib/stores/availability.svelte.js");
    const { sessions } = await import("../../lib/stores/sessions.svelte.js");
    const home = { providers: { anthropic: "Moonshot AI is not usable here: this is your home folder" } };
    state.routes["GET providers/availability"] = home;
    await loadAvailability();
    let finishHome;
    state.routes["GET providers/availability"] = (_body, dir) => dir === "/home/test/dev/crush"
      ? { providers: {} }
      : new Promise((resolve) => { finishHome = resolve; });
    const pending = loadAvailability();
    setChatFolder((sid) => sid === "project" ? { dir: "/home/test/dev/crush", joined: true } : null);
    sessions.currentId = "project";
    mount();
    await settle();
    expect(modelRows().every((row) => !row.disabled)).toBe(true);
    finishHome(home);
    await pending;
    await settle();
    expect(modelRows().every((row) => !row.disabled)).toBe(true);
    expect(document.querySelector(".model-menu").textContent).not.toContain("this is your home folder");

    // Switching back to a home chat must still enforce the restriction.
    state.routes["GET providers/availability"] = home;
    sessions.currentId = "home";
    await settle();
    expect(modelRows().every((row) => row.disabled)).toBe(true);
    expect(document.querySelector(".model-menu").textContent).toContain("this is your home folder");
  });

  it("keeps the menu open after a choice, so the effort level can be picked too", async () => {
    state.routes["GET config"] = config();
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

    // With no chat open the choice is the last pick a new chat starts on.
    expect(state.posts.find((post) => post.path === "last-pick").body).toEqual({ model: "claude-a", provider: "anthropic", reasoning_effort: "low" });

    // An effort level is picked in the same open menu and saved with the
    // model now in force.
    levelButtons()[1].click();
    await settle();

    expect(ui.menu?.kind).toBe("model");
    expect(state.posts.filter((post) => post.path === "last-pick").at(-1).body.reasoning_effort).toBe("high");
    expect(levelButtons()[1].getAttribute("aria-checked")).toBe("true");
  });

  it("a pick changes only this chat, and on the landing page only the last pick", async () => {
    state.routes["GET config"] = config();
    const { sessions } = await import("../../lib/stores/sessions.svelte.js");
    const claudeB = { provider: "anthropic", model: "claude-b" };
    sessions.currentId = "a";
    sessions.byId.a = { id: "a", model: { ...claudeB } };
    sessions.byId.b = { id: "b", model: { ...claudeB } };
    mount();
    await settle();

    modelRows()[0].click();
    await settle();
    expect(state.posts.map((post) => post.path)).toEqual(["sessions/a/model"]);
    expect(sessions.byId.a.model.model).toBe("claude-a");
    expect(sessions.byId.b.model).toEqual(claudeB);

    state.posts = [];
    sessions.currentId = "";
    await settle();
    modelRows()[1].click();
    await settle();
    expect(state.posts.map((post) => post.path)).toEqual(["last-pick"]);
    expect(sessions.byId.b.model).toEqual(claudeB);

    // A chat deleted while its save is on its way leaves no record behind.
    let finish;
    state.routes["POST sessions/gone/model"] = () => new Promise((resolve) => { finish = resolve; });
    sessions.currentId = "gone";
    sessions.byId.gone = { id: "gone", model: { ...claudeB } };
    await settle();
    modelRows()[0].click();
    await settle();
    delete sessions.byId.gone;
    finish({});
    await settle();
    expect(sessions.byId.gone).toBeUndefined();
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
    expect(state.posts.filter((post) => post.path === "last-pick").at(-1).body.reasoning_effort).toBe("high");
    expect(ui.menu?.kind).toBe("model");
  });

  it("browses providers without saving and leaves the current model's effort alone", async () => {
    const own = { provider: "anthropic", model: "claude-a", reasoning_effort: "high" };
    state.routes["GET config"] = { ...config(own), model_efforts: { "anthropic:claude-a": "low" } };
    state.routes["GET providers"] = [...PROVIDERS, { id: "second", name: "Second", models: [{ id: "other", name: "Other" }] }];
    const { sessions } = await import("../../lib/stores/sessions.svelte.js");
    sessions.currentId = "here";
    sessions.byId.here = { id: "here", model: own };
    mount();
    await settle();
    const group = (name) => [...document.querySelectorAll('[data-column="groups"] button')].find((b) => b.textContent.trim() === name);
    group("Second").click();
    await settle();
    modelRows()[0].focus();
    group("Anthropic").click();
    await settle();
    expect(state.posts).toEqual([]);
    modelRows()[0].click();
    await settle();
    expect(state.posts).toEqual([]);
    expect(sessions.byId.here.model).toEqual(own);
  });

  it("saves effort on this chat's model while another chat has another model", async () => {
    const other = { provider: "anthropic", model: "claude-b" };
    state.routes["GET config"] = config(other);
    const { sessions } = await import("../../lib/stores/sessions.svelte.js");
    sessions.currentId = "here";
    sessions.byId.here = { id: "here", model: { provider: "anthropic", model: "claude-a", reasoning_effort: "low" } };
    sessions.byId.there = { id: "there", model: other };
    mount();
    await settle();
    levelButtons()[1].click();
    await settle();
    const expected = { provider: "anthropic", model: "claude-a", reasoning_effort: "high" };
    expect(state.posts.find((p) => p.path === "sessions/here/model").body.model).toEqual(expected);
    expect(sessions.byId.here.model).toEqual(expected);
    expect(sessions.byId.there.model).toEqual(other);
    expect(levelButtons()[1].getAttribute("aria-checked")).toBe("true");
  });

  it("reports a failed chat save without writing any other model or claiming success", async () => {
    const own = { provider: "anthropic", model: "claude-a", reasoning_effort: "low" };
    state.routes["GET config"] = config(own);
    state.routes["POST sessions/here/model"] = () => { throw new Error("chat save refused"); };
    const { sessions } = await import("../../lib/stores/sessions.svelte.js");
    sessions.currentId = "here";
    sessions.byId.here = { id: "here", model: own };
    mount();
    await settle();
    levelButtons()[1].click();
    await settle();
    expect(state.posts.some((p) => p.path === "config/model" || p.path === "last-pick" || p.path === "agent/update")).toBe(false);
    expect(sessions.byId.here.model).toEqual(own);
    expect(levelButtons()[0].getAttribute("aria-checked")).toBe("true");
    expect(JSON.stringify(ui)).toContain("chat save refused");
  });

  it("shows a newly chosen model's effort instead of the previous model's clicked effort", async () => {
    const own = { provider: "anthropic", model: "claude-a", reasoning_effort: "low" };
    const providers = structuredClone(PROVIDERS);
    providers[0].models[1] = { id: "claude-b", name: "Claude B", reasoning_levels: ["low", "high"], default_reasoning_effort: "low" };
    state.routes["GET providers"] = providers;
    state.routes["GET config"] = config(own);
    const { sessions } = await import("../../lib/stores/sessions.svelte.js");
    sessions.currentId = "here";
    sessions.byId.here = { id: "here", model: own };
    mount();
    await settle();
    levelButtons()[1].click();
    await settle();
    modelRows()[1].click();
    await settle();
    expect(sessions.byId.here.model.model).toBe("claude-b");
    expect(levelButtons()[0].getAttribute("aria-checked")).toBe("true");
    levelButtons()[1].click();
    await settle();
    expect(sessions.byId.here.model.reasoning_effort).toBe("high");
  });

  it("draws the app's scrollbar on both of its lists", async () => {
    state.routes["GET config"] = config();
    mount();
    await settle();
    expect(document.querySelector(".model-menu .groups").classList.contains("kit-scroll")).toBe(true);
    expect(document.querySelector(".model-menu .rows").classList.contains("kit-scroll")).toBe(true);
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
