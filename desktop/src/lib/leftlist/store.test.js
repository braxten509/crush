import { beforeEach, expect, it, vi } from "vitest";

const h = vi.hoisted(() => ({ switchFolder: vi.fn(), newChat: vi.fn(), conn: { project: { path: "/original" } } }));
vi.mock("../stores/connection.svelte.js", () => ({ switchFolder: h.switchFolder, conn: h.conn }));
vi.mock("../stores/sessions.svelte.js", () => ({ newChat: h.newChat }));
vi.mock("../engine/commands.js", () => ({ settings_set: vi.fn() }));
vi.mock("../terminal/tabs.svelte.js", () => ({ openTerminal: vi.fn() }));

const { startIn } = await import("./store.svelte.js");

beforeEach(() => {
  h.switchFolder.mockReset().mockImplementation(async (dir) => {
    h.conn.project.path = dir;
    return true;
  });
  h.newChat.mockReset().mockResolvedValue(true);
  h.conn.project.path = "/original";
});

it("folder + keeps the old chat shown while switching and returns the creation result", async () => {
  h.newChat.mockResolvedValue(false);
  expect(await startIn("session", "/other")).toBe(false);
  expect(h.switchFolder).toHaveBeenCalledWith("/other", { keepShown: true });
  expect(h.newChat).toHaveBeenCalledWith("/other");
  expect(h.switchFolder).toHaveBeenLastCalledWith("/original", { keepShown: true });
});

it("finishes saving one folder's chat before starting another folder's chat", async () => {
  let finish;
  h.newChat.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
  const first = startIn("session", "/first");
  await vi.waitFor(() => expect(h.newChat).toHaveBeenCalledWith("/first"));
  const second = startIn("session", "/second");
  await Promise.resolve();
  expect(h.switchFolder).not.toHaveBeenCalledWith("/second", expect.anything());
  finish(true);
  expect(await first).toBe(true);
  expect(await second).toBe(true);
  expect(h.newChat.mock.calls.map(([dir]) => dir)).toEqual(["/first", "/second"]);
});
