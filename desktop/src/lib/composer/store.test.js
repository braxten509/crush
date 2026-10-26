import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The composer store (lib/stores/composer.svelte.js) with the real event loop
// and stores, and fake Tauri calls: engine answers come from `routes`
// ("METHOD path" → value or fn); an unknown GET fails as not_found.
const h = vi.hoisted(() => ({ routes: new Map(), calls: [], entries: [], ring: null, notices: [], abort: true, files: new Map() }));

vi.mock("@tauri-apps/api/webviewWindow", () => ({
  getCurrentWebviewWindow: () => ({
    listen: async (_name, fn) => {
      h.ring = fn;
      return () => {};
    },
  }),
}));

vi.mock("../engine/commands.js", async (importOriginal) => ({
  ...(await importOriginal()),
  api: async (method, path, query, body, callId) => {
    h.calls.push({ method, path, query, body, callId });
    const route = h.routes.get(`${method} ${path}`);
    if (typeof route === "function") return route({ query, body, callId });
    if (route !== undefined) return route;
    if (method === "GET") return Promise.reject({ code: "not_found", message: `no answer for GET ${path}` });
    return null;
  },
  api_abort: async (callId) => {
    h.calls.push({ method: "ABORT", path: callId });
    return h.abort;
  },
  engine_set_current_session: async () => null,
  events_take: async () => h.entries.splice(0),
  lastClipboardWrite: () => null,
  files_list: async () => [...h.files.keys()].map((path) => ({ path, is_dir: false })),
  file_info: async (path) => {
    const file = h.files.get(path);
    return file ? { exists: true, is_dir: false, size: 4, mime: "text/plain; charset=utf-8", modified: file.modified } : { exists: false, is_dir: false, size: 0, mime: "", modified: 0 };
  },
  file_read: async (path) => {
    if (!h.files.has(path)) throw { code: "bad_path", message: `open ${path}: no such file or directory` };
    return { mime: "text/plain; charset=utf-8", base64: "dGV4dA==" };
  },
}));

const tick = () => new Promise((resolve) => setTimeout(resolve, 0));
async function settle(rounds = 12) {
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

let C; // composer store
let S; // sessions store
let M; // messages store
let Q; // queue store
let W; // workspace store
let conn;

beforeEach(async () => {
  vi.resetModules();
  h.routes.clear();
  h.calls.length = 0;
  h.entries.length = 0;
  h.ring = null;
  h.notices.length = 0;
  h.abort = true;
  h.files.clear();
  vi.stubGlobal("requestAnimationFrame", (fn) => setTimeout(() => fn(0), 0));
  vi.doMock("../stores/ui.svelte.js", async (importOriginal) => {
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
  S = await import("../stores/sessions.svelte.js");
  M = await import("../stores/messages.svelte.js");
  Q = await import("../stores/queue.svelte.js");
  W = await import("../stores/workspace.svelte.js");
  ({ conn } = await import("../stores/connection.svelte.js"));
  C = await import("../stores/composer.svelte.js");
  await events.startEvents();
  await settle();
  conn.workspace = { id: "ws1", path: "/proj" };
  W.ws.agent = { is_ready: true, model: { name: "Opus 5.5", supports_attachments: true }, model_cfg: { reasoning_effort: "max" } };
  h.routes.set("GET agent/sessions/s1/prompts/queued", 0);
  h.routes.set("GET agent/sessions/s1/prompts/list", []);
  h.routes.set("GET sessions/s1/messages/user", []);
  h.routes.set("GET messages/user", []);
  h.routes.set("GET /v1/prompt-history", []);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

const record = (id, extra = {}) => ({ id, title: `Chat ${id}`, parent_session_id: "", created_at: 1, updated_at: 1, is_busy: false, attached_clients: 1, has_unfinished_work: false, pending_prompt: "", ...extra });

// The window shows chat id, loaded and empty.
function show(id, extra = {}) {
  S.upsertSession(record(id, extra));
  M.readyChat(id);
  S.sessions.currentId = id;
  C.chatShown(id);
}

const sends = () => h.calls.filter((c) => c.method === "POST" && c.path === "agent");
const lastNotice = () => h.notices[h.notices.length - 1] ?? null;

describe("sending", () => {
  it("sends the text with a pending item, then empties the field (TYPE-004, TYPE-042)", async () => {
    show("s1");
    C.setText("  hello there  ");
    expect(await C.send()).toBe(true);
    expect(sends()).toHaveLength(1);
    const body = sends()[0].body;
    expect(body).toMatchObject({ session_id: "s1", prompt: "hello there" });
    expect(M.messages.pending[body.submission_id]).toMatchObject({ text: "hello there", sessionId: "s1", held: false });
    expect(C.draftOf().text).toBe("");
    // Every sent prompt goes to the history shared by every folder and app.
    expect(h.calls.filter((c) => c.method === "POST" && c.path === "/v1/prompt-history").map((c) => c.body)).toEqual([{ text: "hello there" }]);
  });

  it("sends nothing for an empty field (TYPE-004)", async () => {
    show("s1");
    C.setText("   ");
    expect(await C.send()).toBe(false);
    expect(sends()).toHaveLength(0);
  });

  it("turns a trailing backslash into a new line instead of sending (TYPE-006)", async () => {
    show("s1");
    C.setText("first line\\");
    expect(await C.send()).toBe(false);
    expect(C.draftOf().text).toBe("first line\n");
    expect(sends()).toHaveLength(0);
  });

  it("asks to quit on exit and quit, and keeps the text without a quit sheet (TYPE-007)", async () => {
    show("s1");
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    C.setText(" quit ");
    expect(await C.send()).toBe(false);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('"quit"'));
    expect(sends()).toHaveLength(0);
    expect(C.draftOf().text).toBe(" quit ");
    warn.mockRestore();
  });

  it("keeps the text while a mode switch is on its way (TYPE-008)", async () => {
    show("s1");
    const slow = later();
    h.routes.set("POST agent/main", () => slow.promise);
    const switching = C.cycleMode();
    await settle();
    expect(C.composer.switching).toBe(true);
    C.setText("hello");
    expect(await C.send()).toBe(false);
    expect(lastNotice()).toEqual({ kind: "info", text: "Switching input mode, one moment..." });
    expect(C.draftOf().text).toBe("hello");
    expect(sends()).toHaveLength(0);
    slow.resolve(null);
    await switching;
    expect(C.composer.input).toBe("plan");
    expect(C.currentMode()).toBe("plan");
  });

  it("does not send while the agent is not ready, and keeps the text (TYPE-043)", async () => {
    show("s1");
    W.ws.agent = { is_ready: false };
    C.setText("hello");
    expect(await C.send()).toBe(false);
    expect(lastNotice()).toEqual({ kind: "error", text: "coder agent is not initialized" });
    expect(C.draftOf().text).toBe("hello");
    expect(sends()).toHaveLength(0);
  });

  it("holds a prompt sent while the chat is busy (TYPE-044)", async () => {
    show("s1", { is_busy: true });
    C.setText("next one");
    await C.send();
    const id = sends()[0].body.submission_id;
    expect(M.messages.pending[id].held).toBe(true);
    expect(M.heldFor("s1")).toHaveLength(1);
  });

  describe("a failed send (TYPE-048)", () => {
    it("an engine refusal puts the text and attachments back with the error", async () => {
      show("s1");
      h.routes.set("POST agent", () => Promise.reject({ code: "bad_request", message: "prompt refused", status: 400 }));
      h.routes.set("GET sessions/s1/messages", []);
      C.setText("hello");
      C.addAttachment({ kind: "text", name: "notes.txt", mime: "text/plain", path: "notes.txt", base64: "aGk=" });
      await C.send();
      await settle();
      expect(h.notices).toContainEqual({ kind: "error", text: "prompt refused" });
      expect(Object.keys(M.messages.pending)).toHaveLength(0);
      expect(C.restoreReturned("s1")).toBe(true);
      expect(C.draftOf().text).toBe("hello");
      expect(C.draftOf().attachments.map((a) => a.name)).toEqual(["notes.txt"]);
    });

    it("a refused prompt the engine saved after all stays sent", async () => {
      show("s1");
      let id = "";
      h.routes.set("POST agent", ({ body }) => {
        id = body.submission_id;
        return Promise.reject({ code: "conflict", message: "busy", status: 409 });
      });
      h.routes.set("GET sessions/s1/messages", () => [{ id: "m1", role: "user", session_id: "s1", parts: [{ type: "text", data: { text: "hello", submission_id: id } }], created_at: 2, updated_at: 2 }]);
      C.setText("hello");
      await C.send();
      await settle();
      expect(M.messages.returned.s1).toBeUndefined();
      expect(M.messages.bySession.s1.byId.has("m1")).toBe(true);
    });

    it("a broken connection keeps the item as maybe not sent", async () => {
      show("s1");
      h.routes.set("POST agent", () => Promise.reject({ code: "transport", message: "connection reset" }));
      h.routes.set("GET sessions/s1/messages", []);
      C.setText("hello");
      await C.send();
      await settle();
      const items = Object.values(M.messages.pending);
      expect(items).toHaveLength(1);
      expect(items[0].state).toBe("uncertain");
      expect(M.messages.returned.s1).toBeUndefined();
      expect(h.notices).toContainEqual({ kind: "error", text: "connection reset" });
    });

    it("a send made while reconnecting was never sent: the text comes back", async () => {
      show("s1");
      h.routes.set("POST agent", () => Promise.reject({ code: "engine_unavailable", message: "not connected to the Crush server" }));
      C.setText("hello");
      await C.send();
      await settle();
      expect(Object.keys(M.messages.pending)).toHaveLength(0);
      expect(M.messages.returned.s1).toEqual([{ text: "hello", attachments: [] }]);
      expect(h.calls.some((c) => c.path === "sessions/s1/messages")).toBe(false);
    });

    it("a send refused while the connection has failed keeps the failed notice and its Try again (SIDE-030)", async () => {
      show("s1");
      await deliver(ev("connection", "updated", { state: "connected", attempt: 0 }));
      await deliver(ev("connection", "updated", { state: "failed", attempt: 3, error: { code: "engine_unavailable", message: "ensure failed" } }));
      h.routes.set("POST agent", () => Promise.reject({ code: "engine_unavailable", message: "Crush Desktop lost its connection to the Crush engine; nothing was sent." }));
      C.setText("hello");
      await C.send();
      await settle();
      const { ui } = await import("../stores/ui.svelte.js");
      expect(ui.notice.text).toBe("Can't restore the connection to the Crush server. Restart Crush to recover. Nothing was sent.");
      expect(ui.notice.action?.label).toBe("Try again");
      expect(M.messages.returned.s1).toEqual([{ text: "hello", attachments: [] }]);
    });

    it("a stopped send is dropped without coming back", async () => {
      show("s1");
      h.routes.set("POST agent", () => Promise.reject({ code: "aborted", message: "stopped" }));
      C.setText("hello");
      await C.send();
      await settle();
      expect(Object.keys(M.messages.pending)).toHaveLength(0);
      expect(M.messages.returned.s1).toBeUndefined();
    });
  });

  it("holds a send made while another chat opens and sends it there (TYPE-049)", async () => {
    show("s1");
    S.upsertSession(record("s2"));
    const slow = later();
    h.routes.set("GET sessions/s2", () => slow.promise);
    h.routes.set("GET sessions/s2/history", []);
    h.routes.set("GET sessions/s2/messages", []);
    h.routes.set("GET agent/sessions/s2/prompts/queued", 0);
    h.routes.set("GET agent/sessions/s2/prompts/list", []);
    const opening = S.openChat("s2");
    await settle();
    C.setText("for the other chat");
    await C.send();
    await settle();
    expect(sends()).toHaveLength(0);
    slow.resolve(record("s2"));
    await opening;
    await settle();
    expect(sends()).toHaveLength(1);
    expect(sends()[0].body.session_id).toBe("s2");
  });

  it("gives a held send back to the field when the other chat does not open (TYPE-049)", async () => {
    show("s1");
    S.upsertSession(record("s2"));
    const slow = later();
    h.routes.set("GET sessions/s2", () => slow.promise);
    h.routes.set("GET sessions/s2/history", []);
    h.routes.set("GET sessions/s2/messages", []);
    const opening = S.openChat("s2");
    await settle();
    C.setText("for the other chat");
    await C.send();
    slow.reject({ code: "not_found", message: "session not found" });
    await opening;
    await settle();
    expect(sends()).toHaveLength(0);
    expect(M.messages.returned.s1).toEqual([{ text: "for the other chat", attachments: [] }]);
  });

  it("makes the chat on the landing page, keeping skills turned on before it first (TYPE-041)", async () => {
    S.sessions.currentId = "";
    C.chatShown("");
    h.routes.set("POST sessions", record("n1", { title: "New Session" }));
    h.routes.set("GET agent/sessions/n1/prompts/queued", 0);
    h.routes.set("GET agent/sessions/n1/prompts/list", []);
    h.routes.set("GET sessions/n1/messages/user", []);
    await C.keepSkill({ id: "sk1", name: "demo" }, true);
    expect(C.keptSkills().map((s) => s.name)).toEqual(["demo"]);
    C.setText("first prompt");
    await C.send();
    await settle();
    const order = h.calls.filter((c) => c.method === "POST" && c.path !== "/v1/prompt-history").map((c) => c.path);
    expect(order).toEqual(["sessions", "sessions/n1/kept-skills", "agent"]);
    expect(h.calls.find((c) => c.path === "sessions/n1/kept-skills").body).toEqual({ id: "sk1", name: "demo", on: true, toggleable: false });
    expect(sends()[0].body.session_id).toBe("n1");
    expect(S.sessions.currentId).toBe("n1");
    expect(C.composer.pendingKept).toEqual([]);
  });

  it("keeps the text when the chat cannot be made (TYPE-041)", async () => {
    S.sessions.currentId = "";
    C.chatShown("");
    h.routes.set("POST sessions", () => Promise.reject({ code: "internal", message: "disk full", status: 500 }));
    C.setText("first prompt");
    await C.send();
    await settle();
    expect(h.notices).toContainEqual({ kind: "error", text: "disk full" });
    expect(M.messages.returned[""]).toEqual([{ text: "first prompt", attachments: [] }]);
    expect(sends()).toHaveLength(0);
  });
});

describe("! commands (TYPE-020, TYPE-021)", () => {
  function typeBang(command) {
    C.typed({ before: "", value: "!", caret: 1, data: "!" });
    expect(C.draftOf().bang).toBe(true);
    C.typed({ before: "", value: command, caret: command.length, data: command.at(-1) });
  }

  it("makes the chat first on the landing page and streams the output in seq order", async () => {
    S.sessions.currentId = "";
    C.chatShown("");
    h.routes.set("POST sessions", record("n1", { title: "New Session" }));
    h.routes.set("GET sessions/n1/messages/user", []);
    const answer = later();
    let requestId = "";
    h.routes.set("POST agent/sessions/n1/shell", ({ query, body, callId }) => {
      requestId = query.request_id;
      expect(callId).toBe(query.request_id);
      expect(body).toMatchObject({ command: "echo hi", is_first_message: true });
      return answer.promise;
    });
    typeBang("echo hi");
    expect(C.currentMode()).toBe("bang");
    expect(C.currentPlaceholder()).toBe("Run a shell command");
    expect(await C.send()).toBe(true);
    await settle();
    expect(h.calls.some((c) => c.path === "agent")).toBe(false);
    expect(C.composer.shells[requestId]).toMatchObject({ sessionId: "n1", command: "echo hi", running: true });
    expect(C.draftOf("n1").bang).toBe(false);
    await deliver(ev("shell_output", "created", { request_id: requestId, session_id: "n1", seq: 1, chunk: "b" }));
    expect(C.composer.shells[requestId].output).toBe("");
    await deliver(ev("shell_output", "created", { request_id: requestId, session_id: "n1", seq: 0, chunk: "a" }));
    expect(C.composer.shells[requestId].output).toBe("ab");
    answer.resolve({ output: "hi\n", exit_code: 0 });
    await settle();
    expect(C.composer.shells[requestId]).toMatchObject({ running: false, exitCode: 0, output: "hi\n" });
    await deliver(ev("message", "created", { id: "m9", role: "user", session_id: "n1", parts: [{ type: "shell_command", data: { command: "echo hi", output: "hi\n", exit_code: 0 } }], created_at: 3, updated_at: 3 }));
    expect(C.composer.shells[requestId]).toBeUndefined();
  });

  it("Esc closes the command's own call and ends it with exit 130 at once, kept locally", async () => {
    show("s1");
    let requestId = "";
    const answer = later();
    h.routes.set("POST agent/sessions/s1/shell", ({ query }) => {
      requestId = query.request_id;
      return answer.promise;
    });
    typeBang("sleep 30");
    await C.send();
    await settle();
    expect(C.working()).toBe(true);
    expect(C.currentPlaceholder()).not.toBe("Run a shell command");
    expect(await C.stop()).toBe(true);
    await settle();
    expect(h.calls).toContainEqual({ method: "ABORT", path: requestId });
    expect(C.composer.shells[requestId]).toMatchObject({ running: false, exitCode: 130 });
    answer.reject({ code: "aborted", message: "stopped" });
    await settle();
    expect(C.composer.shells[requestId]).toMatchObject({ running: false, exitCode: 130 });
    expect(h.calls.some((c) => c.path.endsWith("/cancel"))).toBe(false);
  });

  it("a command that already ended finishes from its answer", async () => {
    show("s1");
    h.abort = false;
    let requestId = "";
    const answer = later();
    h.routes.set("POST agent/sessions/s1/shell", ({ query }) => {
      requestId = query.request_id;
      return answer.promise;
    });
    typeBang("true");
    await C.send();
    await settle();
    await C.stop();
    await settle();
    expect(C.composer.shells[requestId].running).toBe(true);
    answer.resolve({ output: "", exit_code: 0 });
    await settle();
    expect(C.composer.shells[requestId]).toMatchObject({ running: false, exitCode: 0 });
  });

  it("shows a failed command as an error notice", async () => {
    show("s1");
    h.routes.set("POST agent/sessions/s1/shell", () => Promise.reject({ code: "internal", message: "no shell", status: 500 }));
    typeBang("ls");
    await C.send();
    await settle();
    expect(h.notices).toContainEqual({ kind: "error", text: "shell: no shell" });
    expect(Object.keys(C.composer.shells)).toHaveLength(0);
  });

  it("Backspace on an empty bang prompt leaves bang mode", () => {
    show("s1");
    C.typed({ before: "", value: "!", caret: 1, data: "!" });
    expect(C.draftOf().bang).toBe(true);
    expect(C.backspaceLeavesBang()).toBe(true);
    expect(C.currentMode()).toBe("normal");
  });
});

describe("stopping (TYPE-046, TYPE-047)", () => {
  it("Esc while busy posts interrupt, never cancel", async () => {
    show("s1", { is_busy: true });
    expect(await C.stop()).toBe(true);
    expect(h.calls.filter((c) => c.path === "agent/sessions/s1/interrupt")).toHaveLength(1);
    expect(h.calls.some((c) => c.path.endsWith("/cancel"))).toBe(false);
  });

  it("Esc while idle does nothing", async () => {
    show("s1");
    expect(await C.stop()).toBe(false);
    expect(h.calls.some((c) => c.path.endsWith("/interrupt"))).toBe(false);
  });

  it("sends one interrupt while one is on its way (TYPE-047)", async () => {
    show("s1", { is_busy: true });
    const slow = later();
    h.routes.set("POST agent/sessions/s1/interrupt", () => slow.promise);
    const first = C.interruptAndSend();
    await settle();
    S.sessions.byId.s1.is_busy = false;
    expect(C.interruptPending()).toBe(true);
    expect(await C.interruptAndSend()).toBe(true);
    slow.resolve(null);
    await first;
    expect(h.calls.filter((c) => c.path === "agent/sessions/s1/interrupt")).toHaveLength(1);
  });
});

describe("input mode and YOLO (TYPE-023 to TYPE-025)", () => {
  const skips = () => h.calls.filter((c) => c.path === "permissions/skip").map((c) => c.body.skip);
  const mains = () => h.calls.filter((c) => c.path === "agent/main").map((c) => c.body.agent_id);

  it("cycles code → plan → code with YOLO → code, turning off only the YOLO it turned on", async () => {
    show("s1");
    await C.cycleMode();
    expect(C.composer.input).toBe("plan");
    expect(lastNotice()).toEqual({ kind: "plan", text: "Plan with Crush before generating any code." });
    expect(C.currentPlaceholder()).toBe("Let's plan");
    await C.cycleMode();
    expect(C.composer.input).toBe("code");
    expect(C.composer.yolo).toBe(true);
    expect(C.currentMode()).toBe("yolo");
    expect(C.currentPlaceholder()).toBe("Go crazy");
    expect(lastNotice()).toEqual({ kind: "yolo", text: "Skip permission prompts. System level commands will be blocked." });
    await C.cycleMode();
    expect(C.composer.yolo).toBe(false);
    expect(lastNotice()).toEqual({ kind: "info", text: "input mode: code" });
    expect(skips()).toEqual([true, false]);
    expect(mains()).toEqual(["plan", "coder"]);
  });

  it("keeps YOLO the user turned on through the cycle", async () => {
    show("s1");
    await C.toggleYolo();
    await C.cycleMode();
    await C.cycleMode();
    expect(C.composer.input).toBe("code");
    expect(C.composer.yolo).toBe(true);
    await C.cycleMode();
    expect(C.composer.input).toBe("plan");
  });

  it("refuses to switch while the chat is busy", async () => {
    show("s1", { is_busy: true });
    expect(await C.cycleMode()).toBe(false);
    expect(lastNotice()).toEqual({ kind: "warn", text: "Agent is busy, please wait before switching input mode..." });
    expect(mains()).toEqual([]);
  });

  it("keeps the old mode when the switch fails", async () => {
    show("s1");
    h.routes.set("POST agent/main", () => Promise.reject({ code: "conflict", message: "agent is busy", status: 409 }));
    expect(await C.cycleMode()).toBe(false);
    expect(C.composer.input).toBe("code");
    expect(lastNotice()).toEqual({ kind: "error", text: "agent is busy" });
  });

  it("Ctrl+Y toggles YOLO with its banners, and in plan mode switches to YOLO coding (TYPE-024)", async () => {
    show("s1");
    await C.toggleYolo();
    expect(lastNotice()).toEqual({ kind: "yolo", text: "Skip permission prompts. System level commands will be blocked." });
    await C.toggleYolo();
    expect(lastNotice()).toEqual({ kind: "info", text: "Yolo mode disabled" });
    expect(skips()).toEqual([true, false]);
    await C.cycleMode();
    expect(C.composer.input).toBe("plan");
    await C.toggleYolo();
    expect(C.composer.input).toBe("code");
    expect(C.composer.yolo).toBe(true);
  });

  it("puts YOLO back when the engine refuses the change", async () => {
    show("s1");
    h.routes.set("POST permissions/skip", () => Promise.reject({ code: "internal", message: "nope", status: 500 }));
    await C.toggleYolo();
    expect(C.composer.yolo).toBe(false);
  });

  it("reads YOLO from the engine, and a change made here meanwhile wins", async () => {
    show("s1");
    const slow = later();
    h.routes.set("GET permissions/skip", () => slow.promise);
    const reading = C.readYolo();
    await C.toggleYolo();
    slow.resolve({ skip: false });
    await reading;
    expect(C.composer.yolo).toBe(true);
  });
});

describe("history and the queue (TYPE-017 to TYPE-019, TYPE-050)", () => {
  it("steps through the prompts sent anywhere and back to the draft", async () => {
    h.routes.set("GET /v1/prompt-history", ["second", "first", "!ls"]);
    show("s1");
    await settle();
    C.setText("draft");
    expect(await C.historyUp()).toBe(true);
    expect(C.draftOf().text).toBe("second");
    await C.historyUp();
    expect(C.draftOf().text).toBe("first");
    await C.historyUp();
    expect(C.draftOf().text).toBe("ls");
    expect(C.currentMode()).toBe("bang");
    C.historyDown();
    expect(C.draftOf().text).toBe("first");
    expect(C.currentMode()).toBe("normal");
    expect(C.historyEscape()).toBe(true);
    expect(C.draftOf().text).toBe("draft");
  });

  it("recalls a queued prompt with its attachments while the chat works", async () => {
    show("s1", { is_busy: true });
    h.routes.set("POST agent/sessions/s1/prompts/recall", ({ query }) =>
      query.acknowledge ? null : { prompt: "queued one", submission_id: "q1", attachments: [{ file_path: "a.txt", file_name: "a.txt", mime_type: "text/plain", content: "aGk=" }] },
    );
    C.setText("");
    await C.historyUp();
    expect(C.draftOf().text).toBe("queued one");
    expect(C.draftOf().attachments).toEqual([{ kind: "text", name: "a.txt", mime: "text/plain", path: "a.txt", base64: "aGk=" }]);
    h.routes.set("POST agent/sessions/s1/prompts/recall", ({ query }) => (query.acknowledge ? null : { prompt: "queued two", submission_id: "q2", attachments: [] }));
    await C.historyUp();
    expect(C.draftOf().text).toBe("queued one\n\nqueued two");
  });

  it("in bang mode ↑ asks the history, not the queue", async () => {
    show("s1", { is_busy: true });
    h.routes.set("POST agent/sessions/s1/prompts/recall", ({ query }) => (query.acknowledge ? null : { prompt: "queued", submission_id: "q1", attachments: [] }));
    C.typed({ before: "", value: "!", caret: 1, data: "!" });
    // history.go:70-75.
    await C.historyUp();
    expect(h.calls.some((c) => c.path.endsWith("/prompts/recall"))).toBe(false);
  });

  it("a prompt recalled into bang mode waits and says so", async () => {
    show("s1", { is_busy: true });
    const slow = later();
    h.routes.set("POST agent/sessions/s1/prompts/recall", ({ query }) => (query.acknowledge ? null : slow.promise));
    const recalling = C.historyUp();
    await settle();
    C.typed({ before: "", value: "!", caret: 1, data: "!" });
    slow.resolve({ prompt: "queued", submission_id: "q1", attachments: [] });
    await recalling;
    expect(lastNotice()).toEqual({ kind: "info", text: "Queued message recovered. Press Up outside shell mode to edit it." });
    expect(C.draftOf().text).toBe("");
    expect(M.messages.returned.s1).toEqual([{ text: "queued", attachments: [] }]);
    expect(C.backspaceLeavesBang()).toBe(true);
    expect(await C.historyUp()).toBe(true);
    expect(C.draftOf().text).toBe("queued");
  });

  it("shows a failed recall and tries the queue again next time", async () => {
    show("s1");
    Q.queue.bySession.s1 = { count: 1, items: [], loaded: true };
    h.routes.set("POST agent/sessions/s1/prompts/recall", () => Promise.reject({ code: "internal", message: "recall failed", status: 500 }));
    await C.historyUp();
    expect(lastNotice()).toEqual({ kind: "error", text: "recall failed" });
    Q.queue.bySession.s1 = { count: 0, items: [], loaded: true };
    h.routes.set("POST agent/sessions/s1/prompts/recall", ({ query }) => (query.acknowledge ? null : { prompt: "got it", submission_id: "q9", attachments: [] }));
    await C.historyUp();
    expect(C.draftOf().text).toBe("got it");
  });

  it("clears the queue at once (TYPE-050)", async () => {
    show("s1", { is_busy: true });
    Q.queue.bySession.s1 = { count: 2, items: [{ text: "a" }, { text: "b" }], loaded: true };
    const clearing = Q.clear("s1");
    expect(Q.queue.bySession.s1.count).toBe(0);
    await clearing;
    expect(h.calls.some((c) => c.path === "agent/sessions/s1/prompts/clear")).toBe(true);
  });
});

describe("@ picks (TYPE-026, TYPE-029)", () => {
  async function pick(text) {
    C.setText("");
    C.typed({ before: "", value: "@", caret: 1, data: "@" });
    await settle();
    C.typed({ before: "@", value: `@${text}`, caret: text.length + 1, data: text.at(-1) });
    expect(C.composer.popup.rows[0].row.text).toBe("README.md");
    await C.pickRow(0);
    await settle();
  }

  it("ranks README.md first for @rea and puts its path and a space in place", async () => {
    show("s1");
    h.files.set("docs/readme-notes.txt", { modified: 1 });
    h.files.set("README.md", { modified: 1 });
    h.files.set("src/area.js", { modified: 1 });
    h.routes.set("GET filetracker/lastread", "0001-01-01T00:00:00Z");
    await pick("rea");
    expect(C.draftOf().text).toBe("README.md ");
    expect(C.composer.popup).toBe(null);
    expect(C.draftOf().attachments.map((a) => a.name)).toEqual(["README.md"]);
  });

  it("skips a file the chat read and that has not changed since, asking with the chat's id", async () => {
    show("s1");
    h.files.set("README.md", { modified: Date.parse("2026-10-01T00:00:00Z") });
    h.routes.set("GET filetracker/lastread", ({ query }) => (query.session_id === "s1" && query.path === "/proj/README.md" ? "2026-10-02T00:00:00Z" : "0001-01-01T00:00:00Z"));
    await pick("readme");
    expect(C.draftOf().attachments).toEqual([]);
  });

  it("attaches a file changed since the chat read it", async () => {
    show("s1");
    h.files.set("README.md", { modified: Date.parse("2026-10-03T00:00:00Z") });
    h.routes.set("GET filetracker/lastread", "2026-10-02T00:00:00Z");
    await pick("readme");
    expect(C.draftOf().attachments.map((a) => a.name)).toEqual(["README.md"]);
  });

  it("closes the popup on a space", async () => {
    show("s1");
    h.files.set("README.md", { modified: 1 });
    C.typed({ before: "", value: "@", caret: 1, data: "@" });
    await settle();
    expect(C.composer.popup).not.toBe(null);
    C.typed({ before: "@", value: "@ ", caret: 2, data: " " });
    expect(C.composer.popup).toBe(null);
  });
});

describe("the plan handoff (TYPE-052 to TYPE-054)", () => {
  const ready = "The plan.\n<!-- CRUSH_PLAN_READY -->";

  it("opens on a ready plan, comes back on an empty Enter, and starts coding with the hidden prompt", async () => {
    show("s1");
    await C.cycleMode();
    expect(C.composer.input).toBe("plan");
    await deliver(ev("run_complete", "created", { session_id: "s1", run_id: "r1", message_id: "m1", text: ready }));
    expect(C.composer.planHandoff).toEqual({ sessionId: "s1", open: true });
    C.dismissPlan();
    expect(C.composer.planHandoff).toEqual({ sessionId: "s1", open: false });
    await C.send();
    expect(C.composer.planHandoff).toEqual({ sessionId: "s1", open: true });
    h.calls.length = 0;
    await C.confirmPlan(false);
    await settle();
    expect(C.composer.input).toBe("code");
    expect(C.composer.planHandoff).toBe(null);
    expect(sends()).toHaveLength(1);
    expect(sends()[0].body).toMatchObject({ session_id: "s1", prompt: "Implement the plan.", hidden_user_message: true });
    expect(sends()[0].body.submission_id).toBeUndefined();
    expect(Object.keys(M.messages.pending)).toHaveLength(0);
  });

  it("Code with YOLO turns permission skipping on (TYPE-055)", async () => {
    show("s1");
    await C.cycleMode();
    await deliver(ev("run_complete", "created", { session_id: "s1", run_id: "r1", message_id: "m1", text: ready }));
    await C.confirmPlan(true);
    expect(C.composer.yolo).toBe(true);
    expect(C.composer.input).toBe("code");
  });

  it("ignores a plan run without the marker, and a new prompt clears a ready plan", async () => {
    show("s1");
    await C.cycleMode();
    await deliver(ev("run_complete", "created", { session_id: "s1", run_id: "r1", message_id: "m1", text: "Still planning." }));
    expect(C.composer.planHandoff).toBe(null);
    await deliver(ev("run_complete", "created", { session_id: "s1", run_id: "r2", message_id: "m2", text: ready }));
    C.setText("change step 2");
    await C.send();
    expect(C.composer.planHandoff).toBe(null);
  });

  it("another chat drops the ready plan and leaves plan mode", async () => {
    show("s1");
    await C.cycleMode();
    await deliver(ev("run_complete", "created", { session_id: "s1", run_id: "r1", message_id: "m1", text: ready }));
    show("s2");
    await settle();
    expect(C.composer.planHandoff).toBe(null);
    expect(C.composer.input).toBe("code");
  });
});
