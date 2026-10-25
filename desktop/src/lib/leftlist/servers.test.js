import { beforeEach, expect, it, vi } from "vitest";

const h = vi.hoisted(() => ({
  conn: { clientId: "old", folders: {} },
  list: { servers: [], serversClientId: "", openFolds: {} },
  read: vi.fn(),
  kill: vi.fn(),
}));
vi.mock("../engine/api.js", () => ({ api: { servers: h.read, killJob: h.kill } }));
vi.mock("../engine/commands.js", () => ({ open_link: vi.fn() }));
vi.mock("../stores/connection.svelte.js", () => ({ conn: h.conn, inFolder: (dir) => dir === "/shown" }));
vi.mock("../stores/sessions.svelte.js", () => ({ sessions: { order: [] }, rootOf: (id) => id }));
vi.mock("./store.svelte.js", () => ({ leftList: h.list }));

beforeEach(() => {
  vi.resetModules();
  h.conn.clientId = "old";
  h.conn.folders = {};
  h.list.servers = [];
  h.list.serversClientId = "";
  h.read.mockReset();
  h.kill.mockReset();
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

// Servers are not per folder (owner, 2026-10-25): every folder the window
// keeps joined lists its servers, and a stop goes to the server's own folder.
it("lists every joined folder's servers and stops each in its own folder", async () => {
  const servers = await import("./servers.js");
  h.conn.folders = { "/shown": {}, "/other": {} };
  h.read.mockImplementation(async (dir) => (dir === "/other" ? [{ port: 7000, session_id: "b", job_id: "j2" }] : [{ port: 5173, session_id: "a", job_id: "j1" }]));
  h.kill.mockResolvedValue({});
  await servers.readServers();
  expect(servers.visibleServers().map((s) => s.port).sort()).toEqual([5173, 7000]);
  servers.stopServer(servers.visibleServers().find((s) => s.port === 7000));
  expect(h.kill).toHaveBeenCalledWith("b", { job_id: "j2" }, "/other");
  servers.stopServer(servers.visibleServers().find((s) => s.port === 5173));
  expect(h.kill).toHaveBeenCalledWith("a", { job_id: "j1" }, "");
});
