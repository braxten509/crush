// CHAT-047 to CHAT-050 and CHAT-085: a tool's status, its early-state line,
// and what a working group says (ports of tool_header.go:31-82,
// toolgroup.go:293-370,460-473, activity_test.go, retry_status_test.go,
// thought_line_test.go).
import { describe, expect, it } from "vitest";
import {
  activityKey,
  activityStart,
  cleanThoughtLine,
  earlyState,
  elapsedText,
  errorLine,
  groupStatus,
  isUnfinished,
  movedToBackground,
  retryWords,
  statusMark,
  toolActivity,
  toolStatus,
} from "./status.js";

describe("tool status", () => {
  const call = (fields) => ({ id: "c", name: "bash", input: "{}", status: "running", result: null, ...fields });
  it("follows the result first", () => {
    expect(toolStatus(call({ result: { content: "x", is_error: false } }))).toBe("done");
    expect(toolStatus(call({ result: { content: "x", is_error: true } }))).toBe("error");
    expect(toolStatus(call({ status: "cancelled", result: { content: "x", is_error: false } }))).toBe("done");
  });
  it("calls a command moved to the background backgrounded", () => {
    const moved = { name: "bash", content: "x", metadata: JSON.stringify({ background: true }), is_error: false };
    expect(toolStatus(call({ result: moved }))).toBe("background");
    expect(movedToBackground({ ...moved, metadata: JSON.stringify({ background: true, shell_id: "7", end_time: 5 }) })).toBe(true);
    expect(movedToBackground({ ...moved, metadata: JSON.stringify({ background: true, end_time: 5 }) })).toBe(false);
    expect(movedToBackground({ ...moved, name: "job_output" })).toBe(false);
    expect(movedToBackground({ ...moved, metadata: "{" })).toBe(false);
    // A ToolCallView result carries no name: the call's name decides.
    const nameless = { content: "x", metadata: JSON.stringify({ background: true }), is_error: false };
    expect(movedToBackground(nameless)).toBe(false);
    expect(movedToBackground(nameless, "bash")).toBe(true);
    expect(toolStatus(call({ result: nameless }))).toBe("background");
    expect(toolStatus(call({ name: "view", result: nameless }))).toBe("done");
  });
  it("lets a stop mark win over the timeline's status, then permission and running marks", () => {
    expect(toolStatus(call({}), { c: "cancelled" })).toBe("cancelled");
    expect(toolStatus(call({ status: "cancelled" }))).toBe("cancelled");
    expect(toolStatus(call({ status: "pending" }), { c: "permission" })).toBe("pending");
    expect(toolStatus(call({}), { c: "permission" })).toBe("permission");
    expect(toolStatus(call({ status: "permission" }))).toBe("permission");
    expect(toolStatus(call({ status: "background" }))).toBe("background");
    expect(toolStatus(call({}))).toBe("running");
  });
  it("marks each status (CHAT-047)", () => {
    expect(statusMark("done")).toBe("check");
    expect(statusMark("background")).toBe("check");
    expect(statusMark("error")).toBe("cross");
    expect(statusMark("cancelled")).toBe("dot");
    for (const s of ["pending", "running", "permission"]) {
      expect(statusMark(s)).toBe("activity");
      expect(isUnfinished(s)).toBe(true);
    }
    expect(isUnfinished("cancelled")).toBe(false);
  });
});

describe("early states (CHAT-049, CHAT-050)", () => {
  it("says what a tool waits for", () => {
    expect(earlyState("running", null)).toEqual({ type: "early", tone: "waiting", text: "Waiting for tool response..." });
    expect(earlyState("pending", null).text).toBe("Waiting for tool response...");
    expect(earlyState("permission", null)).toEqual({ type: "early", tone: "waiting", text: "Requesting permission..." });
    expect(earlyState("cancelled", null)).toEqual({ type: "early", tone: "cancelled", text: "Canceled." });
    expect(earlyState("done", { content: "x" })).toBeNull();
    expect(earlyState("background", { content: "x" })).toBeNull();
  });
  it("puts an error on one line, a denial or stop as a warning", () => {
    expect(earlyState("error", { content: "line 1\nline 2" })).toEqual({ type: "error", tag: "ERROR", text: "line 1 line 2" });
    expect(errorLine("User denied permission").tag).toBe("WARN");
    expect(errorLine("tool: User cancelled the call").tag).toBe("WARN");
    expect(errorLine("denied").tag).toBe("ERROR");
  });
});

describe("live group words (CHAT-085)", () => {
  it("names what each running tool does", () => {
    const words = {
      agent: "Waiting for subagent",
      task: "Waiting for subagent",
      bash: "Running a command",
      job_output: "Checking a command",
      job_kill: "Checking a command",
      view: "Reading a file",
      edit: "Editing a file",
      multiedit: "Editing a file",
      write: "Editing a file",
      grep: "Searching",
      glob: "Searching",
      ls: "Searching",
      sourcegraph: "Searching",
      web_search: "Searching the web",
      fetch: "Reading a web page",
      web_fetch: "Reading a web page",
      download: "Reading a web page",
      todos: "Updating the plan",
      lsp_references: "Checking code",
      mcp_github_search: "Using github",
      // No "_" after the server: no server to name (strings.Cut fails).
      mcp_bare: "Working",
      mcp_: "Working",
      something_else: "Working",
    };
    for (const [name, text] of Object.entries(words)) expect(toolActivity(name), name).toBe(text);
  });
  it("follows the group's last step", () => {
    const tool = (name, status) => ({ kind: "tool", name, status });
    expect(groupStatus({ live: true, busy: true, last: tool("bash", "running") })).toBe("Running a command");
    expect(groupStatus({ live: true, busy: true, last: tool("edit", "pending") })).toBe("Editing a file");
    expect(groupStatus({ live: true, busy: true, last: tool("bash", "permission") })).toBe("Waiting for approval");
    expect(groupStatus({ live: true, busy: true, last: tool("bash", "cancelled") })).toBe("");
    expect(groupStatus({ live: true, busy: true, last: tool("bash", "done") })).toBe("Thinking");
    expect(groupStatus({ live: true, busy: false, last: tool("bash", "done") })).toBe("");
    expect(groupStatus({ live: false, busy: true, last: tool("bash", "running") })).toBe("");
    expect(groupStatus({ live: true, busy: true, last: tool("bash", "done"), compactingEarlier: true })).toBe("Compacting conversation");
  });
  it("says what a thinking step does", () => {
    const step = (fields) => ({ kind: "step", compacting: false, canceled: false, spinning: true, activity: "thinking", ...fields });
    expect(groupStatus({ live: true, busy: true, last: step({}) })).toBe("Thinking");
    expect(groupStatus({ live: true, busy: true, last: step({ activity: "writing" }) })).toBe("Writing command");
    expect(groupStatus({ live: true, busy: true, last: step({ activity: "rate_limited" }) })).toBe("Rate limited — waiting to retry");
    expect(groupStatus({ live: true, busy: true, last: step({ activity: "retrying" }) })).toBe("Waiting to retry");
    expect(groupStatus({ live: true, busy: true, last: step({ compacting: true }) })).toBe("Compacting conversation");
    expect(groupStatus({ live: true, busy: false, last: step({ canceled: true }) })).toBe("");
    expect(retryWords("thinking")).toBe("");
  });
});

describe("activity timer (activity_test.go)", () => {
  it("shows nothing under 10 seconds, then seconds, minutes and hours", () => {
    expect(elapsedText(9999)).toBe("");
    expect(elapsedText(10_000)).toBe("10s");
    expect(elapsedText(20_000)).toBe("20s");
    expect(elapsedText(65_000)).toBe("1m 5s");
    expect(elapsedText(3_725_000)).toBe("1h 2m");
    expect(elapsedText(-5)).toBe("");
  });
  it("restarts per tool call but not for a thinking heartbeat", () => {
    const group = `timer-${Math.random()}`;
    const t0 = 1_000_000;
    expect(activityStart(group, activityKey("Running a command", "first"), t0)).toBe(t0);
    expect(activityStart(group, activityKey("Running a command", "first"), t0 + 20_000)).toBe(t0);
    // The tool finished: thinking starts its own timer, and a heartbeat keeps it.
    expect(activityStart(group, activityKey("Thinking", "first"), t0 + 21_000)).toBe(t0 + 21_000);
    expect(activityStart(group, activityKey("Thinking", "first"), t0 + 30_000)).toBe(t0 + 21_000);
    expect(activityStart(group, activityKey("Running a command", "second"), t0 + 31_000)).toBe(t0 + 31_000);
    expect(activityStart(group, activityKey("Running a command", "third"), t0 + 40_000)).toBe(t0 + 40_000);
    expect(activityKey("", "x")).toBe("");
    expect(activityKey("Thinking", "")).toBe("Thinking");
  });
});

describe("thought line (thought_line_test.go)", () => {
  it("keeps the last line without heading marks or escape codes", () => {
    expect(cleanThoughtLine("First step.\n\n## Second step\n")).toBe("Second step");
    expect(cleanThoughtLine("**Checking how Magisk asks for su approval**")).toBe("Checking how Magisk asks for su approval");
    expect(cleanThoughtLine("Look at the result.\nPick the next file to read.")).toBe("Pick the next file to read.");
    expect(cleanThoughtLine("\x1b[1mBold\x1b[0m  ")).toBe("Bold");
    expect(cleanThoughtLine("")).toBe("");
  });
});
