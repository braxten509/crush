import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The API key sheet (ApiKey.svelte), owner 2026-10-28: the key is typed in a
// password field, and a key that checks out is saved and the sheet closes
// with no further key press. The engine's verify and save routes are faked.
const state = vi.hoisted(() => ({ calls: [], ok: true }));

vi.mock("@tauri-apps/api/core", () => ({ invoke: vi.fn(async () => null) }));
vi.mock("@tauri-apps/api/webviewWindow", () => ({
  getCurrentWebviewWindow: () => ({ listen: async () => () => {} }),
}));
vi.mock("../../lib/engine/commands.js", () => ({
  api: vi.fn(async () => null),
  apiSecret: vi.fn(async (path, body) => {
    state.calls.push(path);
    return path === "auth/verify-key" ? { ok: state.ok } : {};
  }),
  events_take: vi.fn(async () => []),
  api_abort: vi.fn(async () => true),
}));
vi.mock("../../lib/signin/flows.js", async (original) => ({ ...(await original()), MIN_VERIFY_MS: 0 }));

if (typeof Element.prototype.animate !== "function") {
  Element.prototype.animate = function animate() {
    const animation = { cancel() {}, finish() {}, onfinish: null, oncancel: null, playState: "finished" };
    setTimeout(() => animation.onfinish?.(), 0);
    return animation;
  };
}
globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

let svelte;
let dialogs;
let sheetKey;
const hosts = [];

beforeEach(async () => {
  vi.resetModules();
  svelte = await import("svelte");
  dialogs = await import("../../lib/dialogs.js");
  ({ sheetKey } = await import("../../lib/kit/sheetkeys.js"));
  state.calls = [];
  state.ok = true;
});

afterEach(() => {
  while (hosts.length) svelte.unmount(hosts.pop());
  document.body.replaceChildren();
});

async function settle(turns = 40) {
  for (let i = 0; i < turns; i += 1) {
    await new Promise((resolve) => setTimeout(resolve, 0));
    svelte.flushSync();
  }
}

async function openWithKey(text) {
  const onDone = vi.fn();
  const props = { providerId: "orcarouter", providerName: "OrcaRouter", onDone };
  dialogs.registerDialog("api_key_input", (await import("./ApiKey.svelte")).default);
  dialogs.openDialog("api_key_input", props);
  hosts.push(svelte.mount((await import("./ApiKey.svelte")).default, { target: document.body, props }));
  await settle(2);
  const input = document.querySelector('[data-sheet="api_key_input"] input');
  input.value = text;
  input.dispatchEvent(new Event("input", { bubbles: true }));
  svelte.flushSync();
  return { input, onDone };
}

const open = () => dialogs.dialogs.map((d) => d.id);

describe("the API key sheet (ApiKey.svelte)", () => {
  it("hides the key and saves and closes once it checks out", async () => {
    const { input, onDone } = await openWithKey("sk-secret");
    expect(input.type).toBe("password");

    sheetKey(new KeyboardEvent("keydown", { key: "Enter" }));
    await settle();

    expect(state.calls).toEqual(["auth/verify-key", "config/provider-key"]);
    expect(open()).not.toContain("api_key_input");
    expect(onDone).toHaveBeenCalledWith("done");
  });

  it("keeps a wrong key's sheet open, unsaved", async () => {
    state.ok = false;
    const { onDone } = await openWithKey("sk-wrong");

    sheetKey(new KeyboardEvent("keydown", { key: "Enter" }));
    await settle();

    expect(state.calls).toEqual(["auth/verify-key"]);
    expect(open()).toContain("api_key_input");
    expect(onDone).not.toHaveBeenCalled();
  });
});
