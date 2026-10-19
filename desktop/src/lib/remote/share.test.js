import { beforeEach, describe, expect, it, vi } from "vitest";

// Phone sharing's two fixed bugs (desktop-design-5-6 2.27; review t90
// findings 2 and 3): a share that came up after Cancel must be stopped, but
// not when a newer start wants it; and a phone request made for one chat
// must never run in another.
const api = vi.hoisted(() => ({ startRemote: vi.fn(), stopRemote: vi.fn(() => Promise.resolve()), remoteActionDone: vi.fn(() => Promise.resolve()) }));
const sessions = vi.hoisted(() => ({ currentId: "s1" }));
const runAction = vi.hoisted(() => vi.fn());
const ensureSession = vi.hoisted(() => vi.fn());
const busy = vi.hoisted(() => ({ value: false }));
vi.mock("../engine/api.js", () => ({ api }));
vi.mock("../dialogs.js", () => ({ openDialog: vi.fn(), closeDialog: vi.fn(), hasDialog: vi.fn(() => false) }));
vi.mock("../stores/composer.svelte.js", () => ({ setBadge: vi.fn(), composer: { input: "" } }));
vi.mock("../stores/ui.svelte.js", () => ({ notify: vi.fn() }));
vi.mock("../stores/sessions.svelte.js", () => ({ sessions, ensureSession }));
vi.mock("../actions.js", () => ({ runAction }));
vi.mock("../busy.js", () => ({ windowBusy: () => busy.value }));
vi.mock("./presence.js", () => ({ resetPresence: vi.fn(), sendPresence: vi.fn() }));

const { cancelStart, startShare } = await import("./share.js");
const { share } = await import("./store.svelte.js");
const { runRemoteAction } = await import("./actions.js");

function deferred() {
  let resolve;
  const promise = new Promise((r) => (resolve = r));
  return { promise, resolve };
}

beforeEach(() => {
  vi.clearAllMocks();
  share.state = "off";
  sessions.currentId = "s1";
  busy.value = false;
});

describe("start and Cancel", () => {
  it("stops a share that came up after Cancel", async () => {
    const late = deferred();
    api.startRemote.mockReturnValueOnce(late.promise);
    const started = startShare();
    cancelStart();
    late.resolve({ state: "on" });
    await started;
    expect(api.stopRemote).toHaveBeenCalledTimes(1);
    expect(share.state).toBe("off");
  });

  it("keeps the share when a newer start wants it", async () => {
    const first = deferred();
    const second = deferred();
    api.startRemote.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    const a = startShare();
    cancelStart();
    const b = startShare();
    first.resolve({ state: "on" });
    second.resolve({ state: "on", phones: [] });
    await Promise.all([a, b]);
    expect(api.stopRemote).not.toHaveBeenCalled();
    expect(share.state).toBe("on");
  });
});

describe("phone requests", () => {
  it("refuses a request made for another chat, and runs nothing", async () => {
    await runRemoteAction({ action_id: "a1", kind: "yolo", session_id: "s0" });
    expect(runAction).not.toHaveBeenCalled();
    expect(api.remoteActionDone).toHaveBeenCalledWith("a1", { error: "the Crush window switched to another chat" });
  });

  it("refuses a new chat when the window switched while it was made", async () => {
    sessions.currentId = "";
    ensureSession.mockImplementationOnce(async () => {
      sessions.currentId = "other";
      return "new";
    });
    await runRemoteAction({ action_id: "a2", kind: "new_chat_send", session_id: "" });
    expect(api.remoteActionDone).toHaveBeenCalledWith("a2", { error: "the Crush window switched to another chat" });
  });

  it("switches mode only when the window is not busy", async () => {
    busy.value = true;
    await runRemoteAction({ action_id: "a3", kind: "mode", session_id: "s1" });
    expect(runAction).not.toHaveBeenCalled();
    busy.value = false;
    await runRemoteAction({ action_id: "a4", kind: "mode", session_id: "s1" });
    expect(runAction).toHaveBeenCalledWith("mode.cycle");
    expect(api.remoteActionDone).toHaveBeenLastCalledWith("a4", {});
  });
});
