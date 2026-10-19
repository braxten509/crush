// Tool and group status (CHAT-047 to CHAT-050, CHAT-081, CHAT-085): the
// status a tool row shows, its early-state line, and the words a working
// group says. Ports of chat/tools.go:356-377, tool_header.go:31-82,
// toolgroup.go:293-370,460-473, activity_timer.go and thought_line.go.
import { stripAnsi } from "../render/ansi.js";
import { decode } from "./go.js";

const BASH_META = {
  start_time: "int",
  end_time: "int",
  output: "string",
  description: "string",
  working_directory: "string",
  background: "bool",
  shell_id: "string",
};

// movedToBackground (bash.go:112-125): a bash result whose command went on
// running in the background: a Crush job, or a CLI command moved there.
// name is the call's tool name (a ToolCallView result does not carry one).
export function movedToBackground(result, name = result?.name) {
  if (!result || name !== "bash") return false;
  const { value: meta, ok } = decode(result.metadata ?? "", BASH_META);
  if (!ok) return false;
  return meta.background && (meta.shell_id !== "" || meta.end_time === 0);
}

// The status a tool shows (ToolCallView status, lib/types.js). A result
// decides it (error, or done; "background" for a command moved to the
// background); without one, the stop mark of prompts.callStatus wins, then
// the timeline's own status, then a permission or running mark.
export function toolStatus(call, marks = {}) {
  const result = call?.result;
  if (result) {
    if (result.is_error) return "error";
    return movedToBackground(result, call.name ?? result.name) ? "background" : "done";
  }
  const mark = marks?.[call?.id];
  if (call?.status === "cancelled" || mark === "cancelled") return "cancelled";
  if (call?.status === "pending") return "pending";
  if (mark === "permission") return "permission";
  if (mark === "running") return "running";
  if (call?.status === "permission" || call?.status === "background") return call.status;
  return "running";
}

// Still working: no result yet and not stopped.
export const isUnfinished = (status) => status === "pending" || status === "running" || status === "permission";

// The status mark of a tool line (CHAT-047): the activity mark while it
// works, a check when done (a command moved to the background counts as
// done, as the terminal's ✓), a cross on error, a dot when stopped.
export function statusMark(status) {
  switch (status) {
    case "done":
    case "background":
      return "check";
    case "error":
      return "cross";
    case "cancelled":
      return "dot";
  }
  return "activity";
}

// toolErrorContent (tool_header.go:50-67): the error tag and the message on
// one line; a denial or a stop is a warning.
export function errorLine(content) {
  const text = String(content ?? "").replaceAll("\n", " ");
  const warn = text.includes("User denied permission") || text.includes("User cancelled");
  return { type: "error", tag: warn ? "WARN" : "ERROR", text };
}

// toolEarlyStateContent (tool_header.go:31-48): the line a tool shows instead
// of its body while it waits, after it failed or when it was stopped; null
// once it has a successful result.
export function earlyState(status, result) {
  switch (status) {
    case "error":
      return errorLine(result?.content ?? "");
    case "cancelled":
      return { type: "early", tone: "cancelled", text: "Canceled." };
    case "permission":
      return { type: "early", tone: "waiting", text: "Requesting permission..." };
    case "running":
    case "pending":
      return { type: "early", tone: "waiting", text: "Waiting for tool response..." };
  }
  return null;
}

// toolActivity (toolgroup.go:341-370): a running tool in a few words.
export function toolActivity(name) {
  switch (name) {
    case "agent":
    case "task":
      return "Waiting for subagent";
    case "bash":
      return "Running a command";
    case "job_output":
    case "job_kill":
      return "Checking a command";
    case "view":
      return "Reading a file";
    case "edit":
    case "multiedit":
    case "write":
      return "Editing a file";
    case "grep":
    case "glob":
    case "ls":
    case "sourcegraph":
      return "Searching";
    case "web_search":
      return "Searching the web";
    case "fetch":
    case "web_fetch":
    case "download":
      return "Reading a web page";
    case "todos":
      return "Updating the plan";
  }
  const n = String(name ?? "");
  if (n.startsWith("lsp_")) return "Checking code";
  if (n.startsWith("mcp_")) {
    const rest = n.slice(4);
    const cut = rest.indexOf("_");
    if (cut >= 0) return "Using " + rest.slice(0, cut);
  }
  return "Working";
}

// retryActivityStatus (assistant_render.go:198-207).
export function retryWords(activity) {
  if (activity === "rate_limited") return "Rate limited — waiting to retry";
  if (activity === "retrying") return "Waiting to retry";
  return "";
}

// ToolGroupItem.status (toolgroup.go:293-339): what a live group is doing
// now, or "" when it is not working. last is the group's last step:
//   {kind: "tool", name, status}  (status as toolStatus gives it)
//   {kind: "step", compacting, canceled, spinning, activity}  (a thinking-only step)
// busy: the agent still works; compactingEarlier: an earlier step compacts.
export function groupStatus({ live = true, last = null, busy = false, compactingEarlier = false } = {}) {
  if (!live || !last) return "";
  if (last.kind === "step") {
    if (last.compacting) return "Compacting conversation";
    if (last.canceled) return "";
    if (last.spinning) {
      const retry = retryWords(last.activity);
      if (retry) return retry;
      if (last.activity === "writing") return "Writing command";
      return "Thinking";
    }
  } else if (last.kind === "tool") {
    switch (last.status) {
      case "cancelled":
        return "";
      case "permission":
        return "Waiting for approval";
      case "running":
      case "pending":
        return toolActivity(last.name);
    }
  }
  if (busy) return compactingEarlier ? "Compacting conversation" : "Thinking";
  return "";
}

// The activity timer's key (toolgroup.go:460-473): the status plus the last
// tool call's id, so the timer restarts for each call, but not when a
// thinking heartbeat arrives after a finished tool.
export function activityKey(status, lastToolId) {
  if (!status) return "";
  return lastToolId ? status + "\x00" + lastToolId : status;
}

// activityTimer (activity_timer.go): when each group's current activity
// started, kept for the window's life by group id.
const started = new Map();

export function activityStart(groupId, key, now = Date.now()) {
  const known = started.get(groupId);
  if (!known || known.key !== key) {
    started.set(groupId, { key, at: now });
    return now;
  }
  return known.at;
}

// The elapsed text: nothing under 10 s, then "12s", "3m 4s", "1h 2m".
export function elapsedText(ms) {
  const seconds = Math.max(0, Math.floor(ms / 1000));
  if (seconds < 10) return "";
  if (seconds >= 3600) return `${Math.floor(seconds / 3600)}h ${Math.floor(seconds / 60) % 60}m`;
  if (seconds >= 60) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
  return `${seconds}s`;
}

// cleanThoughtLine (thought_line.go:46-54): the last non-blank line without
// escape codes or the marks models put around thinking headings.
export function cleanThoughtLine(text) {
  let t = stripAnsi(String(text ?? "")).replace(/[ \t\r\n]+$/, "");
  const i = t.lastIndexOf("\n");
  if (i >= 0) t = t.slice(i + 1);
  return t.replace(/^[ \t*_#`]+|[ \t*_#`]+$/g, "");
}
