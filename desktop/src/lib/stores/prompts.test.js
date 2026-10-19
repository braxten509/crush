import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The real event loop (events.js) with fake Tauri calls: the test hands
// batches to events_take and rings the doorbell; engine answers come from
// `routes` ("METHOD path" → value or fn); an unknown GET fails as not_found.
const h = vi.hoisted(() => ({ routes: new Map(), calls: [], entries: [], ring: null, current: [] }));

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
async function settle(rounds = 8) {
  for (let i = 0; i < rounds; i += 1) await tick();
}
async function deliver(...entries) {
  h.entries.push(...entries);
  h.ring?.();
  await settle();
}
const ev = (type, event, payload) => ({ kind: "event", type, event, payload });

let sessions;
let prompts;
let ui;
let events;

beforeEach(async () => {
  vi.resetModules();
  h.routes.clear();
  h.calls.length = 0;
  h.entries.length = 0;
  h.ring = null;
  h.current.length = 0;
  vi.stubGlobal("requestAnimationFrame", (fn) => setTimeout(() => fn(0), 0));
  events = await import("../engine/events.js");
  sessions = await import("./sessions.svelte.js");
  prompts = await import("./prompts.svelte.js");
  ui = await import("./ui.svelte.js");
  await events.startEvents();
  await settle();
  // The shown chat; P3.7's prompt opens the slot whenever a request is stored.
  sessions.upsertSession({ id: "s1", title: "Shown", updated_at: 10 });
  sessions.upsertSession({ id: "s2", title: "Other", updated_at: 5 });
  sessions.sessions.currentId = "s1";
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const request = (toolCallId, sessionId = "s1") => ({
  id: `perm-${toolCallId}`,
  session_id: sessionId,
  tool_call_id: toolCallId,
  tool_name: "bash",
  description: "Run a command",
  action: "execute",
  params: { command: "ls" },
  path: "/tmp/home/proj",
});
const notice = (toolCallId, flags = {}) => ({ session_id: "s1", tool_call_id: toolCallId, granted: false, denied: false, ...flags });

// What P3.7's prompt does with a stored request: it holds the slot.
function drawSlot() {
  if (prompts.prompts.permission) ui.openSlot({ kind: "permission", component: null, props: {} });
}

describe("the permission notification rule (DLG-119)", () => {
  it("a request, then the requested mark, keeps the slot; granted closes it", async () => {
    await deliver(ev("permission_request", "created", request("call-1")));
    drawSlot();
    await deliver(ev("permission_notification", "updated", notice("call-1")));
    expect(prompts.prompts.permission?.tool_call_id).toBe("call-1");
    expect(ui.ui.slot?.kind).toBe("permission");
    expect(prompts.prompts.callStatus["call-1"]).toBe("permission");

    await deliver(ev("permission_notification", "updated", notice("call-1", { granted: true })));
    expect(prompts.prompts.permission).toBeNull();
    expect(ui.ui.slot).toBeNull();
    expect(prompts.prompts.callStatus["call-1"]).toBe("running");
  });

  it("the requested mark first, then the request, ends the same way", async () => {
    await deliver(ev("permission_notification", "updated", notice("call-1")));
    expect(prompts.prompts.callStatus["call-1"]).toBe("permission");
    expect(prompts.prompts.permission).toBeNull();
    await deliver(ev("permission_request", "created", request("call-1")));
    drawSlot();
    expect(ui.ui.slot?.kind).toBe("permission");
    await deliver(ev("permission_notification", "updated", notice("call-1", { granted: true })));
    expect(prompts.prompts.permission).toBeNull();
    expect(ui.ui.slot).toBeNull();
  });

  it("both in one batch, in either order, keep the slot open", async () => {
    await deliver(ev("permission_notification", "updated", notice("call-1")), ev("permission_request", "created", request("call-1")));
    expect(prompts.prompts.permission?.tool_call_id).toBe("call-1");
    prompts.clearPermission();
    await deliver(ev("permission_request", "created", request("call-2")), ev("permission_notification", "updated", notice("call-2")));
    expect(prompts.prompts.permission?.tool_call_id).toBe("call-2");
  });

  it("an answer for another tool call leaves the slot open", async () => {
    await deliver(ev("permission_request", "created", request("call-1")));
    drawSlot();
    await deliver(ev("permission_notification", "updated", notice("call-9", { granted: true })));
    expect(prompts.prompts.permission?.tool_call_id).toBe("call-1");
    expect(ui.ui.slot?.kind).toBe("permission");
    await deliver(ev("permission_notification", "updated", notice("call-9", { denied: true })));
    expect(ui.ui.slot?.kind).toBe("permission");
  });

  it("denied closes the slot and keeps the tool at permission until its result", async () => {
    await deliver(ev("permission_request", "created", request("call-1")));
    drawSlot();
    await deliver(ev("permission_notification", "updated", notice("call-1", { denied: true })));
    expect(prompts.prompts.permission).toBeNull();
    expect(ui.ui.slot).toBeNull();
    expect(prompts.prompts.callStatus["call-1"]).toBe("permission");
  });

  it("a newer request replaces the shown one", async () => {
    await deliver(ev("permission_request", "created", request("call-1")));
    await deliver(ev("permission_request", "created", request("call-2")));
    expect(prompts.prompts.permission?.tool_call_id).toBe("call-2");
  });

  it("a tool this window cancelled stays cancelled", async () => {
    prompts.prompts.callStatus["call-1"] = "cancelled";
    await deliver(ev("permission_notification", "updated", notice("call-1", { granted: true })));
    expect(prompts.prompts.callStatus["call-1"]).toBe("cancelled");
  });

  it("drops a request for a chat this window has not seen, and takes a helper's for the shown chat", async () => {
    await deliver(ev("permission_request", "created", request("call-1", "unknown")));
    expect(prompts.prompts.permission).toBeNull();
    await deliver(ev("permission_request", "created", request("call-2", "m1$$call-agent")));
    expect(prompts.prompts.permission?.tool_call_id).toBe("call-2");
    await deliver(ev("permission_notification", "updated", { ...notice("call-2", { granted: true }), session_id: "m1$$call-agent" }));
    expect(prompts.prompts.permission).toBeNull();
  });

  it("a child chat named by its session record counts for its parent", async () => {
    await deliver(ev("session", "created", { id: "task-1", parent_session_id: "s1", title: "Task" }));
    await deliver(ev("permission_request", "created", request("call-3", "task-1")));
    expect(prompts.prompts.permission?.session_id).toBe("task-1");
    expect(sessions.rootOf("task-1")).toBe("s1");
  });
});

describe("waiting marks (prompt_pending)", () => {
  it("sets the chat's pending_prompt, a permission winning over a question", async () => {
    const mark = (kind, pending) => ev("prompt_pending", "updated", { session_id: "s2", kind, pending });
    await deliver(mark("question", true));
    expect(sessions.sessions.byId.s2.pending_prompt).toBe("question");
    await deliver(mark("permission", true));
    expect(sessions.sessions.byId.s2.pending_prompt).toBe("permission");
    await deliver(mark("permission", false));
    expect(sessions.sessions.byId.s2.pending_prompt).toBe("question");
    await deliver(mark("question", false));
    expect(sessions.sessions.byId.s2.pending_prompt).toBe("");
    expect(prompts.prompts.pendingByRoot.s2).toEqual({ permission: false, question: false });
  });

  it("drops a mark for a chat this window has not seen", async () => {
    await deliver(ev("prompt_pending", "updated", { session_id: "nope", kind: "permission", pending: true }));
    expect(prompts.prompts.pendingByRoot.nope).toBeUndefined();
    expect(sessions.sessions.byId.nope).toBeUndefined();
  });

  it("a session event never clears the mark (a protected field)", async () => {
    await deliver(ev("prompt_pending", "updated", { session_id: "s2", kind: "permission", pending: true }));
    await deliver(ev("session", "updated", { id: "s2", title: "Renamed", updated_at: 20 }));
    expect(sessions.sessions.byId.s2.title).toBe("Renamed");
    expect(sessions.sessions.byId.s2.pending_prompt).toBe("permission");
  });
});

describe("questions and switching chats", () => {
  it("a question notification clears the shown batch", async () => {
    await deliver(ev("question_batch_request", "created", { id: "q1", session_id: "s1", tool_call_id: "c1", questions: [] }));
    expect(prompts.prompts.question?.id).toBe("q1");
    await deliver(ev("question_batch_notification", "updated", { batch_id: "q1", session_id: "s1" }));
    expect(prompts.prompts.question).toBeNull();
  });

  it("showing another chat drops the prompts this window can no longer answer", async () => {
    await deliver(ev("permission_request", "created", request("call-1")));
    await deliver(ev("question_batch_request", "created", { id: "q1", session_id: "s1", questions: [] }));
    drawSlot();
    prompts.keepPromptsFor("s1");
    expect(prompts.prompts.permission).not.toBeNull();
    prompts.keepPromptsFor("s2");
    expect(prompts.prompts.permission).toBeNull();
    expect(prompts.prompts.question).toBeNull();
    expect(ui.ui.slot).toBeNull();
  });

  it("a resync drops a request answered while the stream was down", async () => {
    await deliver(ev("permission_request", "created", request("call-1")));
    h.routes.set("GET agent/sessions/s1", { id: "s1", title: "Shown", updated_at: 10, is_busy: true, pending_prompt: "" });
    await deliver({ kind: "resync" });
    expect(prompts.prompts.permission).toBeNull();
    expect(sessions.sessions.byId.s1.is_busy).toBe(true);
  });

  it("a resync keeps a request the chat still waits on", async () => {
    await deliver(ev("permission_request", "created", request("call-1")));
    h.routes.set("GET agent/sessions/s1", { id: "s1", title: "Shown", updated_at: 10, pending_prompt: "permission" });
    await deliver({ kind: "resync" });
    expect(prompts.prompts.permission?.tool_call_id).toBe("call-1");
    expect(prompts.prompts.pendingByRoot.s1).toEqual({ permission: true, question: false });
  });
});
