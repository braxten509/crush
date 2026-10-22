import { afterEach, beforeEach, expect, it, vi } from "vitest";

const h = vi.hoisted(() => ({
  conn: { state: "connected", clientId: "first" },
  open: { list: [{ id: "chat", dir: "/project" }] },
  read: vi.fn(), upsert: vi.fn(),
}));
vi.mock("../engine/api.js", () => ({ api: { getSession: h.read } }));
vi.mock("../stores/connection.svelte.js", () => ({ conn: h.conn, inFolder: (dir) => dir === "/project" }));
vi.mock("../stores/openchats.svelte.js", () => ({ openChats: h.open }));
vi.mock("../stores/sessions.svelte.js", () => ({
  sessions: { byId: { chat: { id: "chat" } }, order: ["chat"] },
  busy: () => false, readStart: () => 1, rootOf: (id) => id, upsertSession: h.upsert,
}));

beforeEach(() => {
  vi.resetModules();
  vi.useFakeTimers();
  h.conn.clientId = "first";
  h.read.mockReset();
  h.upsert.mockReset();
});
afterEach(() => vi.useRealTimers());

it("ignores a status reply from a previous join, then accepts a fresh read", async () => {
  let answer;
  h.read.mockImplementationOnce(() => new Promise((resolve) => { answer = resolve; }));
  const states = await import("./states.js");
  states.readSoon("chat");
  await vi.advanceTimersByTimeAsync(0);
  expect(h.read).toHaveBeenCalledTimes(1);
  h.conn.clientId = "second";
  answer({ id: "chat", has_unfinished_work: false });
  await vi.advanceTimersByTimeAsync(0);
  expect(h.upsert).not.toHaveBeenCalled();
  h.read.mockResolvedValueOnce({ id: "chat", has_unfinished_work: true });
  states.readSoon("chat");
  await vi.advanceTimersByTimeAsync(500);
  expect(h.upsert).toHaveBeenCalledWith({ id: "chat", has_unfinished_work: true }, { since: 1 });
});
