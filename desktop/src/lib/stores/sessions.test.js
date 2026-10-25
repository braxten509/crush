import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The real event loop (events.js) with fake Tauri calls (see prompts.test.js).
const h = vi.hoisted(() => ({ routes: new Map(), calls: [], entries: [], ring: null, current: [], notices: [] }));

vi.mock("@tauri-apps/api/webviewWindow", () => ({
  getCurrentWebviewWindow: () => ({
    listen: async (_name, fn) => {
      h.ring = fn;
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
  engine_set_current_session: async (sid) => {
    h.current.push(sid);
    return null;
  },
  events_take: async () => h.entries.splice(0),
  lastClipboardWrite: () => null,
}));

const tick = () => new Promise((resolve) => setTimeout(resolve, 0));
async function settle(rounds = 10) {
  for (let i = 0; i < rounds; i += 1) await tick();
}
async function deliver(...entries) {
  h.entries.push(...entries);
  h.ring?.();
  await settle();
}
const ev = (type, event, payload) => ({ kind: "event", type, event, payload });

// An engine answer the test gives when it wants: a slow open.
function later() {
  let resolve;
  let reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

let S; // sessions store
let M; // messages store
let api;
let ui;
let ws;
let loadFiles;

beforeEach(async () => {
  vi.resetModules();
  h.routes.clear();
  h.calls.length = 0;
  h.entries.length = 0;
  h.ring = null;
  h.current.length = 0;
  h.notices.length = 0;
  vi.stubGlobal("requestAnimationFrame", (fn) => setTimeout(() => fn(0), 0));
  // Every notice, in order (a newer one replaces the shown one).
  vi.doMock("./ui.svelte.js", async (importOriginal) => {
    const real = await importOriginal();
    return {
      ...real,
      notify: (kind, text, ...rest) => {
        h.notices.push({ kind, text });
        return real.notify(kind, text, ...rest);
      },
    };
  });
  const events = await import("../engine/events.js");
  S = await import("./sessions.svelte.js");
  M = await import("./messages.svelte.js");
  ({ api } = await import("../engine/api.js"));
  ui = await import("./ui.svelte.js");
  ({ ws, loadFiles } = await import("./workspace.svelte.js"));
  await events.startEvents();
  await settle();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const chat = (id, extra = {}) => ({ id, title: `Chat ${id}`, parent_session_id: "", message_count: 0, created_at: 1, updated_at: 1, is_busy: false, attached_clients: 0, has_unfinished_work: false, pending_prompt: "", ...extra });
const finish = { type: "finish", data: { reason: "end_turn", time: 2 } };
const reply = (id, sid, { finished = false, text = "hi", at = 2, ...extra } = {}) => ({
  id,
  role: "assistant",
  session_id: sid,
  parts: [{ type: "text", data: { text } }, ...(finished ? [finish] : [])],
  model: "opus",
  provider: "claude-code",
  created_at: at,
  updated_at: at,
  ...extra,
});
const user = (id, sid, text = "hello", submission = "") => ({ id, role: "user", session_id: sid, parts: [{ type: "text", data: { text, ...(submission ? { submission_id: submission } : {}) } }], created_at: 1, updated_at: 1 });

// A chat that opens at once: its record, history and messages.
function openable(id, list = [], record = {}) {
  h.routes.set(`GET sessions/${id}`, chat(id, record));
  h.routes.set(`GET sessions/${id}/history`, []);
  h.routes.set(`GET sessions/${id}/messages`, list);
}

async function withList(...records) {
  h.routes.set("GET sessions", records);
  await S.loadSessions();
}

describe("the live flag (1.5.3, LIST-012)", () => {
  it("keeps an open chat's status and activity when another folder's list loads", async () => {
    await withList(chat("s1", { has_unfinished_work: true }));
    S.setOpenChats(["s1"]);
    await deliver(ev("message", "created", reply("m1", "s1", { at: 40, finished: true })));
    await withList(chat("s2"));
    expect(S.sessions.byId.s1?.has_unfinished_work).toBe(true);
    expect(S.sessions.activity.s1).toBe(40);
    expect(S.sessions.order).toContain("s1");
  });

  it("an assistant message without finish for a chat not loaded turns it busy; its run_complete turns it idle", async () => {
    await withList(chat("s1"), chat("s2"));
    expect(S.busy("s2")).toBe(false);
    await deliver(ev("message", "created", reply("m1", "s2")));
    expect(S.sessions.live.s2).toBe(true);
    expect(S.busy("s2")).toBe(true);
    expect(M.messages.bySession.s2).toBeUndefined(); // the message itself is dropped
    await deliver(ev("run_complete", "updated", { session_id: "s2", message_id: "m1" }));
    expect(S.busy("s2")).toBe(false);
  });

  it("a message with finish clears it", async () => {
    await withList(chat("s2"));
    await deliver(ev("message", "created", reply("m1", "s2")));
    await deliver(ev("message", "updated", reply("m1", "s2", { finished: true })));
    expect(S.busy("s2")).toBe(false);
  });

  it("is never set for a chat this window has not seen", async () => {
    await withList(chat("s1"));
    await deliver(ev("message", "created", reply("m1", "ghost")));
    expect(S.sessions.live.ghost).toBeUndefined();
    expect(S.sessions.byId.ghost).toBeUndefined();
  });

  it("loading the chat clears it; its messages answer busy then", async () => {
    await withList(chat("s2"));
    await deliver(ev("message", "created", reply("m1", "s2")));
    openable("s2", [reply("m1", "s2")]);
    await S.openChat("s2");
    expect(S.sessions.live.s2).toBeUndefined();
    // The snapshot's unfinished message counts only after a live update (the ghost guard).
    expect(S.busy("s2")).toBe(false);
    await deliver(ev("message", "updated", reply("m1", "s2", { text: "hi there" })));
    expect(S.busy("s2")).toBe(true);
    await deliver(ev("message", "updated", reply("m1", "s2", { finished: true })));
    expect(S.busy("s2")).toBe(false);
  });

  it("a resync's list read replaces it with the read is_busy", async () => {
    await withList(chat("s1"), chat("s2"));
    await deliver(ev("message", "created", reply("m1", "s2")));
    h.routes.set("GET sessions", [chat("s1", { is_busy: true }), chat("s2", { is_busy: false })]);
    await deliver({ kind: "resync" });
    expect(S.sessions.live.s2).toBeUndefined();
    expect(S.sessions.live.s1).toBe(true);
    expect(S.busy("s2")).toBe(false);
    expect(S.busy("s1")).toBe(true);
  });

  it("a chat's own read clears it, so a chat the list read as busy stops working with no event", async () => {
    await withList(chat("s1"), chat("s2", { is_busy: true }));
    expect(S.busy("s2")).toBe(true);
    S.upsertSession(chat("s2", { is_busy: false }), { since: S.readStart() });
    expect(S.sessions.live.s2).toBeUndefined();
    expect(S.busy("s2")).toBe(false);
  });

  it("a message newer than the record moves the chat up the list", async () => {
    await withList(chat("s1", { updated_at: 30 }), chat("s2", { updated_at: 20 }));
    expect(S.sessions.order).toEqual(["s1", "s2"]);
    await deliver(ev("message", "created", reply("m1", "s2", { at: 40 })));
    expect(S.sessions.order).toEqual(["s2", "s1"]);
  });

  it("leaving a chat that is replying keeps it loaded and busy in the list", async () => {
    await withList(chat("s1"), chat("s2"));
    openable("s1", []);
    await S.openChat("s1");
    await deliver(ev("message", "created", reply("m1", "s1")));
    expect(S.busy("s1")).toBe(true);
    openable("s2", []);
    await S.openChat("s2");
    expect(M.messages.bySession.s1?.status).toBe("ready"); // kept, so switching back is at once
    expect(S.busy("s1")).toBe(true);
  });
});

describe("busy (1.5.3)", () => {
  it("(a) the last read; a run_complete clears it", async () => {
    await withList(chat("s1", { is_busy: true }));
    expect(S.busy("s1")).toBe(true);
    await deliver(ev("run_complete", "updated", { session_id: "s1" }));
    expect(S.busy("s1")).toBe(false);
  });

  it("(b) this window's send until the run_complete with its run_id", async () => {
    await withList(chat("s1"));
    api.send({ session_id: "s1", prompt: "a", run_id: "r1" });
    api.send({ session_id: "s1", prompt: "b", run_id: "r2" });
    await settle();
    expect(S.busy("s1")).toBe(true);
    await deliver(ev("run_complete", "updated", { session_id: "s1", run_id: "r1" }));
    expect(S.busy("s1")).toBe(true);
    await deliver(ev("run_complete", "updated", { session_id: "s1", run_id: "r2" }));
    expect(S.busy("s1")).toBe(false);
  });

  it("(b) a send the engine refused starts nothing", async () => {
    await withList(chat("s1"));
    h.routes.set("POST agent", () => Promise.reject({ code: "agent_not_ready", message: "agent not initialized" }));
    await api.send({ session_id: "s1", prompt: "a" }).catch(() => {});
    await settle();
    expect(S.busy("s1")).toBe(false);
  });

  it("a read started before a run_complete does not bring is_busy back", async () => {
    await withList(chat("s1"));
    const slow = later();
    h.routes.set("GET sessions/s9", () => slow.promise);
    await deliver(ev("session", "created", { id: "s9", title: "New", updated_at: 3 }));
    await deliver(ev("run_complete", "updated", { session_id: "s9" }));
    slow.resolve(chat("s9", { is_busy: true }));
    await settle();
    // The event's record is newer than the read's, so its fields stay too.
    expect(S.sessions.byId.s9.title).toBe("New");
    expect(S.busy("s9")).toBe(false);
  });

  it("a read that started after the run_complete brings is_busy", async () => {
    await withList(chat("s1"));
    await deliver(ev("run_complete", "updated", { session_id: "s1" }));
    const since = S.readStart();
    S.upsertSession(chat("s1", { is_busy: true }), { since });
    expect(S.busy("s1")).toBe(true);
  });
});

describe("session records", () => {
  it("events keep the four computed fields; a read replaces them", async () => {
    await withList(chat("s1", { is_busy: true, attached_clients: 2, has_unfinished_work: true, pending_prompt: "question" }));
    await deliver(ev("session", "updated", { id: "s1", title: "Renamed", updated_at: 5, is_busy: false, attached_clients: 0, has_unfinished_work: false, pending_prompt: "" }));
    const s = S.sessions.byId.s1;
    expect(s.title).toBe("Renamed");
    expect([s.is_busy, s.attached_clients, s.has_unfinished_work, s.pending_prompt]).toEqual([true, 2, true, "question"]);
    S.upsertSession(chat("s1", { title: "Renamed", updated_at: 5 }));
    expect(S.sessions.byId.s1.is_busy).toBe(false);
    expect(S.sessions.byId.s1.pending_prompt).toBe("");
  });

  it("a created event for an unseen chat reads it once", async () => {
    await withList(chat("s1"));
    h.routes.set("GET sessions/s2", chat("s2", { is_busy: true, pending_prompt: "permission" }));
    await deliver(ev("session", "created", { id: "s2", title: "New", updated_at: 9 }));
    expect(S.sessions.byId.s2.pending_prompt).toBe("permission");
    expect(S.busy("s2")).toBe(true);
    await deliver(ev("session", "created", { id: "s2", title: "New", updated_at: 9 }));
    expect(h.calls.filter((c) => c.path === "sessions/s2")).toHaveLength(1);
    expect(S.sessions.order[0]).toBe("s2");
  });

  it("children are not in the list but lead to their root", async () => {
    await withList(chat("s1"));
    await deliver(ev("session", "created", chat("title-s1", { parent_session_id: "s1" })));
    expect(S.sessions.byId["title-s1"]).toBeUndefined();
    expect(S.rootOf("title-s1")).toBe("s1");
    expect(S.sessions.order).toEqual(["s1"]);
  });
});

describe("opening a chat (LIST-034, TYPE-049)", () => {
  it("names the chat being opened, then shows it and tells the engine", async () => {
    await withList(chat("s1"));
    const slow = later();
    h.routes.set("GET sessions/s1", chat("s1"));
    h.routes.set("GET sessions/s1/history", []);
    h.routes.set("GET sessions/s1/messages", () => slow.promise);
    const done = S.openChat("s1");
    await settle();
    expect(S.sessions.opening).toMatchObject({ id: "s1", title: "Chat s1" });
    expect(S.sessions.currentId).toBe("");
    slow.resolve([user("u1", "s1")]);
    expect(await done).toBe(true);
    expect(S.sessions.currentId).toBe("s1");
    expect(S.sessions.opening).toBeNull();
    expect(h.current).toEqual(["s1"]);
    expect(M.messages.bySession.s1.ids).toEqual(["u1"]);
  });

  it("a newer switch wins over an older one that lands later", async () => {
    await withList(chat("a"), chat("b"));
    const slowA = later();
    h.routes.set("GET sessions/a", chat("a"));
    h.routes.set("GET sessions/a/history", []);
    h.routes.set("GET sessions/a/messages", () => slowA.promise);
    openable("b", [user("ub", "b")]);
    const first = S.openChat("a");
    await settle();
    const second = S.openChat("b");
    expect(await second).toBe(true);
    slowA.resolve([user("ua", "a")]);
    expect(await first).toBe(false);
    expect(S.sessions.currentId).toBe("b");
    expect(M.messages.bySession.a).toBeUndefined();
  });

  it("a prompt sent while another chat opens goes to that chat, not the shown one", async () => {
    await withList(chat("old"), chat("new"));
    openable("old", []);
    await S.openChat("old");
    const slow = later();
    h.routes.set("GET sessions/new", chat("new"));
    h.routes.set("GET sessions/new/history", []);
    h.routes.set("GET sessions/new/messages", () => slow.promise);
    const opening = S.openChat("new");
    await settle();
    const sentTo = [];
    expect(S.deferSend({ text: "for new", run: (sid) => sentTo.push(sid) })).toBe(true);
    expect(sentTo).toEqual([]);
    slow.resolve([]);
    await opening;
    await settle();
    expect(sentTo).toEqual(["new"]);
  });

  it("nothing is held when no switch is pending", async () => {
    await withList(chat("old"));
    openable("old", []);
    await S.openChat("old");
    expect(S.deferSend({ text: "x", run: () => {} })).toBe(false);
  });

  it("a failed open gives the prompt back to the field and the shell command a warning", async () => {
    await withList(chat("old"), chat("bad"));
    openable("old", []);
    await S.openChat("old");
    const slow = later();
    h.routes.set("GET sessions/bad", chat("bad"));
    h.routes.set("GET sessions/bad/history", []);
    h.routes.set("GET sessions/bad/messages", () => slow.promise);
    const opening = S.openChat("bad");
    await settle();
    const run = vi.fn();
    S.deferSend({ text: "keep me", attachments: [{ name: "a.png" }], run });
    S.deferSend({ text: "ls", shell: true, run });
    slow.reject({ code: "transport", message: "socket closed" });
    expect(await opening).toBe(false);
    expect(run).not.toHaveBeenCalled();
    expect(S.sessions.currentId).toBe("old");
    expect(M.takeReturned("old")).toEqual([{ text: "keep me", attachments: [{ name: "a.png" }] }]);
    expect(h.current.at(-1)).toBe("old");
    expect(h.notices).toEqual([
      { kind: "warn", text: "Shell command not run: the conversation did not open" },
      { kind: "error", text: "Couldn't load this chat: socket closed" },
    ]);
    expect(ui.ui.notice.action.label).toBe("Try again");
  });

  it("Try again opens the chat that failed", async () => {
    await withList(chat("bad"));
    h.routes.set("GET sessions/bad", () => Promise.reject({ code: "transport", message: "down" }));
    h.routes.set("GET sessions/bad/history", []);
    h.routes.set("GET sessions/bad/messages", []);
    expect(await S.openChat("bad")).toBe(false);
    expect(S.sessions.currentId).toBe("");
    h.routes.set("GET sessions/bad", chat("bad"));
    expect(await ui.ui.notice.action.run()).toBe(true);
    expect(S.sessions.currentId).toBe("bad");
  });

  it("a chat deleted while it opens is abandoned and its held prompt returns", async () => {
    await withList(chat("old"), chat("gone"));
    openable("old", []);
    await S.openChat("old");
    const slow = later();
    h.routes.set("GET sessions/gone", chat("gone"));
    h.routes.set("GET sessions/gone/history", []);
    h.routes.set("GET sessions/gone/messages", () => slow.promise);
    const opening = S.openChat("gone");
    await settle();
    S.deferSend({ text: "for gone", run: vi.fn() });
    await deliver(ev("session", "deleted", { id: "gone" }));
    expect(S.sessions.opening).toBeNull();
    slow.resolve([]);
    expect(await opening).toBe(false);
    expect(S.sessions.currentId).toBe("old");
    expect(M.takeReturned("old").map((d) => d.text)).toEqual(["for gone"]);
  });

  it("a history read that fails still opens the chat, with no files and a warning", async () => {
    await withList(chat("s1"));
    h.routes.set("GET sessions/s1", chat("s1"));
    h.routes.set("GET sessions/s1/messages", []);
    expect(await S.openChat("s1")).toBe(true);
    expect(S.sessions.currentId).toBe("s1");
    expect(ws.files.s1).toEqual([]);
    expect(h.notices).toEqual([{ kind: "warn", text: "Couldn't read this chat's file history, so its changed files are not listed" }]);
  });

  for (const order of ["the newer read lands first", "the older read lands first"]) {
    it(`the open's history read and a later history read share one guard: ${order}`, async () => {
      await withList(chat("s1"));
      h.routes.set("GET sessions/s1", chat("s1"));
      h.routes.set("GET sessions/s1/messages", []);
      const reads = [];
      h.routes.set("GET sessions/s1/history", () => new Promise((resolve) => reads.push(resolve)));
      const opening = S.openChat("s1");
      await settle();
      const reading = loadFiles("s1");
      await settle();
      expect(reads).toHaveLength(2);
      const older = [{ id: "f1", session_id: "s1", path: "/p/a.go", version: 0, created_at: 10 }];
      const newer = [...older, { id: "f2", session_id: "s1", path: "/p/b.go", version: 0, created_at: 20 }];
      if (order === "the newer read lands first") {
        reads[1](newer);
        await settle();
        reads[0](older);
      } else {
        reads[0](older);
        await settle();
        reads[1](newer);
      }
      expect(await opening).toBe(true);
      await reading;
      await settle();
      expect(ws.files.s1.map((f) => f.id)).toEqual(["f1", "f2"]);
    });
  }

  it("a file event during the open's history read is not lost (1.5.4)", async () => {
    await withList(chat("s1"));
    h.routes.set("GET sessions/s1", chat("s1"));
    h.routes.set("GET sessions/s1/messages", []);
    let answer;
    h.routes.set("GET sessions/s1/history", () => new Promise((resolve) => (answer = resolve)));
    const opening = S.openChat("s1");
    await settle();
    await deliver(ev("file", "created", { id: "f2", session_id: "s1", path: "/p/b.go", version: 0, created_at: 20 }));
    // The read answers with what the engine had before that event.
    answer([{ id: "f1", session_id: "s1", path: "/p/a.go", version: 0, created_at: 10 }]);
    expect(await opening).toBe(true);
    await settle();
    expect(ws.files.s1.map((f) => f.id)).toEqual(["f1", "f2"]);
  });

  it("-s opens that chat and -C the newest one (START-006)", async () => {
    await withList(chat("new", { updated_at: 9 }), chat("old", { updated_at: 1 }));
    openable("new", []);
    openable("old", []);
    expect(await S.openLaunchChat({ continueLast: true })).toBe(true);
    expect(S.sessions.currentId).toBe("new");
    expect(await S.openLaunchChat({ session: "old" })).toBe(false); // a chat is already shown
  });

  it("-s with a whole chat id", async () => {
    await withList(chat("new"), chat("old"));
    openable("old", []);
    expect(await S.openLaunchChat({ session: "old" })).toBe(true);
    expect(S.sessions.currentId).toBe("old");
    expect(await S.openLaunchChat({ session: "zzz" })).toBe(false);
  });
});

describe("open chats load ahead (owner, 2026-10-21)", () => {
  it("a preloaded chat shows at once, with no read in the way", async () => {
    await withList(chat("a"), chat("b"));
    openable("a", [user("ua", "a")]);
    openable("b", [user("ub", "b")]);
    await S.openChat("a");
    expect(await S.preloadChat("b")).toBe(true);
    expect(S.isKept("b")).toBe(true);
    const reads = h.calls.length;
    h.routes.set("GET sessions/b/messages", () => new Promise(() => {})); // a read now would never end
    expect(await S.openChat("b")).toBe(true);
    expect(S.sessions.currentId).toBe("b");
    expect(M.messages.bySession.b.ids).toEqual(["ub"]);
    expect(h.calls.length).toBeGreaterThan(reads); // read again behind it
  });

  it("open chats stay kept past the usual six; others make way", async () => {
    const ids = Array.from({ length: 9 }, (_, i) => `c${i}`);
    await withList(...ids.map((id) => chat(id)));
    for (const id of ids) openable(id, [user(`u${id}`, id)]);
    S.setOpenChats(ids.slice(0, 8));
    for (const id of ids) await S.preloadChat(id);
    for (const id of ids.slice(0, 8)) expect(S.isKept(id)).toBe(true);
    expect(S.isKept("c8")).toBe(false);
  });

  it("a chat opened while it preloads is left to the opening", async () => {
    await withList(chat("a"));
    const slow = later();
    h.routes.set("GET sessions/a", chat("a"));
    h.routes.set("GET sessions/a/history", []);
    h.routes.set("GET sessions/a/messages", () => slow.promise);
    const preload = S.preloadChat("a");
    await settle();
    const opening = S.openChat("a");
    slow.resolve([user("ua", "a")]);
    expect(await preload).toBe(false);
    expect(await opening).toBe(true);
    expect(S.sessions.currentId).toBe("a");
    expect(S.isKept("a")).toBe(false);
  });
});

describe("deleted, new and first chats (LIST-037, LIST-033, TYPE-041)", () => {
  it("the shown chat deleted elsewhere: the landing state and a notice", async () => {
    await withList(chat("s1"), chat("s2"));
    openable("s1", []);
    await S.openChat("s1");
    await deliver(ev("session", "deleted", { id: "s1" }));
    expect(S.sessions.currentId).toBe("");
    expect(S.sessions.order).toEqual(["s2"]);
    expect(h.current.at(-1)).toBe("");
    expect(ui.ui.notice).toMatchObject({ kind: "info", text: "This chat was deleted elsewhere." });
  });

  it("this window's own delete shows no notice", async () => {
    await withList(chat("s1"));
    openable("s1", []);
    await S.openChat("s1");
    await S.deleteChat("s1");
    expect(h.calls.at(-1)).toMatchObject({ method: "DELETE", path: "sessions/s1" });
    await deliver(ev("session", "deleted", { id: "s1" }));
    expect(S.sessions.currentId).toBe("");
    expect(ui.ui.notice).toBeNull();
  });

  it("a deleted event for an unknown chat does nothing", async () => {
    await withList(chat("s1"));
    openable("s1", []);
    await S.openChat("s1");
    await deliver(ev("session", "deleted", { id: "nope" }));
    expect(S.sessions.order).toEqual(["s1"]);
    expect(S.sessions.currentId).toBe("s1");
    expect(M.messages.bySession.s1.status).toBe("ready");
    expect(ui.ui.notice).toBeNull();
  });

  it("New chat is saved immediately and survives leaving without sending; the old chat keeps working", async () => {
    await withList(chat("s1", { is_busy: true }));
    openable("s1", [], { is_busy: true });
    await S.openChat("s1");
    h.routes.set("POST sessions", ({ body }) => chat("fresh", { title: body.title }));
    expect(await S.newChat()).toBe(true);
    expect(ui.ui.notice).toBeNull();
    expect(S.busy("s1")).toBe(true);
    expect(S.sessions.currentId).toBe("fresh");
    expect(h.current.at(-1)).toBe("fresh");
    expect(h.calls.filter((c) => c.method === "POST" && c.path === "sessions")).toHaveLength(1);
    openable("fresh");
    await S.openChat("s1");
    expect(S.sessions.order).toContain("fresh");
    expect(S.sessions.byId.fresh.message_count).toBe(0);
    expect(await S.openChat("fresh")).toBe(true);
    expect(await S.ensureSession()).toBe("fresh");
    expect(h.calls.filter((c) => c.method === "POST" && c.path === "sessions")).toHaveLength(1);
  });

  it("keeps the current chat until a new chat has saved, and reports creation failures", async () => {
    await withList(chat("s1"));
    openable("s1", []);
    await S.openChat("s1");
    const saving = later();
    h.routes.set("POST sessions", () => saving.promise);
    const starting = S.newChat("/project");
    await settle();
    expect(S.sessions.currentId).toBe("s1");
    saving.reject(new Error("Cannot save chat"));
    expect(await starting).toBe(false);
    expect(S.sessions.currentId).toBe("s1");
    expect(S.sessions.draft).toBe(false);
    expect(ui.ui.notice).toMatchObject({ kind: "error", text: expect.stringContaining("Cannot save chat") });
    h.routes.set("POST sessions", () => chat("fresh"));
    expect(await S.newChat("/project")).toBe(true);
    expect(S.sessions.currentId).toBe("fresh");
  });

  it("ensureSession makes one \"New Session\" for two quick sends", async () => {
    await withList();
    h.routes.set("POST sessions", ({ body }) => chat("fresh", { title: body.title }));
    const [a, b] = await Promise.all([S.ensureSession(), S.ensureSession()]);
    expect([a, b]).toEqual(["fresh", "fresh"]);
    expect(h.calls.filter((c) => c.method === "POST" && c.path === "sessions")).toHaveLength(1);
    expect(h.calls.find((c) => c.path === "sessions" && c.method === "POST").body).toEqual({ title: "New Session" });
    expect(S.sessions.currentId).toBe("fresh");
    expect(S.sessions.order).toEqual(["fresh"]);
    expect(M.messages.bySession.fresh.status).toBe("ready");
    expect(h.current.at(-1)).toBe("fresh");
    expect(await S.ensureSession()).toBe("fresh");
  });
});
