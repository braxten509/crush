import { beforeEach, describe, expect, it, vi } from "vitest";

const calls = [];
const windowClose = vi.fn(async () => calls.push("window_close"));
// window_close_hold, as Rust answers it: asking, the other windows open.
let others = 0;
const holds = []; // what reached Rust, in order, with the engine reads
const windowCloseHold = vi.fn(async (asking, going) => {
  holds.push(asking ? "hold" : `release${going ? "+closing" : ""}`);
  return asking ? others : 0;
});
let requested = null;
vi.mock("./engine/commands.js", () => ({ windowClose, windowCloseHold }));
// The engines' running work, by folder ("" the shown one); none by default.
let work = {};
const engine = vi.fn(async (dir = "") => {
  holds.push("engine");
  return work[dir] ?? { pid: 1 };
});
vi.mock("./engine/api.js", () => ({ api: { engine } }));
vi.mock("./stores/connection.svelte.js", () => ({ conn: { folders: { "/shown": {}, "/b": {} } }, inFolder: (dir) => dir === "/shown" }));
// The close sheet: its props, to answer it.
let sheet = null;
const openDialog = vi.fn((id, props) => {
  sheet = { id, ...props };
  return true;
});
vi.mock("./dialogs.js", () => ({ dialogRegistered: () => true, openDialog }));
vi.mock("@tauri-apps/api/webviewWindow", () => ({
  getCurrentWebviewWindow: () => ({
    listen: async (name, fn) => {
      if (name === "close-requested") requested = fn;
      return () => {};
    },
  }),
}));

let closing;

beforeEach(async () => {
  vi.resetModules();
  calls.length = 0;
  windowClose.mockClear();
  others = 0;
  holds.length = 0;
  windowCloseHold.mockClear();
  openDialog.mockClear();
  work = {};
  sheet = null;
  requested = null;
  closing = await import("./closing.js");
});

describe("closing the window (desktop-design-5-6 2.23, 6.4)", () => {
  it("runs the steps by order, each awaited, then window_close", async () => {
    closing.registerCloseStep({ id: "late", order: 20, run: async () => calls.push("late") });
    closing.registerCloseStep({
      id: "early",
      order: 10,
      run: () => new Promise((done) => setTimeout(() => done(calls.push("early")), 5)),
    });
    await closing.closeWindow();
    expect(calls).toEqual(["early", "late", "window_close"]);
  });

  it("logs and skips a failing step", async () => {
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    closing.registerCloseStep({ id: "bad", order: 1, run: async () => { throw new Error("boom"); } });
    closing.registerCloseStep({ id: "good", order: 2, run: async () => calls.push("good") });
    await closing.closeWindow();
    expect(calls).toEqual(["good", "window_close"]);
    expect(error).toHaveBeenCalledWith('close step "bad" failed', "boom");
    error.mockRestore();
  });

  it("runs once: a second call waits for the first", async () => {
    const run = vi.fn(async () => calls.push("step"));
    closing.registerCloseStep({ id: "one", order: 1, run });
    const first = closing.closeWindow();
    const second = closing.closeWindow();
    expect(second).toBe(first);
    await second;
    expect(run).toHaveBeenCalledTimes(1);
    expect(windowClose).toHaveBeenCalledTimes(1);
  });

  it("replaces a step registered again under its id, and unregisters", async () => {
    const off = closing.registerCloseStep({ id: "s", order: 1, run: async () => calls.push("old") });
    closing.registerCloseStep({ id: "s", order: 1, run: async () => calls.push("new") });
    off(); // the old registration's unregister leaves the new one
    const offGone = closing.registerCloseStep({ id: "gone", order: 2, run: async () => calls.push("gone") });
    offGone();
    await closing.closeWindow();
    expect(calls).toEqual(["new", "window_close"]);
  });

  it("closes on the window's close-requested event", async () => {
    await Promise.resolve();
    expect(typeof requested).toBe("function");
    await requested();
    expect(windowClose).toHaveBeenCalledTimes(1);
  });

  it("asks first only when the last window closes with work running; Cancel keeps it open", async () => {
    closing.registerCloseStep({ id: "step", order: 1, run: async () => calls.push("step") });
    // Another window stays open: no question, the window closes; Rust's
    // fallback is held while that is decided, then armed again.
    work = { "": { pid: 1, busy_chats: 2 } };
    others = 1;
    await closing.closeWindow();
    expect(openDialog).not.toHaveBeenCalled();
    expect(calls).toEqual(["step", "window_close"]);
    expect(holds).toEqual(["hold", "release+closing"]);

    // The last window, with 2 chats on the shown folder's engine and a
    // sub-agent on a joined folder's: the sheet asks, and holds Rust's
    // fallback off while it waits.
    vi.resetModules();
    closing = await import("./closing.js");
    closing.registerCloseStep({ id: "step", order: 1, run: async () => calls.push("step") });
    calls.length = 0;
    windowClose.mockClear();
    holds.length = 0;
    others = 0;
    work = { "": { pid: 1, busy_chats: 2, running_helpers: 0 }, "/b": { pid: 2, busy_chats: 0, running_helpers: 1 } };
    const first = closing.closeWindow();
    await vi.waitFor(() => expect(sheet).not.toBeNull());
    expect(sheet).toMatchObject({ id: "close", chats: 2, helpers: 1 });
    expect(holds).toEqual(["hold", "engine", "engine"], "held before any engine is read");
    sheet.answer("cancel");
    await first;
    expect(calls).toEqual([]);
    expect(windowClose).not.toHaveBeenCalled();
    expect(holds.at(-1)).toBe("release", "a window that stays gets no fallback");

    // Cancel left the window closable: the next close asks again; Stop
    // engine runs the steps, then closes stopping both engines.
    sheet = null;
    const second = closing.closeWindow();
    expect(second).not.toBe(first);
    await vi.waitFor(() => expect(sheet).not.toBeNull());
    sheet.answer("stop");
    await second;
    expect(calls).toEqual(["step", "window_close"]);
    expect(windowClose).toHaveBeenCalledWith(["", "/b"]);
  });
});
