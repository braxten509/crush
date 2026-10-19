import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// 1.5.3, "Events that name a chat this window has not seen": engine-sent and
// fan-in events have no order between them (engine-design.md:107), so
// `session created` and the chat's first `prompt_pending` or first assistant
// `message` can arrive either way round. Each order must end with the same
// waiting mark and busy state. The real event loop (events.js) applies the
// batches; the fake engine answers a chat read with its state at that moment.
const h = vi.hoisted(() => ({ routes: new Map(), calls: [], entries: [], ring: null, engine: { pending: "", busy: false }, reads: [] }));

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
    if (method === "GET" && path === "sessions/s9") {
      // The engine computes the four fields when it reads; the answer comes
      // back when the test lets it.
      const answer = { id: "s9", title: "From the terminal", parent_session_id: "", created_at: 5, updated_at: 5, is_busy: h.engine.busy, pending_prompt: h.engine.pending, attached_clients: 1, has_unfinished_work: false };
      return new Promise((resolve) => h.reads.push(() => resolve(answer)));
    }
    const route = h.routes.get(`${method} ${path}`);
    if (route !== undefined) return typeof route === "function" ? route() : route;
    if (method === "GET") return Promise.reject({ code: "not_found", message: `no answer for GET ${path}` });
    return null;
  },
  api_abort: async () => true,
  engine_set_current_session: async () => null,
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
async function answerReads() {
  for (const answer of h.reads.splice(0)) answer();
  await settle();
}
const ev = (type, event, payload) => ({ kind: "event", type, event, payload });

let S;
let P;

beforeEach(async () => {
  vi.resetModules();
  h.routes.clear();
  h.calls.length = 0;
  h.entries.length = 0;
  h.ring = null;
  h.reads.length = 0;
  h.engine.pending = "";
  h.engine.busy = false;
  vi.stubGlobal("requestAnimationFrame", (fn) => setTimeout(() => fn(0), 0));
  const events = await import("../engine/events.js");
  S = await import("./sessions.svelte.js");
  P = await import("./prompts.svelte.js");
  await import("./messages.svelte.js");
  await events.startEvents();
  await settle();
  // This window shows another chat; s9 is new, started by another app.
  S.upsertSession({ id: "s1", title: "Shown", updated_at: 1 });
  S.sessions.currentId = "s1";
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const created = ev("session", "created", { id: "s9", title: "From the terminal", parent_session_id: "", created_at: 5, updated_at: 5 });
const asks = ev("prompt_pending", "updated", { session_id: "s9", kind: "permission", pending: true });
const reply = (finished) =>
  ev("message", finished ? "updated" : "created", {
    id: "m1",
    role: "assistant",
    session_id: "s9",
    parts: [{ type: "text", data: { text: "Working on it" } }, ...(finished ? [{ type: "finish", data: { reason: "end_turn", time: 7 } }] : [])],
    created_at: 6,
    updated_at: 6,
  });

const mark = () => S.sessions.byId.s9?.pending_prompt;

describe("session created and prompt_pending", () => {
  it("created first, then the mark, in one batch", async () => {
    h.engine.pending = "permission"; // the prompt is up before the window reads
    await deliver(created, asks);
    await answerReads();
    expect(mark()).toBe("permission");
    expect(P.prompts.pendingByRoot.s9).toEqual({ permission: true, question: false });
  });

  it("the mark first, then created, in one batch", async () => {
    h.engine.pending = "permission";
    await deliver(asks, created);
    await answerReads();
    expect(mark()).toBe("permission");
    expect(P.prompts.pendingByRoot.s9).toEqual({ permission: true, question: false });
  });

  it("created first, the read computed before the prompt, the mark after it", async () => {
    await deliver(created); // the read goes out now, while nothing waits
    h.engine.pending = "permission";
    await deliver(asks);
    await answerReads(); // its answer says "" but is older than the mark
    expect(mark()).toBe("permission");
  });

  it("the mark first, in its own batch, then created", async () => {
    h.engine.pending = "permission";
    await deliver(asks);
    expect(S.sessions.byId.s9).toBeUndefined(); // dropped: nothing waits for the chat
    await deliver(created);
    await answerReads();
    expect(mark()).toBe("permission");
  });

  it("the read happens once, whichever order", async () => {
    h.engine.pending = "permission";
    await deliver(asks, created, created);
    await answerReads();
    expect(h.calls.filter((c) => c.path === "sessions/s9")).toHaveLength(1);
  });
});

describe("session created and the first assistant message", () => {
  it("created first, then the reply: busy, then idle after run_complete", async () => {
    h.engine.busy = true;
    await deliver(created, reply(false));
    expect(S.busy("s9")).toBe(true);
    await answerReads();
    expect(S.busy("s9")).toBe(true);
    await deliver(reply(true), ev("run_complete", "updated", { session_id: "s9", message_id: "m1" }));
    h.engine.busy = false;
    expect(S.busy("s9")).toBe(false);
  });

  it("the reply first, then created: busy, then idle after run_complete", async () => {
    h.engine.busy = true;
    await deliver(reply(false), created);
    await answerReads();
    expect(S.busy("s9")).toBe(true);
    await deliver(reply(true), ev("run_complete", "updated", { session_id: "s9", message_id: "m1" }));
    expect(S.busy("s9")).toBe(false);
  });

  it("created first with a read computed before the run started still shows it busy", async () => {
    await deliver(created); // is_busy false at read time
    h.engine.busy = true;
    await deliver(reply(false));
    await answerReads();
    expect(S.busy("s9")).toBe(true);
  });

  it("the reply in its own batch first, then created", async () => {
    h.engine.busy = true;
    await deliver(reply(false));
    expect(S.busy("s9")).toBe(false); // dropped: the chat is unknown yet
    await deliver(created);
    await answerReads();
    expect(S.busy("s9")).toBe(true);
    await deliver(ev("run_complete", "updated", { session_id: "s9" }));
    expect(S.busy("s9")).toBe(false);
  });

  it("a run_complete before the read's answer is not undone by it", async () => {
    h.engine.busy = true;
    await deliver(reply(false), created); // the read goes out while the run is on
    await deliver(reply(true), ev("run_complete", "updated", { session_id: "s9" }));
    await answerReads(); // computed while busy, answered after the end
    expect(S.busy("s9")).toBe(false);
  });
});
