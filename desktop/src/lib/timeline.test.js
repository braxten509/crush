// @vitest-environment jsdom
// lib/timeline.js: stored messages → timeline items (CHAT-002 to CHAT-037,
// CHAT-080's grouping, 4.4's keys). The cases follow the terminal's tests
// named beside each block.
import { afterEach, describe, expect, test } from "vitest";
import { interrupted } from "./stores/messages.svelte.js";
import {
  buildTimeline,
  createTimelineState,
  footerText,
  hypercredits,
  keyLetters,
  noticeParts,
  parseSkill,
  ranTools,
  REFUSAL_DETAILS,
  REFUSAL_TITLE,
  systemNotice,
  taskNotice,
  toolActivity,
} from "./timeline.js";

// -- Message builders (wire.js shapes) --
let at = 1_792_320_000;
const nextTime = () => (at += 10);
const text = (t, extra = {}) => ({ type: "text", data: { text: t, ...extra } });
const reasoning = (t, extra = {}) => ({ type: "reasoning", data: { thinking: t, signature: "", ...extra } });
const finish = (reason = "end_turn", extra = {}) => ({ type: "finish", data: { reason, time: at + 5, ...extra } });
const call = (id, name, input = "{}", finished = true) => ({ type: "tool_call", data: { id, name, input, finished } });
const result = (id, name, content = "ok", extra = {}) => ({ type: "tool_result", data: { tool_call_id: id, name, content, metadata: "{}", is_error: false, ...extra } });
const user = (id, t, extra = {}) => ({ id, role: "user", session_id: "s1", parts: [text(t)], model: "", provider: "", created_at: nextTime(), updated_at: at, ...extra });
const assistant = (id, parts, extra = {}) => ({ id, role: "assistant", session_id: "s1", parts, model: "opus", provider: "claude-code", created_at: nextTime(), updated_at: at, ...extra });
const tool = (id, parts) => ({ id, role: "tool", session_id: "s1", parts, model: "", provider: "", created_at: nextTime(), updated_at: at });
const system = (id, t) => ({ id, role: "system", session_id: "s1", parts: [text(t)], model: "", provider: "", created_at: nextTime(), updated_at: at });

const PROVIDERS = [{ id: "claude-code", name: "Claude Code", models: [{ id: "opus", name: "Opus 5.5" }] }];

function build(msgs, opts = {}, state = createTimelineState()) {
  const byId = new Map(msgs.map((m) => [m.id, m]));
  return buildTimeline({ sessionId: "s1", ids: msgs.map((m) => m.id), byId, providers: PROVIDERS, now: Date.now(), ...opts }, state);
}
const kinds = (out) => out.items.map((i) => i.kind);

afterEach(() => interrupted.clear());

describe("your messages (CHAT-002 to CHAT-007)", () => {
  test("user item: the text as stored, chips for attachments, keyed by message id", () => {
    const m = user("u1", "Fix <b>x</b> *y*\tnow", {});
    m.parts.push({ type: "binary", data: { Path: "/tmp/home/proj/shot-of-the-screen.png", MIMEType: "image/png", Data: "iVBORw0KGgo=" } });
    m.parts.push({ type: "binary", data: { Path: "/tmp/home/proj/notes.txt", MIMEType: "text/plain", Data: "aGk=" } });
    const [item] = build([m]).items;
    expect(item).toMatchObject({ kind: "user", key: "u1", messageId: "u1", sessionId: "s1", text: "Fix <b>x</b> *y*\tnow", letter: "" });
    expect(item.chips).toEqual([
      { kind: "picture", name: "shot-of-the-screen.png", mime: "image/png", viewable: true, src: "data:image/png;base64,iVBORw0KGgo=" },
      { kind: "text", name: "notes.txt", mime: "text/plain", viewable: false },
    ]);
  });

  test("hidden: a user message whose text part is hidden makes no item (CHAT-005)", () => {
    const hidden = user("u1", "Implement the plan.", { parts: [text("Implement the plan.", { hidden: true })] });
    expect(build([hidden]).items).toEqual([]);
  });

  test("a skill block is one notice; broken XML stays as typed (CHAT-008)", () => {
    const valid = "<loaded_skill>\n  <name>teach</name>\n  <description>Explains things from the start</description>\n  <location>/x</location>\n  <instructions>\n do &amp; tell\n  </instructions>\n</loaded_skill>";
    const [notice] = build([user("u1", valid)]).items;
    expect(notice).toMatchObject({ kind: "notice", key: "notice:u1", notice: "skill", name: "teach", text: "Explains things from the start" });
    const broken = "<loaded_skill>\n  <name>teach</name>\n  <description>unclosed";
    const [plain] = build([user("u2", broken)]).items;
    expect(plain).toMatchObject({ kind: "user", text: broken });
    expect(parseSkill("<loaded_skill><name>a & b</name></loaded_skill>")).toBe(null);
  });

  // What Go's xml.Unmarshal makes of these (encoding/xml, strict).
  test.each([
    ["CDATA and character references", "<loaded_skill><name><![CDATA[a<b]]></name><description>&#65;&#x42;&quot;&apos;&lt;&gt;</description></loaded_skill>", { name: "a<b", description: "AB\"'<>" }],
    ["text of deeper elements is skipped", "<loaded_skill><name>te<b>XX</b>ach</name></loaded_skill>", { name: "teach", description: "" }],
    ["comments, attributes and other children", "<loaded_skill><!-- c --><name id='1' x=\"y\">teach</name><other>o</other></loaded_skill>", { name: "teach", description: "" }],
    ["the last child of a name wins", "<loaded_skill><name>a</name><name>b</name></loaded_skill>", { name: "b", description: "" }],
    ["what follows the element is not read", "<loaded_skill><name>a</name></loaded_skill> trailing & junk <", { name: "a", description: "" }],
    ["\\r\\n becomes \\n", "<loaded_skill><description>one\r\ntwo\rthree</description></loaded_skill>", { name: "", description: "one\ntwo\nthree" }],
    ["an empty child", "<loaded_skill><name/><description></description></loaded_skill>", { name: "", description: "" }],
  ])("skill XML: %s", (_case, xml, want) => {
    expect(parseSkill(xml)).toEqual(want);
  });

  test.each([
    ["an unknown entity", "<loaded_skill><name>&nbsp;</name></loaded_skill>"],
    ["a character XML does not allow", "<loaded_skill><name>a\u0001b</name></loaded_skill>"],
    ["a character reference XML does not allow", "<loaded_skill><name>&#0;</name></loaded_skill>"],
    ["a mismatched end tag", "<loaded_skill><name>a</nam></loaded_skill>"],
    ["an unclosed comment", "<loaded_skill><!-- c <name>a</name></loaded_skill>"],
    ["an attribute without quotes", "<loaded_skill><name id=1>a</name></loaded_skill>"],
    ["]]> outside CDATA", "<loaded_skill><name>a]]>b</name></loaded_skill>"],
    ["no end", "<loaded_skill><name>a</name>"],
  ])("skill XML that Go refuses: %s", (_case, xml) => {
    expect(parseSkill(xml)).toBe(null);
  });

  test.each([
    ["done", { notice: "task", state: "done", text: "Task Fixer finished" }],
    ["failed", { notice: "task", state: "failed", text: "Task Fixer failed" }],
    ["stopped", { notice: "task", state: "stopped", text: "Task Fixer stopped" }],
    ["running", { notice: "task", state: "running", text: "Still running: Fixer" }],
  ])("task result %s (CHAT-009)", (status, want) => {
    const body = `<crush-task-result>\n<task-id>t1</task-id>\n<name>Fixer</name>\n<status>${status}</status>\n<result>\nall good\n</result>\n</crush-task-result>`;
    const [item] = build([user("u1", body)]).items;
    expect(item).toMatchObject({ kind: "notice", name: "Fixer", ...want });
  });

  test("question answers (CHAT-010)", () => {
    expect(taskNotice("Questions", "answered")).toEqual({ notice: "answers", state: "answered", name: "Questions", text: "Your answers to the questions: answered" });
    expect(taskNotice("Questions", "cancelled")).toMatchObject({ notice: "answers", state: "cancelled" });
    expect(taskNotice("Questions", "done")).toMatchObject({ notice: "task", text: "Task Questions finished" });
  });

  test.each([
    [{ notice: "skill", state: "", name: "teach", text: "Explains things" }, { icon: "skill", tone: "ink", before: "Loaded Skill → ", strong: "teach", after: " · Explains things" }],
    [{ notice: "skill", state: "", name: "teach", text: "" }, { icon: "skill", before: "Loaded Skill → ", strong: "teach", after: "" }],
    [{ notice: "task", state: "done", name: "Fixer", text: "Task Fixer finished" }, { icon: "check", tone: "ink", before: "Task ", strong: "Fixer", after: " finished" }],
    [{ notice: "task", state: "failed", name: "Fixer", text: "Task Fixer failed" }, { icon: "cross", tone: "error", before: "Task ", strong: "Fixer", after: " failed" }],
    [{ notice: "task", state: "stopped", name: "Fixer", text: "Task Fixer stopped" }, { icon: "dot", tone: "muted", after: " stopped" }],
    [{ notice: "task", state: "running", name: "Fixer", text: "Still running: Fixer" }, { icon: "dot", tone: "ink", before: "Still running: ", strong: "Fixer", after: "" }],
    [{ notice: "answers", state: "answered", name: "Questions", text: "" }, { icon: "check", before: "Your answers to the questions: ", strong: "answered" }],
    [{ notice: "answers", state: "cancelled", name: "Questions", text: "" }, { icon: "dot", tone: "muted", strong: "cancelled" }],
    [{ notice: "system", state: "on", name: "teach", text: "teach turned on by the agent" }, { icon: "half-circle", tone: "ink", before: "", strong: "teach", after: " turned on by the agent" }],
    [{ notice: "system", state: "off", name: "teach", text: "teach turned off by you" }, { icon: "circle", tone: "muted", strong: "teach", after: " turned off by you" }],
    [{ notice: "system", state: "", name: "", text: "Memory was refreshed" }, { icon: "half-circle", before: "Memory was refreshed", strong: "", after: "" }],
  ])("the line of %j (CHAT-008 to CHAT-011)", (item, want) => {
    expect(noticeParts(item)).toMatchObject(want);
  });

  test("your message carries its time for the byline", () => {
    const m = user("u1", "hi");
    expect(build([m]).items[0].time).toBe(m.created_at);
  });

  test("system notices (CHAT-011; system_notice_test.go:14-29)", () => {
    const [on, off, plain] = build([
      system("n1", "teach turned on by the agent"),
      system("n2", "teach turned off by the agent"),
      system("n3", "Memory was refreshed"),
    ]).items;
    expect(on).toMatchObject({ kind: "notice", notice: "system", state: "on", name: "teach", text: "teach turned on by the agent" });
    expect(off).toMatchObject({ state: "off", name: "teach" });
    expect(plain).toMatchObject({ state: "", name: "", text: "Memory was refreshed" });
    expect(systemNotice("x turned")).toEqual({ notice: "system", state: "", name: "", text: "x turned" });
  });

  test("shell commands show as shell items only, also after reopening (CHAT-013)", () => {
    const m = user("u1", "", { parts: [{ type: "shell_command", data: { command: "printf 'a\\nb'", output: "a\nb", exit_code: 0 } }] });
    const out = build([m]);
    expect(kinds(out)).toEqual(["shell"]);
    expect(out.items[0]).toMatchObject({ key: "shell:u1:0", command: "printf 'a\\nb'", output: "a\nb", exitCode: 0, running: false, requestId: "" });
  });

  test("a live command keeps its key when its saved copy arrives", () => {
    const state = createTimelineState();
    const live = { requestId: "r1", sessionId: "s1", command: "echo hi", output: "hi\n", lastSeq: 1, running: true, exitCode: null, startedAt: 1 };
    const first = build([], { shells: [live] }, state);
    expect(first.items[0]).toMatchObject({ kind: "shell", key: "shell:r1", running: true, requestId: "r1" });
    const done = { ...live, running: false, exitCode: 0 };
    const saved = user("u1", "", { parts: [{ type: "shell_command", data: { command: "echo hi", output: "hi\n", exit_code: 0 } }] });
    const second = build([saved], { shells: [done] }, state);
    expect(second.items.map((i) => i.key)).toEqual(["shell:r1"]);
    // The live state goes away later; the saved item keeps the key.
    expect(build([saved], { shells: [] }, state).items.map((i) => i.key)).toEqual(["shell:r1"]);
  });

  test("a pending prompt shows until its saved copy, which takes its key", () => {
    const state = createTimelineState();
    const p = { submissionId: "sub1", sessionId: "s1", text: "hello", attachments: [], held: false, queued: false, state: "working", acknowledged: true, createdAt: 5 };
    const first = build([], { pending: [p] }, state);
    expect(first.items[0]).toMatchObject({ kind: "pending", key: "pending:sub1", submissionId: "sub1", text: "hello", state: "working" });
    const saved = user("u1", "hello", { parts: [text("hello", { submission_id: "sub1" })] });
    const second = build([saved], { pending: [] }, state);
    expect(second.items[0]).toMatchObject({ kind: "user", key: "pending:sub1", messageId: "u1" });
  });
});

describe("replies (CHAT-014, CHAT-015, CHAT-023)", () => {
  test("a reply keeps its stored Markdown, author and time", () => {
    const a = assistant("a1", [text("**Bold** and `code`."), finish()]);
    const out = build([user("u1", "q"), a]);
    expect(kinds(out)).toEqual(["user", "reply", "footer"]);
    expect(out.items[1]).toMatchObject({ key: "a1", text: "**Bold** and `code`.", finished: true, plan: "none", author: "Claude", time: a.created_at });
  });

  test("reasoning never reaches an item; a finished thinking-only step leaves nothing (assistant_reasoning_visibility_test.go)", () => {
    for (const provider of ["grok-cli", "codex-cli", "claude-code", "opencode-cli", "agy-cli", "abacus", "future-provider"]) {
      const live = assistant("a1", [reasoning("PRIVATE_REASONING_FIXTURE")], { provider });
      const out = build([user("u1", "q"), live], { busy: true });
      expect(out.status).toBe("Thinking");
      const statusItem = out.items.find((i) => i.kind === "status");
      expect(statusItem.thought).toBe("PRIVATE_REASONING_FIXTURE");
      // Only the busy line's newest thought carries it (CHAT-022); no other field does.
      const others = out.items.filter((i) => i !== statusItem);
      expect(JSON.stringify(others)).not.toContain("PRIVATE_REASONING_FIXTURE");
      const done = assistant("a1", [reasoning("PRIVATE_REASONING_FIXTURE"), text("Visible reply."), finish()], { provider });
      const after = build([user("u1", "q"), done]);
      expect(JSON.stringify(after.items)).not.toContain("PRIVATE_REASONING_FIXTURE");
      expect(JSON.stringify(after.items)).not.toContain("Thought for");
      expect(after.items.find((i) => i.kind === "reply").text).toBe("Visible reply.");
    }
    const thoughtOnly = assistant("a2", [reasoning("hidden ".repeat(100)), finish()]);
    expect(build([user("u1", "q"), thoughtOnly]).items.map((i) => i.kind)).toEqual(["user"]);
  });

  test("no item kind carries reasoning text, whatever the state (CHAT-024)", () => {
    const secret = "SECRET_THOUGHT_7";
    const cases = [
      [assistant("a1", [reasoning(secret)])],
      [assistant("a1", [reasoning(secret), text("Partial")])],
      [assistant("a1", [reasoning(secret), call("c1", "bash", '{"command":"ls"}')]), tool("t1", [result("c1", "bash")])],
      [assistant("a1", [reasoning(secret), finish("canceled")])],
      [assistant("a1", [reasoning(secret), finish("error", { message: "boom" })])],
      [assistant("a1", [reasoning(secret), text("Done"), finish()])],
    ];
    for (const msgs of cases) {
      for (const busy of [false, true]) {
        const out = build([user("u1", "q"), ...msgs], { busy, thoughtLine: false });
        expect(JSON.stringify(out.items)).not.toContain(secret);
      }
    }
  });

  test("a step that later gets reply text shows again", () => {
    const state = createTimelineState();
    const thinking = assistant("a1", [reasoning("plan")]);
    expect(kinds(build([user("u1", "q"), thinking], { busy: true }, state))).toEqual(["user", "status"]);
    const replying = { ...thinking, parts: [reasoning("plan"), text("Now the answer")] };
    expect(kinds(build([user("u1", "q"), replying], { busy: true }, state))).toEqual(["user", "reply", "status"]);
  });
});

describe("live status (CHAT-019 to CHAT-022)", () => {
  const now = 1_000_000_000;
  const words = (m, opts = {}) => build([user("u1", "q"), m], { busy: true, now, ...opts }).status;

  test("each word", () => {
    expect(words(assistant("a1", []))).toBe("Waiting for the model");
    expect(words(assistant("a1", [], { activity: "thinking" }))).toBe("Thinking");
    expect(words(assistant("a1", [reasoning("x", { started_at: 1 })]))).toBe("Thinking");
    expect(words(assistant("a1", [text("Hi")], { activity: "responding", activity_at: now / 1000 - 4 }))).toBe("Responding");
    expect(words(assistant("a1", [text("Hi")], { activity: "responding", activity_at: now / 1000 - 5 }))).toBe("Waiting for the model");
    expect(words(assistant("a1", [], { activity: "writing" }))).toBe("Writing command");
    expect(words(assistant("a1", [], { is_compacting: true }))).toBe("Compacting conversation");
  });

  test("Responding asks for a rebuild when it would end", () => {
    const out = build([user("u1", "q"), assistant("a1", [text("Hi")], { activity: "responding", activity_at: now / 1000 - 2 })], { busy: true, now });
    expect(out.refreshAt).toBe(now + 3000);
  });

  test("gone once tools start, when not busy, and on a step something newer follows", () => {
    expect(words(assistant("a1", [call("c1", "bash")]))).not.toBe("Waiting for the model");
    expect(build([user("u1", "q"), assistant("a1", [], { activity: "thinking" })], { busy: false }).items.map((i) => i.kind)).toEqual(["user"]);
    const out = build([user("u1", "q"), assistant("a1", [text("first")]), assistant("a2", [text("second")])], { busy: true });
    expect(kinds(out)).toEqual(["user", "reply", "reply", "status"]);
    expect(out.items[3].key).toBe("status:a2");
  });

  test.each([
    ["rate_limited", "Rate limited — waiting to retry"],
    ["retrying", "Waiting to retry"],
  ])("retry words for %s, in the reply and in a group (retry_status_test.go)", (activity, label) => {
    expect(words(assistant("a1", [], { activity }))).toBe(label);
    const grouped = [user("u1", "q"), assistant("a0", [call("c1", "bash")]), tool("t0", [result("c1", "bash")]), assistant("a1", [], { activity })];
    expect(build(grouped, { busy: true }).status).toBe(label);
    // The activity changes: the words go.
    expect(words(assistant("a1", [], { activity: "thinking" }))).toBe("Thinking");
    const groupedThinking = [...grouped.slice(0, 3), assistant("a1", [], { activity: "thinking" })];
    expect(build(groupedThinking, { busy: true }).status).toBe("Thinking");
    // A cancel: the words go.
    expect(words(assistant("a1", [finish("canceled")], { activity }))).toBe("");
    const groupedCanceled = [...grouped.slice(0, 3), assistant("a1", [finish("canceled")], { activity })];
    expect(build(groupedCanceled, { busy: true }).status).toBe("");
  });

  test("the thought peek follows Thinking only, and the setting turns it off", () => {
    const m = assistant("a1", [], { activity: "thinking", activity_at: now / 1000, activity_detail: "**Checking how Magisk asks for su approval**" });
    const out = build([user("u1", "q"), m], { busy: true, now });
    expect(out.items.at(-1)).toMatchObject({ kind: "status", words: "Thinking", thought: "Checking how Magisk asks for su approval" });
    expect(build([user("u1", "q"), m], { busy: true, now, thoughtLine: false }).items.at(-1).thought).toBe("");
    const writing = { ...m, activity: "writing" };
    expect(build([user("u1", "q"), writing], { busy: true, now }).items.at(-1)).toMatchObject({ words: "Writing command", thought: "" });
  });

  test("the timer starts when the window first sees the activity and restarts when it changes", () => {
    const state = createTimelineState();
    const m = assistant("a1", [], { activity: "thinking" });
    const one = build([user("u1", "q"), m], { busy: true, now: 1000 }, state);
    expect(one.items.at(-1).startedAt).toBe(1000);
    const heartbeat = { ...m, updated_at: m.updated_at + 1 };
    expect(build([user("u1", "q"), heartbeat], { busy: true, now: 9000 }, state).items.at(-1).startedAt).toBe(1000);
    const writing = { ...m, activity: "writing" };
    expect(build([user("u1", "q"), writing], { busy: true, now: 12000 }, state).items.at(-1).startedAt).toBe(12000);
  });

  test("while busy, a prompt with nothing after it gets the waiting status", () => {
    const out = build([user("u1", "q", { parts: [text("q", { submission_id: "sub9" })] })], { busy: true });
    expect(out.items.at(-1)).toMatchObject({ kind: "status", key: "status:waiting:sub9", words: "Waiting for the model" });
    expect(build([user("u1", "q")], { busy: false }).items.map((i) => i.kind)).toEqual(["user"]);
  });
});

describe("banners (CHAT-025 to CHAT-029)", () => {
  test("Canceled after a stopped reply, not after a step that called tools", () => {
    const out = build([user("u1", "q"), assistant("a1", [text("Half"), finish("canceled")])]);
    expect(kinds(out)).toEqual(["user", "reply", "banner"]);
    expect(out.items[2]).toMatchObject({ key: "banner:a1", banner: "canceled", title: "Canceled" });
    const tools = build([user("u1", "q"), assistant("a1", [call("c1", "bash"), finish("canceled")])]);
    expect(tools.items.some((i) => i.kind === "banner")).toBe(false);
  });

  test("this window's own stop shows Canceled before the engine answers, and later chunks reopen nothing (CHAT-026)", () => {
    const streaming = assistant("a1", [text("Half a")]);
    interrupted.add("a1");
    const out = build([user("u1", "q"), streaming], { busy: true });
    expect(kinds(out)).toEqual(["user", "reply", "banner"]);
    // A late chunk of the stopped run: still canceled, still no status.
    const late = { ...streaming, parts: [text("Half a sentence more")] };
    expect(kinds(build([user("u1", "q"), late], { busy: true }))).toEqual(["user", "reply", "banner"]);
  });

  test("interrupted tools show cancelled; a tools-only step first seen after the stop stays hidden", () => {
    const state = createTimelineState();
    const step = assistant("a1", [call("c1", "bash", '{"command":"sleep 9"}')]);
    build([user("u1", "q"), step], { busy: true }, state);
    interrupted.add("a1");
    const out = build([user("u1", "q"), step], { busy: false, callMarks: { c1: "cancelled" } }, state);
    expect(kinds(out)).toEqual(["user", "tools"]);
    expect(out.items[1].calls[0].status).toBe("cancelled");
    expect(out.items[1].working).toBe(false);
  });

  test("error banner keeps its title and details; refusal has its own words (CHAT-027, CHAT-028)", () => {
    const err = build([user("u1", "q"), assistant("a1", [finish("error", { message: "Provider error 500", details: "upstream said no" })])]);
    expect(err.items.at(-1)).toMatchObject({ kind: "banner", banner: "error", title: "Provider error 500", details: "upstream said no" });
    const refused = build([user("u1", "q"), assistant("a1", [finish("content_filter")])]);
    expect(refused.items.at(-1)).toMatchObject({ banner: "refused", title: REFUSAL_TITLE, details: REFUSAL_DETAILS });
    expect(REFUSAL_TITLE).toBe("Model refused to continue");
    const own = build([user("u1", "q"), assistant("a1", [finish("content_filter", { message: "Blocked", details: "Policy 7" })])]);
    expect(own.items.at(-1)).toMatchObject({ banner: "refused", title: "Blocked", details: "Policy 7" });
  });

  test("compaction: the live words, then compacted, canceled or an error (CHAT-029)", () => {
    const live = assistant("s", [], { is_summary_message: true });
    expect(build([user("u1", "q"), live], { busy: true }).items.at(-1)).toMatchObject({ kind: "status", words: "Compacting conversation" });
    const done = { ...live, parts: [text("Summary of everything"), finish()] };
    const out = build([user("u1", "q"), done]);
    expect(out.items.at(-1)).toMatchObject({ kind: "banner", banner: "compacted", title: "Conversation compacted" });
    expect(JSON.stringify(out.items)).not.toContain("Summary of everything");
    expect(build([user("u1", "q"), { ...live, parts: [finish("canceled")] }]).items.at(-1)).toMatchObject({ banner: "compaction_canceled", title: "Compaction canceled" });
    expect(build([user("u1", "q"), { ...live, parts: [finish("error", { message: "too long" })] }]).items.at(-1)).toMatchObject({ banner: "error", title: "too long" });
  });

  test("an earlier compacting step makes the live step and group say Compacting", () => {
    // A shown step (it has text) that is still compacting, with a newer step after it.
    const compactingStep = assistant("a1", [text("Tidying the history first.")], { is_compacting: true });
    const later = assistant("a2", [], { activity: "thinking" });
    const out = build([user("u1", "q"), compactingStep, later], { busy: true });
    expect(out.status).toBe("Compacting conversation");
    const groupLater = [user("u1", "q"), compactingStep, assistant("a2", [call("c1", "bash")]), tool("t1", [result("c1", "bash")])];
    expect(build(groupLater, { busy: true }).status).toBe("Compacting conversation");
  });
});

describe("plan box (CHAT-031, CHAT-032)", () => {
  const START = "<!-- CRUSH_PLAN_START -->";
  const READY = "<!-- CRUSH_PLAN_READY -->";
  const reply = (msgs, opts) => build(msgs, opts).items.find((i) => i.kind === "reply");

  test("the ready marker closes the box; a streaming plan in plan mode is open", () => {
    expect(reply([user("u1", "q"), assistant("a1", [text(`${START}\n# Plan\n${READY}`), finish()])]).plan).toBe("closed");
    expect(reply([user("u1", "q"), assistant("a1", [text(`${START}\n# Plan\nStep one`)])], { planMode: true, busy: true }).plan).toBe("open");
  });

  test("no start marker, not plan mode, or an old finished message: no open box", () => {
    expect(reply([user("u1", "q"), assistant("a1", [text("Just exploring")])], { planMode: true, busy: true }).plan).toBe("none");
    expect(reply([user("u1", "q"), assistant("a1", [text(`${START}\n# Plan`)])], { planMode: false, busy: true }).plan).toBe("none");
    expect(reply([user("u1", "q"), assistant("a1", [text(`${START}\n# Plan`), finish()])], { planMode: true }).plan).toBe("none");
  });
});

describe("footers (CHAT-035 to CHAT-037)", () => {
  test("the prompt's last turn: model, provider and the time since your message", () => {
    const u = user("u1", "q");
    const a = assistant("a1", [text("Answer"), { type: "finish", data: { reason: "end_turn", time: u.created_at + 12 } }]);
    const out = build([u, a]);
    expect(out.items.at(-1)).toMatchObject({ kind: "footer", key: "footer:a1", text: "Opus 5.5 via Claude Code in 12s", saved: "" });
  });

  test("Unknown Model and a provider not in the list", () => {
    const m = assistant("a1", [text("x"), { type: "finish", data: { reason: "end_turn", time: 100 } }], { provider: "acme", model: "zz" });
    expect(footerText(m, PROVIDERS, 38)).toEqual({ text: "Unknown Model via acme in 1m2s", saved: "" });
  });

  test("Prism: every turn, the routed model and the savings (CHAT-036)", () => {
    const mid = assistant("a1", [call("c1", "bash"), finish("tool_use")], { prism_model_name: "Haiku", prism_hypercredit_savings: 12.4 });
    expect(footerText(mid, PROVIDERS, 0)).toEqual({ text: "Opus 5.5 → Haiku", saved: "◆ 12 Saved" });
    const dollars = assistant("a2", [text("x"), { type: "finish", data: { reason: "end_turn", time: 10 } }], { prism_model_name: "Haiku", prism_dollar_savings: 0.4219 });
    expect(footerText(dollars, PROVIDERS, 0)).toEqual({ text: "Opus 5.5 → Haiku via Claude Code in 10s", saved: "• $0.42 Saved" });
    expect([hypercredits(1), hypercredits(0.94), hypercredits(0.05), hypercredits(2.5)]).toEqual(["1", "0.9", "0.1", "3"]);
  });

  test("no footer where it would label nothing (CHAT-037)", () => {
    // Opening the chat: an assistant message first.
    expect(kinds(build([assistant("a1", [finish()])]))).toEqual([]);
    // Right after your prompt: an end_turn with nothing shown.
    expect(kinds(build([user("u1", "q"), assistant("a1", [finish()])]))).toEqual(["user"]);
    // After another footer.
    const twice = [user("u1", "q"), assistant("a1", [text("x"), finish()]), assistant("a2", [finish()])];
    expect(kinds(build(twice))).toEqual(["user", "reply", "footer"]);
    // After the compaction marker.
    const compacted = [user("u1", "q"), assistant("s1m", [text("sum"), finish()], { is_summary_message: true }), assistant("a2", [finish()])];
    expect(kinds(build(compacted))).toEqual(["user", "banner"]);
  });
});

describe("groups and keys (CHAT-080, 4.4)", () => {
  const run = [
    user("u1", "q"),
    assistant("a1", [text("Looking."), call("c1", "view", '{"file_path":"a.go"}'), call("c2", "edit", '{"file_path":"a.go"}')]),
    tool("t1", [result("c1", "view"), result("c2", "edit")]),
    assistant("a2", [reasoning("think")]),
    assistant("a3", [call("c3", "bash", '{"command":"go test"}')]),
    tool("t3", [result("c3", "bash", "FAIL", { is_error: true })]),
    assistant("a4", [text("Done."), finish()]),
  ];

  test("a run of tools and thinking steps is one group, keyed by its first step", () => {
    const out = build(run);
    expect(kinds(out)).toEqual(["user", "reply", "tools", "reply", "footer"]);
    const g = out.items[2];
    expect(g).toMatchObject({ key: "group:c1", groupId: "c1", working: false, lastFailed: true, failed: 1, backgrounded: 0, live: null, letter: "B" });
    expect(g.calls.map((c) => [c.id, c.status])).toEqual([
      ["c1", "done"],
      ["c2", "done"],
      ["c3", "error"],
    ]);
    expect(g.changes).toMatchObject({ sessionId: "s1", toolCallIds: ["c2"], title: "Ran 3 tools" });
    expect(g.members).toEqual(["c1", "c2", "a2", "c3"]);
  });

  test("Question, Agent and Agentic Fetch never fold; a run of only thinking does not group", () => {
    const msgs = [user("u1", "q"), assistant("a1", [call("q1", "question"), call("c1", "bash"), call("g1", "agent")]), assistant("a2", [reasoning("x")])];
    const out = build(msgs, { busy: true });
    expect(kinds(out)).toEqual(["user", "tool", "tools", "tool", "status"]);
    expect(out.items[1]).toMatchObject({ key: "tool:q1", call: { name: "question", status: "running" } });
  });

  test("the live group says what it is doing; a finished last tool leaves Thinking while busy", () => {
    const working = [user("u1", "q"), assistant("a1", [call("c1", "bash", '{"command":"sleep 5"}')])];
    expect(build(working, { busy: true }).items.at(-1)).toMatchObject({ kind: "tools", working: true, live: { kind: "status", words: "Running a command" } });
    expect(build(working, { busy: true, callMarks: { c1: "permission" } }).status).toBe("Waiting for approval");
    const between = [...working, tool("t1", [result("c1", "bash")])];
    expect(build(between, { busy: true }).status).toBe("Thinking");
    expect(build(between, { busy: false }).items.at(-1).live).toBe(null);
  });

  test.each([
    ["bash", "Running a command"],
    ["view", "Reading a file"],
    ["multiedit", "Editing a file"],
    ["glob", "Searching"],
    ["web_search", "Searching the web"],
    ["download", "Reading a web page"],
    ["todos", "Updating the plan"],
    ["job_output", "Checking a command"],
    ["lsp_references", "Checking code"],
    ["mcp_docker_ps", "Using docker"],
    ["agent", "Waiting for subagent"],
    ["mystery", "Working"],
  ])("tool words: %s → %s", (name, words) => {
    expect(toolActivity(name)).toBe(words);
  });

  test("keys: B, C, … Z, then AA, AB; only groups in phase 3, replies with a branch link in phase 4", () => {
    expect([0, 1, 23, 24, 25, 26].map(keyLetters)).toEqual(["B", "C", "Y", "Z", "AA", "AB"]);
    const out = build(run, { hasKey: (mid) => mid === "a1" });
    expect(out.items.map((i) => i.letter)).toEqual(["", "B", "C", "", ""]);
    expect(ranTools(1)).toBe("Ran 1 tool");
  });

  test("a group in plan mode: thinking steps do not fold", () => {
    const out = build(run, { planMode: true });
    expect(kinds(out)).toEqual(["user", "reply", "tools", "tools", "reply", "footer"]);
  });
});

describe("versions (1.6 items 2-3)", () => {
  test("an unchanged item keeps its object; a changed one gets the next version", () => {
    const state = createTimelineState();
    const u = user("u1", "q");
    const a = assistant("a1", [text("first")]);
    const one = build([u, a], { busy: true }, state);
    const two = build([u, a], { busy: true }, state);
    expect(two.items[0]).toBe(one.items[0]);
    expect(two.items[1]).toBe(one.items[1]);
    const grown = { ...a, parts: [text("first and more")] };
    const three = build([u, grown], { busy: true }, state);
    expect(three.items[0]).toBe(one.items[0]);
    expect(three.items[1]).not.toBe(one.items[1]);
    expect(three.items[1].version).toBe(one.items[1].version + 1);
  });
});
