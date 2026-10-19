// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";

// Branch links, closed chats and trees (desktop-design 3.3, 1.5.3) on the
// real event loop (events.js) with fake Tauri calls, as sessions.test.js.
const h = vi.hoisted(() => ({ routes: new Map(), calls: [], entries: [], listeners: new Map(), settings: null, closedCalls: [], refuse: false }));

vi.mock("@tauri-apps/api/webviewWindow", () => ({
  getCurrentWebviewWindow: () => ({
    listen: async (name, fn) => {
      h.listeners.set(name, fn);
      return () => {};
    },
  }),
}));

vi.mock("../engine/commands.js", () => ({
  api: async (method, path, query, body, callId) => {
    h.calls.push({ method, path, query, body, callId });
    const route = h.routes.get(`${method} ${path}`);
    if (typeof route === "function") return route({ query, body, callId });
    if (route !== undefined) return route;
    if (method === "GET") return Promise.reject({ code: "not_found", message: `no answer for GET ${path}` });
    return null;
  },
  api_abort: async () => true,
  engine_set_current_session: async () => null,
  events_take: async () => h.entries.splice(0),
  lastClipboardWrite: () => null,
  settings_get: async () => structuredClone(h.settings),
  closed_set: async (project, sessionId, closed) => {
    h.closedCalls.push({ project, sessionId, closed });
    if (h.refuse) throw { code: "io", message: "disk full" };
    const list = new Set(h.settings.closed_chats[project] ?? []);
    if (closed) list.add(sessionId);
    else list.delete(sessionId);
    h.settings.closed_chats[project] = [...list];
    return structuredClone(h.settings);
  },
}));

const tick = () => new Promise((resolve) => setTimeout(resolve, 0));
async function settle(rounds = 10) {
  for (let i = 0; i < rounds; i += 1) await tick();
}
async function deliver(...entries) {
  h.entries.push(...entries);
  h.listeners.get("events-ready")?.();
  await settle();
}
const ev = (type, event, payload) => ({ kind: "event", type, event, payload });
const linkReads = () => h.calls.filter((c) => c.method === "GET" && c.path === "chat-links").length;

let L; // links store
let C; // closed store
let T; // tree store
let S; // sessions store
let M; // messages store
let buildRows;

const DATA = "/tmp/home/proj/.crush";

beforeEach(async () => {
  vi.resetModules();
  h.routes.clear();
  h.calls.length = 0;
  h.entries.length = 0;
  h.listeners.clear();
  h.closedCalls.length = 0;
  h.refuse = false;
  h.settings = { version: 1, crush_path: null, yolo: true, closed_chats: {} };
  h.routes.set("GET chat-links", []);
  vi.stubGlobal("requestAnimationFrame", (fn) => setTimeout(() => fn(0), 0));
  const events = await import("../engine/events.js");
  L = await import("./links.svelte.js");
  C = await import("./closed.svelte.js");
  T = await import("./tree.svelte.js");
  S = await import("./sessions.svelte.js");
  M = await import("./messages.svelte.js");
  ({ buildRows } = await import("../listmodel.js"));
  await events.startEvents();
  await deliver(ev("connection", "updated", { state: "connected", attempt: 0, resync: false, engine: { data_dir: DATA } }));
});

const chat = (id, extra = {}) => ({ id, title: `Chat ${id}`, parent_session_id: "", message_count: 0, created_at: 1, updated_at: 1, is_busy: false, attached_clients: 0, has_unfinished_work: false, pending_prompt: "", ...extra });
const user = (id, sid, text = "Please look at the login flow.") => ({ id, role: "user", session_id: sid, parts: [{ type: "text", data: { text } }], created_at: 1, updated_at: 1 });
// The origin link of branch `sid` on message `mid` of chat `parent`; origin false is a copy.
const link = (id, sid, parent, mid, { start = 19, end = 24, origin = true } = {}) => ({
  id,
  session_id: sid,
  parent_session_id: parent,
  source_message_id: mid,
  highlight_text: "login",
  highlight_start: start,
  highlight_end: end,
  origin,
  created_at: 2,
});

function openable(id, list = []) {
  h.routes.set(`GET sessions/${id}`, chat(id));
  h.routes.set(`GET sessions/${id}/history`, []);
  h.routes.set(`GET sessions/${id}/messages`, list);
}

async function withList(...records) {
  h.routes.set("GET sessions", records);
  await S.loadSessions();
}

const rows = (selectedId = "") =>
  buildRows(
    S.sessions.order.map((id) => S.sessions.byId[id]),
    { parentOf: L.parentOf, isClosed: C.isClosed, selectedId },
  );
const shape = (built) => built.rows.map((r) => `${r.id}@${r.depth}`);

describe("links (3.3)", () => {
  it("chat_link created is kept by id: twice is once, and it reaches its message's list", async () => {
    const one = link("l1", "b1", "s1", "m1");
    await deliver(ev("chat_link", "created", one), ev("chat_link", "created", one));
    expect(Object.keys(L.links.byId)).toEqual(["l1"]);
    expect(L.links.byParentMessage.m1.map((l) => l.id)).toEqual(["l1"]);
    expect(L.originOf("b1")?.id).toBe("l1");
  });

  it("a link shows only while its message is on the shown chat's path (PLAN.md:104)", async () => {
    await withList(chat("s1"));
    await deliver(ev("chat_link", "created", link("l1", "b1", "s1", "m1")));
    expect(L.linksFor("m1")).toEqual([]); // no chat holds m1 yet
    openable("s1", [user("m1", "s1")]);
    await S.openChat("s1");
    expect(L.linksFor("m1")).toEqual([{ kind: "branch-link", start: 19, end: 24, data: expect.objectContaining({ id: "l1" }) }]);
    // The tree moves away from m1, then back.
    h.routes.set("GET sessions/s1/messages", [user("m2", "s1", "Another path")]);
    await deliver(ev("tree_changed", "updated", { session_id: "s1", revision: 2 }));
    expect(L.linksFor("m1")).toEqual([]);
    h.routes.set("GET sessions/s1/messages", [user("m1", "s1")]);
    await deliver(ev("tree_changed", "updated", { session_id: "s1", revision: 3 }));
    expect(L.linksFor("m1")).toHaveLength(1);
  });

  it("parentOf follows origin links only; a deleted parent leaves its branches at the top", async () => {
    const origin = link("l1", "b1", "s1", "m1");
    const sibling = link("l2", "b2", "s1", "m1", { start: 0, end: 6 });
    // b1 copied m1, and with it the link to its sibling b2 (shown inside b1).
    const copied = { ...sibling, id: "l3", parent_session_id: "b1", source_message_id: "m1copy", origin: false };
    await deliver(ev("chat_link", "created", origin), ev("chat_link", "created", sibling), ev("chat_link", "created", copied));
    expect([L.parentOf("b1"), L.parentOf("b2"), L.parentOf("s1")]).toEqual(["s1", "s1", null]);
    expect(L.links.byParentMessage.m1copy.map((l) => l.session_id)).toEqual(["b2"]);
    // s1 is deleted: the engine's cascade took its links.
    h.routes.set("GET chat-links", []);
    await deliver(ev("session", "deleted", chat("s1")));
    expect([L.parentOf("b1"), L.parentOf("b2")]).toEqual([null, null]);
    expect(L.links.byId).toEqual({});
  });

  it("reads the list on the first join, after a session deleted event and on a resync; nothing else reads it", async () => {
    expect(linkReads()).toBe(1); // the join in beforeEach
    await deliver(ev("session", "updated", chat("s1")), ev("chat_link", "created", link("l1", "b1", "s1", "m1")), ev("message", "updated", user("m1", "s1")));
    expect(linkReads()).toBe(1);
    await deliver(ev("session", "deleted", chat("s9")));
    expect(linkReads()).toBe(2);
    await deliver({ kind: "resync" });
    expect(linkReads()).toBe(3);
  });

  it("one read at a time: calls meanwhile become one more read, and a link that came meanwhile is kept", async () => {
    let answer;
    h.routes.set("GET chat-links", () => new Promise((resolve) => (answer = resolve)));
    const first = L.loadLinks();
    await settle();
    L.loadLinks();
    L.loadLinks();
    const later = link("l2", "b2", "s1", "m1");
    await deliver(ev("chat_link", "created", later)); // held while the read is on its way
    expect(L.links.byId.l2).toBeUndefined();
    const reads = linkReads();
    answer([link("l1", "b1", "s1", "m1")]);
    await settle();
    expect(linkReads()).toBe(reads + 1); // exactly one follow-up
    answer([link("l1", "b1", "s1", "m1"), later]);
    expect(await first).toBe(true);
    expect(Object.keys(L.links.byId).sort()).toEqual(["l1", "l2"]);
  });

  it.each([
    ["the session first", ["session", "chat_link"]],
    ["the link first", ["chat_link", "session"]],
  ])("chat_link and session created in either order nest the branch under its parent (%s)", async (_name, order) => {
    await withList(chat("s1"));
    h.routes.set("GET sessions/b1", chat("b1", { title: "login", created_at: 3, updated_at: 3 }));
    const entries = {
      session: ev("session", "created", chat("b1", { title: "login", created_at: 3, updated_at: 3 })),
      chat_link: ev("chat_link", "created", link("l1", "b1", "s1", "m1")),
    };
    for (const type of order) await deliver(entries[type]);
    expect(shape(rows("s1"))).toEqual(["s1@0", "b1@1"]);
  });
});

describe("closing (3.3, 4.8)", () => {
  async function family() {
    await withList(chat("s1", { updated_at: 5 }), chat("b1", { created_at: 2 }), chat("b2", { created_at: 3 }), chat("s2", { updated_at: 1 }));
    await deliver(ev("chat_link", "created", link("l1", "b1", "s1", "m1")), ev("chat_link", "created", link("l2", "b2", "b1", "m2")));
  }

  it("a closed branch is left out and its children move up; the footer counts it; reopening puts it back", async () => {
    await family();
    expect(shape(rows("b2"))).toEqual(["s1@0", "b1@1", "b2@2", "s2@0"]);
    expect(await C.close("b1")).toBe(true);
    expect(h.closedCalls).toEqual([{ project: DATA, sessionId: "b1", closed: true }]);
    const built = rows("b2");
    expect(shape(built)).toEqual(["s1@0", "b2@1", "s2@0"]);
    expect(built.closedCount).toBe(1);
    await C.reopen("b1");
    expect(shape(rows("b2"))).toEqual(["s1@0", "b1@1", "b2@2", "s2@0"]);
  });

  it("another window's change arrives as settings-changed; a refused write is put back with a notice", async () => {
    await family();
    h.listeners.get("settings-changed")({ payload: { ...h.settings, closed_chats: { [DATA]: ["s2"], "/elsewhere": ["s1"] } } });
    expect([C.isClosed("s2"), C.isClosed("s1")]).toEqual([true, false]);
    h.refuse = true;
    const { ui } = await import("./ui.svelte.js");
    expect(await C.close("s1")).toBe(false);
    expect(C.isClosed("s1")).toBe(false);
    expect(ui.notice?.text).toBe("Couldn't close this chat: disk full");
  });

  it("closing the shown chat opens the first chat still listed; closing the last leaves the landing state (D15)", async () => {
    await family();
    for (const id of ["s1", "b1", "b2", "s2"]) openable(id);
    await S.openChat("s1");
    await C.closeChat("s1");
    expect(S.sessions.currentId).toBe("b1"); // s1's branch moved up to the top
    await C.closeChat("b1");
    await C.closeChat("b2");
    expect(S.sessions.currentId).toBe("s2");
    await C.closeChat("s2");
    expect(S.sessions.currentId).toBe("");
  });
});

it("the tree store keeps its own revision: another app's tree_changed only marks it stale (P4.4's base)", async () => {
  h.routes.set("GET sessions/s1/tree", { revision: 4, entries: [{ message_id: "m1", parent_id: "", active: true }] });
  await T.readTree("s1");
  expect(T.expectedRevision("s1")).toBe(4);
  T.keepRevision("s1", 5); // this window's own switch
  await deliver(ev("tree_changed", "updated", { session_id: "s1", revision: 5 }));
  expect(T.tree.bySession.s1.stale).toBe(false);
  await deliver(ev("tree_changed", "updated", { session_id: "s1", revision: 6 }));
  expect([T.tree.bySession.s1.stale, T.expectedRevision("s1")]).toEqual([true, 5]);
  await T.readTree("s1");
  expect(T.tree.bySession.s1.stale).toBe(false);
});

// Last in the file: the branch plugin's page listeners stay on the document.
it("a branch link over a web link's words wins: a click opens only the branch, and the hover title is the branch's", async () => {
  await withList(chat("s1"), chat("b1", { title: "Login guide branch" }));
  openable("b1");
  await import("../plugins/branch.js");
  const { createMarkdown } = await import("../render/markdown.js");
  const { guardLinks } = await import("../render/links.js");
  const text = "Read [the login guide](https://example.com/guide) first.";
  const start = text.indexOf("login");
  const end = start + "login guide".length;
  const root = document.createElement("div");
  root.className = "md";
  document.body.replaceChildren(root);
  createMarkdown(root, { messageId: "m1", sessionId: "s1" }).update(text, { finished: true, decorations: [{ kind: "branch-link", start, end, data: link("l1", "b1", "s1", "m1", { start, end }) }] });
  const opened = [];
  guardLinks(root, (href) => opened.push(href), { trust: () => true }); // as a real click
  const anchor = root.querySelector("a");
  const span = anchor.querySelector(".md-deco-branch-link");
  expect(span.textContent).toBe("login guide");
  const click = (el) => el.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true, button: 0, detail: 1 }));
  click(anchor); // the web link's own words still open its address
  expect(opened).toEqual(["https://example.com/guide"]);
  span.dispatchEvent(new MouseEvent("mouseover", { bubbles: true }));
  expect([span.closest("[title]") === span, span.title]).toEqual([true, "Open Login guide branch"]);
  expect(click(span)).toBe(false); // nothing navigates
  await settle();
  expect(opened).toEqual(["https://example.com/guide"]);
  expect(S.sessions.currentId).toBe("b1");
});
