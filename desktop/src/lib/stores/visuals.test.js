import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// The stores without Tauri (see limits.test.js); extractFigures is the real
// one, watched so a test can count the work done.
vi.mock("@tauri-apps/api/webviewWindow", () => ({
  getCurrentWebviewWindow: () => ({ listen: async () => () => {} }),
}));
vi.mock("../engine/commands.js", () => ({
  api: async () => null,
  api_abort: async () => true,
  engine_set_current_session: async () => null,
  events_take: async () => [],
  lastClipboardWrite: () => null,
}));
vi.mock("../render/figures.js", async (original) => {
  const real = await original();
  return { ...real, extractFigures: vi.fn(real.extractFigures) };
});

const SID = "7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01";
const TABLE = "| Cookie | Lifetime | Scope |\n|---|---|---|\n| session | 1 h | site |\n";
const FLOW = "```mermaid\nflowchart LR\n  a[Sign in] --> b[Session]\n```\n";

let store;
let msgs;
let figures;

beforeEach(async () => {
  vi.resetModules();
  msgs = await import("./messages.svelte.js");
  figures = await import("../render/figures.js");
  store = await import("./visuals.svelte.js");
  figures.extractFigures.mockClear();
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

const settle = async (rounds = 6) => {
  for (let i = 0; i < rounds; i++) await new Promise((resolve) => setTimeout(resolve, 0));
};
const fig = (mid, index, title = `t${index}`) => ({ id: `${mid}:${index}`, sessionId: SID, messageId: mid, index, kind: "table", title, source: `| ${title} |` });
const ids = (list) => list.map((f) => f.id);
const user = (id, text = "") => ({ id, role: "user", session_id: SID, parts: [{ type: "text", data: { text } }] });
const reply = (id, text, { finished = true, extra = [] } = {}) => ({
  id,
  role: "assistant",
  session_id: SID,
  parts: [{ type: "text", data: { text } }, ...extra, ...(finished ? [{ type: "finish", data: { reason: "end_turn", time: 1 } }] : [])],
});
function chat(...list) {
  const c = new msgs.ChatMessages();
  c.status = "ready";
  for (const m of list) c.byId.set(m.id, m);
  c.ids = list.map((m) => m.id);
  msgs.messages.bySession[SID] = c;
  return c;
}

describe("setFigures", () => {
  it("replaces a message's figures as a whole and leaves the others alone", () => {
    store.setFigures(SID, "m1", [fig("m1", 0, "a"), fig("m1", 1, "b"), fig("m1", 2, "c")]);
    store.setFigures(SID, "m2", [fig("m2", 0, "x")]);
    const other = store.visuals.bySession[SID].byId["m2:0"];
    store.setFigures(SID, "m1", [fig("m1", 0, "A")]);
    expect(ids(store.figuresFor(SID))).toEqual(["m1:0", "m2:0"]);
    expect(store.figuresFor(SID)[0].title).toBe("A");
    expect(Object.keys(store.visuals.bySession[SID].byId).sort()).toEqual(["m1:0", "m2:0"]);
    expect(store.visuals.bySession[SID].byId["m2:0"]).toBe(other);

    // The same figures again change nothing.
    const entry = store.visuals.bySession[SID];
    store.setFigures(SID, "m1", [fig("m1", 0, "A")]);
    expect(store.visuals.bySession[SID]).toBe(entry);

    store.setFigures(SID, "m1", []);
    expect(ids(store.figuresFor(SID))).toEqual(["m2:0"]);
    store.setFigures(SID, "m2", []);
    expect(store.visuals.bySession[SID]).toBeUndefined();
    expect(store.figuresFor(SID)).toEqual([]);
  });

  it("keeps chat order: message order, then index", () => {
    chat(user("u1"), reply("m1", ""), user("u2"), reply("m2", ""), reply("m3", ""));
    store.setFigures(SID, "m3", [fig("m3", 0)]);
    store.setFigures(SID, "m1", [fig("m1", 1), fig("m1", 0)]);
    store.setFigures(SID, "x9", [fig("x9", 0)]); // not in the chat (yet): last
    store.setFigures(SID, "m2", [fig("m2", 1), fig("m2", 0)]);
    expect(ids(store.figuresFor(SID))).toEqual(["m1:0", "m1:1", "m2:0", "m2:1", "m3:0", "x9:0"]);
    expect(store.visuals.bySession[SID].order).toEqual(["m1:0", "m1:1", "m2:0", "m2:1", "m3:0", "x9:0"]);
  });

  it("ignores calls without a chat or a message", () => {
    store.setFigures("", "m1", [fig("m1", 0)]);
    store.setFigures(SID, "", [fig("m1", 0)]);
    expect(store.visuals.bySession).toEqual({});
  });
});

describe("revealFigure", () => {
  it("marks the figure for 1.2 s and asks VISUALS to scroll each time", () => {
    vi.useFakeTimers();
    store.revealFigure("m1:0");
    expect(store.visuals.marked).toBe("m1:0");
    expect({ ...store.reveal }).toEqual({ id: "m1:0", seq: 1 });
    vi.advanceTimersByTime(1100);
    store.revealFigure("m2:0"); // a newer reveal moves the mark and restarts the time
    expect(store.visuals.marked).toBe("m2:0");
    vi.advanceTimersByTime(1199);
    expect(store.visuals.marked).toBe("m2:0");
    vi.advanceTimersByTime(1);
    expect(store.visuals.marked).toBe("");
    store.revealFigure("m2:0");
    store.revealFigure("m2:0");
    expect(store.reveal.seq).toBe(4);
    store.revealFigure("");
    expect(store.reveal.seq).toBe(4);
  });
});

describe("extraction", () => {
  it("extracts the shown chat's assistant messages in idle time, in chat order", async () => {
    const one = `Intro\n\n${TABLE}\nMore\n\n${FLOW}`;
    chat(user("u1", TABLE), reply("m1", one), user("u2", "next"), reply("m2", FLOW));
    store.syncFigures(SID);
    expect(store.figuresFor(SID)).toEqual([]); // the work waits for idle time
    await settle();
    const real = await vi.importActual("../render/figures.js");
    const want = [...real.extractFigures(SID, "m1", one), ...real.extractFigures(SID, "m2", FLOW)];
    expect(want.map((f) => f.kind)).toEqual(["table", "diagram", "diagram"]);
    expect(store.figuresFor(SID)).toEqual(want);
    expect(ids(store.figuresFor(SID))).toEqual(["m1:0", "m1:1", "m2:0"]);
    // User messages are not read.
    expect(figures.extractFigures.mock.calls.map((c) => c[1])).toEqual(["m1", "m2"]);
  });

  it("counts a streaming message's closed blocks only, and all of them once interrupted", async () => {
    const open = `${TABLE}\nNext\n\n| Name | Value |\n|---|---|\n| a`;
    chat(reply("m1", open, { finished: false }));
    store.syncFigures(SID);
    await settle();
    expect(ids(store.figuresFor(SID))).toEqual(["m1:0"]);
    msgs.interrupted.add("m1"); // withInterrupt gives the shown copy a finish part
    store.syncFigures(SID);
    await settle();
    expect(ids(store.figuresFor(SID))).toEqual(["m1:0", "m1:1"]);
  });

  it("replaces a rewritten message's figures and keeps the other messages' objects", async () => {
    const c = chat(reply("m1", `${TABLE}\n${FLOW}`), reply("m2", TABLE));
    store.syncFigures(SID);
    await settle();
    const kept = store.visuals.bySession[SID].byId["m2:0"];
    c.byId.set("m1", reply("m1", "No visuals now."));
    store.syncFigures(SID);
    await settle();
    expect(ids(store.figuresFor(SID))).toEqual(["m2:0"]);
    expect(store.visuals.bySession[SID].byId["m2:0"]).toBe(kept);
  });

  it("does the work once per text: coalesced updates, unchanged text skipped", async () => {
    const c = chat(reply("m1", TABLE, { finished: false }));
    store.syncFigures(SID);
    c.byId.set("m1", reply("m1", `${TABLE}\nmore`, { finished: false }));
    store.syncFigures(SID);
    c.byId.set("m1", reply("m1", `${TABLE}\nmore words`, { finished: false }));
    store.syncFigures(SID);
    await settle();
    expect(figures.extractFigures).toHaveBeenCalledTimes(1);
    // A new object with the same text (a tool call joined it) is not read again.
    c.byId.set("m1", reply("m1", `${TABLE}\nmore words`, { finished: false, extra: [{ type: "tool_call", data: { id: "t1" } }] }));
    store.syncFigures(SID);
    await settle();
    expect(figures.extractFigures).toHaveBeenCalledTimes(1);
  });

  it("drops the figures of a message that leaves the chat at once, and of a chat that is unloaded", async () => {
    const c = chat(reply("m1", TABLE), reply("m2", TABLE));
    store.syncFigures(SID);
    await settle();
    expect(ids(store.figuresFor(SID))).toEqual(["m1:0", "m2:0"]);
    c.byId.delete("m1");
    c.ids = ["m2"];
    store.syncFigures(SID);
    expect(ids(store.figuresFor(SID))).toEqual(["m2:0"]);
    delete msgs.messages.bySession[SID];
    store.syncFigures("");
    expect(store.visuals.bySession[SID]).toBeUndefined();
  });

  it("follows the chat's order when a message moves", async () => {
    const c = chat(reply("m1", TABLE), reply("m2", TABLE));
    store.syncFigures(SID);
    await settle();
    c.ids = ["m2", "m1"];
    store.syncFigures(SID);
    expect(ids(store.figuresFor(SID))).toEqual(["m2:0", "m1:0"]);
  });

  it("works in slices of at most 4 ms", async () => {
    chat(reply("m1", TABLE), reply("m2", TABLE), reply("m3", TABLE));
    vi.useFakeTimers();
    let now = 0;
    vi.spyOn(performance, "now").mockImplementation(() => (now += 5)); // every extraction "takes" 5 ms
    store.syncFigures(SID);
    vi.runOnlyPendingTimers();
    expect(ids(store.figuresFor(SID))).toEqual(["m1:0"]);
    vi.runOnlyPendingTimers();
    expect(ids(store.figuresFor(SID))).toEqual(["m1:0", "m2:0"]);
    vi.runOnlyPendingTimers();
    expect(ids(store.figuresFor(SID))).toEqual(["m1:0", "m2:0", "m3:0"]);
    expect(vi.getTimerCount()).toBe(0);
  });
});
