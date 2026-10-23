import { beforeEach, expect, it, vi } from "vitest";

const h = vi.hoisted(() => ({
  conn: { clientId: "old" },
  list: { servers: [], serversClientId: "", openFolds: {} },
  read: vi.fn(),
}));
vi.mock("../engine/api.js", () => ({ api: { servers: h.read } }));
vi.mock("../engine/commands.js", () => ({ open_link: vi.fn() }));
vi.mock("../stores/connection.svelte.js", () => ({ conn: h.conn }));
vi.mock("../stores/sessions.svelte.js", () => ({ sessions: { order: [] }, rootOf: (id) => id }));
vi.mock("./store.svelte.js", () => ({ leftList: h.list }));

beforeEach(() => {
  vi.resetModules();
  h.conn.clientId = "old";
  h.list.servers = [];
  h.list.serversClientId = "";
  h.read.mockReset();
});

it("hides the previous folder's servers immediately and rejects its late reply", async () => {
  const servers = await import("./servers.js");
  h.read.mockResolvedValueOnce([{ port: 5189, session_id: "old-chat" }]);
  await servers.readServers();
  expect(servers.visibleServers().map((s) => s.port)).toEqual([5189]);
  let finishOld;
  h.read.mockImplementationOnce(() => new Promise((resolve) => { finishOld = resolve; }));
  const pending = servers.readServers();
  h.conn.clientId = "new";
  expect(servers.visibleServers()).toEqual([]); // no timer or new request needed
  finishOld([{ port: 5190, session_id: "old-chat" }]);
  await pending;
  expect(servers.visibleServers()).toEqual([]);
  h.read.mockResolvedValueOnce([{ port: 6000, session_id: "new-chat" }]);
  await servers.readServers();
  expect(servers.visibleServers().map((s) => s.port)).toEqual([6000]);
});
