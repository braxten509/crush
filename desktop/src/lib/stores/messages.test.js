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

function later() {
  let resolve;
  let reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

let M;
let S;
let P;
let api;

beforeEach(async () => {
  vi.resetModules();
  h.routes.clear();
  h.calls.length = 0;
  h.entries.length = 0;
  h.ring = null;
  h.current.length = 0;
  h.notices.length = 0;
  vi.stubGlobal("requestAnimationFrame", (fn) => setTimeout(() => fn(0), 0));
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
  M = await import("./messages.svelte.js");
  S = await import("./sessions.svelte.js");
  P = await import("./prompts.svelte.js");
  ({ api } = await import("../engine/api.js"));
  await events.startEvents();
  await settle();
  S.upsertSession({ id: "s1", title: "One", updated_at: 1 });
  S.upsertSession({ id: "s2", title: "Two", updated_at: 1 });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const finishPart = (reason = "end_turn") => ({ type: "finish", data: { reason, time: 9 } });
function msg(id, { sid = "s1", role = "assistant", text = "", finished = false, at = 2, calls = [], results = [], submission = "", ...extra } = {}) {
  const parts = [];
  if (text || role !== "tool") parts.push({ type: "text", data: { text, ...(submission ? { submission_id: submission } : {}) } });
  for (const [callId, name = "bash"] of calls) parts.push({ type: "tool_call", data: { id: callId, name, input: "{}", finished: true } });
  for (const callId of results) parts.push({ type: "tool_result", data: { tool_call_id: callId, name: "bash", content: "ok", metadata: "", is_error: false } });
  if (finished) parts.push(finishPart(finished === true ? "end_turn" : finished));
  return { id, role, session_id: sid, parts, model: "opus", provider: "claude-code", created_at: at, updated_at: at, ...extra };
}
const textOf = (m) => m?.parts?.find((p) => p.type === "text")?.data?.text;
const chatOf = (sid = "s1") => M.messages.bySession[sid];

async function loaded(sid, list) {
  h.routes.set(`GET sessions/${sid}/messages`, list);
  await M.load(sid);
  return chatOf(sid);
}

describe("loading a chat (LIST-036): the buffer and its replay", () => {
  it("events that arrive while the chat loads are replayed on the snapshot, in order", async () => {
    const slow = later();
    h.routes.set("GET sessions/s1/messages", () => slow.promise);
    const loading = M.load("s1");
    await settle();
    expect(chatOf().status).toBe("loading");
    await deliver(ev("message", "created", msg("m2", { text: "a", at: 3 })), ev("message", "updated", msg("m2", { text: "ab", at: 3 })));
    await deliver(ev("message", "updated", msg("m2", { text: "abc", at: 3 })));
    expect(chatOf().ids).toEqual([]); // nothing lands before the snapshot
    slow.resolve([msg("m1", { role: "user", text: "hi", at: 1 })]);
    expect(await loading).toBe(true);
    expect(chatOf().status).toBe("ready");
    expect(chatOf().ids).toEqual(["m1", "m2"]);
    expect(textOf(chatOf().byId.get("m2"))).toBe("abc");
  });

  it("finished wins: a buffered chunk never rolls back a message the snapshot shows finished", async () => {
    const slow = later();
    h.routes.set("GET sessions/s1/messages", () => slow.promise);
    const loading = M.load("s1");
    await settle();
    await deliver(ev("message", "updated", msg("m1", { text: "half", at: 2 })));
    slow.resolve([msg("m1", { text: "whole reply", finished: true, at: 2 })]);
    await loading;
    expect(textOf(chatOf().byId.get("m1"))).toBe("whole reply");
    expect(M.isFinished(chatOf().byId.get("m1"))).toBe(true);
  });

  it("a snapshot copy with a newer updated_at wins over a buffered event", async () => {
    const slow = later();
    h.routes.set("GET sessions/s1/messages", () => slow.promise);
    const loading = M.load("s1");
    await settle();
    await deliver(ev("message", "updated", msg("m1", { text: "old", at: 5 })));
    slow.resolve([msg("m1", { text: "newer", at: 7 })]);
    await loading;
    expect(textOf(chatOf().byId.get("m1"))).toBe("newer");
  });

  it("a delete during the load still removes the message", async () => {
    const slow = later();
    h.routes.set("GET sessions/s1/messages", () => slow.promise);
    const loading = M.load("s1");
    await settle();
    await deliver(ev("message", "deleted", msg("m1")));
    slow.resolve([msg("m1", { text: "x", finished: true }), msg("m2", { text: "y", finished: true, at: 3 })]);
    await loading;
    expect(chatOf().ids).toEqual(["m2"]);
  });

  it("the newest load of a chat wins; the older call resolves with it", async () => {
    const first = later();
    const second = later();
    let n = 0;
    h.routes.set("GET sessions/s1/messages", () => (++n === 1 ? first.promise : second.promise));
    const a = M.load("s1");
    await settle();
    await deliver(ev("message", "created", msg("m9", { text: "kept", at: 9 })));
    const b = M.load("s1");
    await settle();
    second.resolve([msg("m1", { text: "second", finished: true })]);
    first.resolve([msg("m1", { text: "first", finished: true })]);
    expect(await a).toBe(true);
    expect(await b).toBe(true);
    expect(textOf(chatOf().byId.get("m1"))).toBe("second");
    expect(chatOf().ids).toEqual(["m1", "m9"]); // the buffer carried over to the newer load
  });

  it("a shown chat keeps taking events while it reloads", async () => {
    await loaded("s1", [msg("m1", { text: "a", finished: true })]);
    const slow = later();
    h.routes.set("GET sessions/s1/messages", () => slow.promise);
    const reloading = M.load("s1");
    await settle();
    await deliver(ev("message", "created", msg("m2", { text: "live", at: 3 })));
    expect(chatOf().ids).toEqual(["m1", "m2"]);
    slow.resolve([msg("m1", { text: "a", finished: true })]);
    await reloading;
    expect(chatOf().ids).toEqual(["m1", "m2"]);
  });

  it("a failed first load leaves the chat in error", async () => {
    h.routes.set("GET sessions/s1/messages", () => Promise.reject({ code: "transport", message: "down" }));
    await expect(M.load("s1")).rejects.toMatchObject({ code: "transport" });
    expect(chatOf().status).toBe("error");
    expect(chatOf().error.message).toBe("down");
  });
});

describe("duplicates and keys (1.5.3)", () => {
  it("applying the same events twice gives the same state", async () => {
    await loaded("s1", []);
    const batch = [ev("message", "created", msg("m1", { text: "a" })), ev("message", "updated", msg("m1", { text: "ab" })), ev("message", "created", msg("m2", { role: "user", text: "q", at: 3 }))];
    await deliver(...batch);
    const once = { ids: chatOf().ids, texts: chatOf().ids.map((id) => textOf(chatOf().byId.get(id))) };
    await deliver(...batch);
    expect(chatOf().ids).toEqual(once.ids);
    expect(chatOf().ids.map((id) => textOf(chatOf().byId.get(id)))).toEqual(once.texts);
  });

  it("a created for a known key is an update; a deleted for an unknown key does nothing", async () => {
    await loaded("s1", [msg("m1", { text: "a" })]);
    await deliver(ev("message", "created", msg("m1", { text: "again" })));
    expect(chatOf().ids).toEqual(["m1"]);
    expect(textOf(chatOf().byId.get("m1"))).toBe("again");
    await deliver(ev("message", "deleted", msg("zz")));
    expect(chatOf().ids).toEqual(["m1"]);
  });

  it("new messages go in by created_at", async () => {
    await loaded("s1", [msg("m1", { at: 1, finished: true }), msg("m3", { at: 5, finished: true })]);
    await deliver(ev("message", "created", msg("m2", { at: 3 })));
    await deliver(ev("message", "created", msg("m4", { at: 5 })));
    expect(chatOf().ids).toEqual(["m1", "m2", "m3", "m4"]);
  });

  it("messages of a chat this window has not seen are dropped", async () => {
    await deliver(ev("message", "created", msg("m1", { sid: "nobody" })));
    expect(M.messages.bySession.nobody).toBeUndefined();
  });
});

describe("protected fields", () => {
  it("message and session events never change is_busy, attached_clients, has_unfinished_work or pending_prompt", async () => {
    S.upsertSession({ id: "s2", title: "Two", updated_at: 1, is_busy: true, attached_clients: 2, has_unfinished_work: true, pending_prompt: "permission" });
    const four = () => {
      const s = S.sessions.byId.s2;
      return [s.is_busy, s.attached_clients, s.has_unfinished_work, s.pending_prompt];
    };
    await deliver(ev("message", "created", msg("m1", { sid: "s2", text: "x", finished: true, at: 4 })));
    await deliver(ev("session", "updated", { id: "s2", title: "Two!", updated_at: 5, is_busy: false, attached_clients: 0, has_unfinished_work: false, pending_prompt: "" }));
    expect(S.sessions.byId.s2.title).toBe("Two!");
    expect(four()).toEqual([true, 2, true, "permission"]);
  });
});

describe("no deep proxy", () => {
  it("a stored message is the object the event carried", async () => {
    await loaded("s1", []);
    const sent = msg("m1", { text: "as sent" });
    await deliver(ev("message", "created", sent));
    expect(chatOf().byId.get("m1")).toBe(sent);
    const update = msg("m1", { text: "as sent, longer" });
    await deliver(ev("message", "updated", update));
    expect(chatOf().byId.get("m1")).toBe(update);
    expect(sent.parts[0].data.text).toBe("as sent"); // the old object was never changed in place
  });

  it("the snapshot's messages are the objects the read returned, and the chat is no proxy", async () => {
    const list = [msg("m1", { role: "user", text: "q" }), msg("m2", { text: "a", finished: true, at: 3 })];
    const chat = await loaded("s1", list);
    expect(chat.byId.get("m1")).toBe(list[0]);
    expect(chat.byId.get("m2")).toBe(list[1]);
    expect(chat).toBeInstanceOf(M.ChatMessages);
    expect(M.messages.bySession.s1).toBe(chat);
  });

  it("ids are replaced as a whole, never pushed into", async () => {
    const chat = await loaded("s1", [msg("m1", { finished: true })]);
    const before = chat.ids;
    await deliver(ev("message", "created", msg("m2", { at: 3 })));
    expect(chat.ids).not.toBe(before);
    expect(before).toEqual(["m1"]);
  });
});

describe("interrupted runs (CHAT-026)", () => {
  async function running() {
    const list = [
      msg("u1", { role: "user", text: "do it", at: 1 }),
      msg("a1", { text: "", at: 2, calls: [["t1"], ["t2"]], finished: "tool_use" }),
      msg("r1", { role: "tool", at: 3, results: ["t2"] }),
      msg("a2", { text: "now", at: 4, calls: [["t3"]] }),
    ];
    await loaded("s1", list);
    S.sessions.currentId = "s1";
    await deliver(ev("message", "updated", list[3])); // a live chunk: the reply is being written
    expect(S.busy("s1")).toBe(true);
  }

  it("marks the unfinished reply and the tools without a result, and stops the busy state at once", async () => {
    await running();
    await api.interrupt("s1");
    expect(h.calls.at(-1)).toMatchObject({ method: "POST", path: "agent/sessions/s1/interrupt" });
    expect(P.prompts.callStatus).toEqual({ t1: "cancelled", t3: "cancelled" });
    expect(M.isInterrupted("a2")).toBe(true);
    expect(M.isInterrupted("a1")).toBe(true);
    expect(M.isInterrupted("u1")).toBe(false);
    expect(S.busy("s1")).toBe(false);
    const shown = M.withInterrupt(chatOf().byId.get("a2"));
    expect(shown.parts.at(-1)).toMatchObject({ type: "finish", data: { reason: "canceled" } });
    expect(M.withInterrupt(chatOf().byId.get("a2"))).toBe(shown); // one copy per message object
    expect(chatOf().byId.get("a2").parts.some((p) => p.type === "finish")).toBe(false); // the stored one is untouched
    expect(M.withInterrupt(chatOf().byId.get("a1"))).toBe(chatOf().byId.get("a1")); // finished already
  });

  it("later chunks of that run reopen nothing unless they carry a finish part", async () => {
    await running();
    await api.interrupt("s1");
    const late = msg("a2", { text: "now and more", at: 4, calls: [["t3"]] });
    await deliver(ev("message", "updated", late));
    expect(chatOf().byId.get("a2")).toBe(late);
    expect(S.busy("s1")).toBe(false);
    expect(M.withInterrupt(late).parts.at(-1).data.reason).toBe("canceled");
    expect(P.prompts.callStatus.t3).toBe("cancelled");
    await deliver(ev("permission_notification", "updated", { session_id: "s1", tool_call_id: "t3", granted: true, denied: false }));
    expect(P.prompts.callStatus.t3).toBe("cancelled");
    const done = msg("a2", { text: "now and more", at: 4, calls: [["t3"]], finished: "canceled" });
    await deliver(ev("message", "updated", done));
    expect(M.withInterrupt(done)).toBe(done);
  });

  it("a tool result still overrides the cancelled mark", async () => {
    await running();
    await api.interrupt("s1");
    await deliver(ev("message", "created", msg("r2", { role: "tool", at: 5, results: ["t3"] })));
    expect(P.prompts.callStatus.t3).toBeUndefined();
  });

  it("with prompts to send next the chat stays busy and held prompts join the chat", async () => {
    await running();
    const held = M.addPending({ sessionId: "s1", text: "next one", held: true });
    expect(M.heldFor("s1").map((p) => p.submissionId)).toEqual([held]);
    await api.interrupt("s1");
    expect(M.heldFor("s1")).toEqual([]);
    expect(M.pendingFor("s1").map((p) => p.text)).toEqual(["next one"]);
    // The reply itself stopped; the queue keeps the chat going.
    expect(M.isInterrupted("a2")).toBe(true);
  });

  it("a step that only called tools stays hidden when it first shows after the interrupt", async () => {
    await running();
    await api.interrupt("s1");
    expect(M.isHiddenInterruptedStep(msg("a2", { calls: [["t3"]] }), false)).toBe(true);
    expect(M.isHiddenInterruptedStep(msg("a2", { text: "words", calls: [["t3"]] }), false)).toBe(false);
    expect(M.isHiddenInterruptedStep(msg("a2", { calls: [["t3"]] }), true)).toBe(false);
  });
});

describe("this window's prompts (TYPE-042, TYPE-044, TYPE-048)", () => {
  it("a pending item until the saved copy with its submission_id arrives", async () => {
    await loaded("s1", []);
    const id = M.addPending({ sessionId: "s1", text: "hello" });
    expect(M.pendingFor("s1")).toMatchObject([{ submissionId: id, text: "hello", state: "working" }]);
    await deliver(ev("message", "created", msg("u1", { role: "user", text: "hello", submission: id })));
    expect(M.pendingFor("s1")).toEqual([]);
    expect(chatOf().ids).toEqual(["u1"]);
  });

  it("a transport failure keeps it, uncertain", () => {
    const id = M.addPending({ sessionId: "s1", text: "maybe" });
    M.markUncertain(id);
    expect(M.pendingFor("s1")[0].state).toBe("uncertain");
  });

  it("a stopped run removes it without giving it back", async () => {
    const id = M.addPending({ sessionId: "s1", text: "stop me" });
    await deliver(ev("run_complete", "updated", { session_id: "s1", submission_id: id, cancelled: true, error: "context canceled" }));
    expect(M.pendingFor("s1")).toEqual([]);
    expect(M.takeReturned("s1")).toEqual([]);
    expect(h.notices).toEqual([]);
  });

  it("a failed run gives back a prompt that was never saved", async () => {
    h.routes.set("GET sessions/s1/messages", [msg("u0", { role: "user", text: "older", submission: "other" })]);
    const id = M.addPending({ sessionId: "s1", text: "lost", attachments: [{ name: "a.png" }] });
    await deliver(ev("run_complete", "updated", { session_id: "s1", submission_id: id, error: "provider said no" }));
    expect(M.pendingFor("s1")).toEqual([]);
    expect(M.takeReturned("s1")).toEqual([{ text: "lost", attachments: [{ name: "a.png" }] }]);
    expect(M.takeReturned("s1")).toEqual([]);
    expect(h.notices).toEqual([]);
  });

  it("a failed run keeps a prompt that was saved", async () => {
    await loaded("s1", []);
    const id = M.addPending({ sessionId: "s1", text: "kept" });
    h.routes.set("GET sessions/s1/messages", [msg("u1", { role: "user", text: "kept", submission: id })]);
    await deliver(ev("run_complete", "updated", { session_id: "s1", submission_id: id, error: "provider said no" }));
    expect(M.pendingFor("s1")).toEqual([]);
    expect(M.takeReturned("s1")).toEqual([]);
    expect(chatOf().ids).toEqual(["u1"]);
  });
});

describe("agent tools' chats (CHAT-078)", () => {
  it("loads each agent's chat, and agents within agents", async () => {
    h.routes.set("GET sessions/a1$$call-1/messages", [msg("c1", { sid: "a1$$call-1", calls: [["call-2", "agentic_fetch"]], finished: true })]);
    h.routes.set("GET sessions/c1$$call-2/messages", [msg("d1", { sid: "c1$$call-2", calls: [["t9"]], finished: true })]);
    await loaded("s1", [msg("a1", { calls: [["call-1", "agent"], ["t0", "bash"]], finished: "tool_use" })]);
    await settle();
    expect(chatOf("a1$$call-1").ids).toEqual(["c1"]);
    expect(chatOf("c1$$call-2").ids).toEqual(["d1"]);
    expect(h.calls.filter((c) => c.path.includes("$$")).map((c) => c.path)).toEqual(["sessions/a1$$call-1/messages", "sessions/c1$$call-2/messages"]);
    M.unload("s1");
    expect(chatOf("a1$$call-1")).toBeUndefined();
    expect(chatOf("c1$$call-2")).toBeUndefined();
  });

  it("reads at most 8 agent chats at once", async () => {
    let open = 0;
    let most = 0;
    const waits = [];
    for (let i = 0; i < 12; i += 1) {
      h.routes.set(`GET sessions/a1$$call-${i}/messages`, () => {
        open += 1;
        most = Math.max(most, open);
        const wait = later();
        waits.push(wait);
        return wait.promise.then((v) => {
          open -= 1;
          return v;
        });
      });
    }
    const calls = Array.from({ length: 12 }, (_, i) => [`call-${i}`, "agent"]);
    await loaded("s1", [msg("a1", { calls, finished: "tool_use" })]);
    await settle();
    expect(most).toBe(8);
    for (let round = 0; round < 3; round += 1) {
      for (const wait of waits.splice(0)) wait.resolve([]);
      await settle();
    }
    expect(most).toBe(8);
    expect(h.calls.filter((c) => c.path.includes("$$"))).toHaveLength(12);
  });

  it("a live event of an agent's chat lands under its call and shows the agent still working", async () => {
    const a1 = msg("a1", { calls: [["call-1", "agent"]] });
    h.routes.set("GET sessions/a1$$call-1/messages", []);
    await loaded("s1", [a1]);
    await settle();
    expect(S.busy("s1")).toBe(false); // a snapshot's unfinished message alone is no proof of work
    await deliver(ev("message", "created", msg("n1", { sid: "a1$$call-1", calls: [["t1"]] })));
    expect(chatOf("a1$$call-1").ids).toEqual(["n1"]);
    expect(S.busy("s1")).toBe(true);
  });

  it("an agent's event that comes before its parent message waits while a load runs", async () => {
    const slow = later();
    h.routes.set("GET sessions/s1/messages", () => slow.promise);
    h.routes.set("GET sessions/a1$$call-1/messages", () => later().promise);
    const loading = M.load("s1");
    await settle();
    await deliver(ev("message", "created", msg("n1", { sid: "a1$$call-1", calls: [["t1"]] })));
    expect(chatOf("a1$$call-1")).toBeUndefined();
    slow.resolve([msg("a1", { calls: [["call-1", "agent"]] })]);
    await loading;
    expect(chatOf("a1$$call-1").ids).toEqual(["n1"]);
  });

  it("an agent's event with no parent anywhere is dropped", async () => {
    await deliver(ev("message", "created", msg("n1", { sid: "zz$$call-1" })));
    expect(chatOf("zz$$call-1")).toBeUndefined();
  });
});

describe("tree_changed", () => {
  it("reloads the shown chat only", async () => {
    await loaded("s1", [msg("m1", { finished: true })]);
    S.sessions.currentId = "s1";
    h.routes.set("GET sessions/s1/messages", [msg("m1", { finished: true }), msg("m2", { finished: true, at: 3 })]);
    await deliver(ev("tree_changed", "updated", { session_id: "s2", revision: 4 }));
    expect(chatOf().ids).toEqual(["m1"]);
    await deliver(ev("tree_changed", "updated", { session_id: "s1", revision: 4 }));
    expect(chatOf().ids).toEqual(["m1", "m2"]);
  });
});
