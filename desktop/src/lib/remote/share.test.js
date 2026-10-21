import { beforeEach, describe, expect, it, vi } from "vitest";

// Phone sharing (desktop-design-5-6 2.27): one share per chat, whichever app
// turned it on (owner, 2026-10-21). A share that came up after Cancel must be
// stopped, but not when a newer start wants it or the chat was shared
// already (review t90 finding 2); the window follows every chat's share and
// stops the shown one's; a phone request made for one chat must never run in
// another (finding 3).
const api = vi.hoisted(() => ({
  startRemote: vi.fn(),
  stopRemote: vi.fn(() => Promise.resolve()),
  remoteShares: vi.fn(),
  remoteActionDone: vi.fn(() => Promise.resolve()),
}));
const notify = vi.hoisted(() => vi.fn());
const sessions = vi.hoisted(() => ({ currentId: "s1" }));
const runAction = vi.hoisted(() => vi.fn());
const ensureSession = vi.hoisted(() => vi.fn());
const busy = vi.hoisted(() => ({ value: false }));
vi.mock("../engine/api.js", () => ({ api }));
vi.mock("../dialogs.js", () => ({ openDialog: vi.fn(), closeDialog: vi.fn(), hasDialog: vi.fn(() => false) }));
vi.mock("../stores/composer.svelte.js", () => ({ composer: { input: "" } }));
vi.mock("../stores/connection.svelte.js", () => ({ conn: { state: "connected" } }));
vi.mock("../stores/ui.svelte.js", () => ({ notify }));
vi.mock("../stores/sessions.svelte.js", () => ({ sessions, ensureSession }));
vi.mock("../actions.js", () => ({ runAction }));
vi.mock("../busy.js", () => ({ windowBusy: () => busy.value }));
vi.mock("./presence.js", () => ({ resetPresence: vi.fn(), sendPresence: vi.fn() }));

const { cancelStart, chatClosed, isShown, loadShares, onStatus, startShare, stopShare } = await import("./share.js");
const { share } = await import("./store.svelte.js");
const { runRemoteAction } = await import("./actions.js");

function deferred() {
  let resolve;
  const promise = new Promise((r) => (resolve = r));
  return { promise, resolve };
}

const on = (chat, phones = []) => ({ state: "on", session_id: chat, host: "pc", port: 7373, phones });

beforeEach(() => {
  vi.clearAllMocks();
  share.state = "off";
  share.shares = {};
  sessions.currentId = "s1";
  busy.value = false;
});

describe("start and Cancel", () => {
  it("starts the shown chat's share, and stops one that came up after Cancel", async () => {
    const late = deferred();
    api.startRemote.mockReturnValueOnce(late.promise);
    const started = startShare();
    expect(api.startRemote).toHaveBeenCalledWith({ session_id: "s1" });
    cancelStart();
    late.resolve(on("s1"));
    await started;
    expect(api.stopRemote).toHaveBeenCalledWith("s1");
    expect(share.state).toBe("off");
    expect(isShown()).toBe(false);
  });

  it("keeps the share when a newer start wants it", async () => {
    const first = deferred();
    const second = deferred();
    api.startRemote.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    const a = startShare();
    cancelStart();
    const b = startShare();
    first.resolve(on("s1"));
    second.resolve(on("s1"));
    await Promise.all([a, b]);
    expect(api.stopRemote).not.toHaveBeenCalled();
    expect(isShown()).toBe(true);
  });

  it("leaves a share another app turned on when Cancel drops a start", async () => {
    onStatus(on("s1"));
    const late = deferred();
    api.startRemote.mockReturnValueOnce(late.promise);
    const started = startShare();
    cancelStart();
    late.resolve(on("s1"));
    await started;
    expect(api.stopRemote).not.toHaveBeenCalled();
    expect(isShown()).toBe(true);
  });

  it("starts the new chat's share as the empty chat", async () => {
    sessions.currentId = null;
    api.startRemote.mockResolvedValueOnce(on(""));
    await startShare({ launchId: "L1", quiet: true });
    expect(api.startRemote).toHaveBeenCalledWith({ session_id: "", launch_id: "L1" });
    expect(isShown()).toBe(true);
  });
});

describe("every chat's share", () => {
  it("follows the engine's shares from any app, and stops the shown one's", async () => {
    onStatus(on("s2"));
    expect(isShown()).toBe(false);
    onStatus(on("s1"));
    expect(isShown()).toBe(true);
    onStatus(on("s1", ["Pixel"]));
    expect(notify).toHaveBeenCalledWith("info", "Remote Control: Pixel connected");
    await stopShare();
    expect(api.stopRemote).toHaveBeenCalledWith("s1");
    expect(isShown()).toBe(false);
    expect(share.shares.s2).toBeTruthy();
    onStatus({ state: "off", session_id: "s2" });
    expect(share.shares).toEqual({});
  });

  it("does not let a list read before a change undo it", async () => {
    onStatus(on("s2"));
    const list = deferred();
    api.remoteShares.mockReturnValueOnce(list.promise);
    const loading = loadShares("ws-1");
    onStatus(on("s1"));
    onStatus({ state: "off", session_id: "s3" });
    list.resolve([on("s3"), on("s4")]);
    await loading;
    expect(Object.keys(share.shares).sort()).toEqual(["s1", "s4"]);
  });

  it("stops a closed chat's share, and nothing for a chat without one", () => {
    onStatus(on("s3"));
    chatClosed("s3");
    chatClosed("s4");
    expect(api.stopRemote).toHaveBeenCalledTimes(1);
    expect(api.stopRemote).toHaveBeenCalledWith("s3");
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
