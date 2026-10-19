import { beforeEach, describe, expect, it, vi } from "vitest";

const calls = [];
const windowClose = vi.fn(async () => calls.push("window_close"));
let requested = null;
vi.mock("./engine/commands.js", () => ({ windowClose }));
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
});
