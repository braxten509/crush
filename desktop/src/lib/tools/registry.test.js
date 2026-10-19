// CHAT-046 to CHAT-079: what each tool's row shows, against the terminal's
// own answer files (internal/ui/chat/testdata/TestStandardToolRendering.golden
// from tool_render_test.go and TestDockerMCPToolRendering.golden from
// docker_mcp_test.go), plus the per-tool rules of bash.go, file.go, mcp.go,
// generic.go, agent.go and edit_error_test.go.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { cells } from "../diff.js";
import { truncate } from "./go.js";
import { lineCounts, nameText, pairsFit, pairsText, pendingName, splitParams, toolView } from "./registry.js";
import { statusMark } from "./status.js";

const ICONS = { check: "✓", cross: "×", dot: "●", activity: "●" };

function stripAnsi(text) {
  return text.replace(/\x1b\[[0-9;:]*[A-Za-z]/g, "").replace(/\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)/g, "");
}

function goldenBlocks(file) {
  const text = stripAnsi(readFileSync(new URL(`../../../../internal/ui/chat/testdata/${file}`, import.meta.url), "utf8"));
  return text
    .split(/^(?==== )/m)
    .filter(Boolean)
    .map((block) => {
      const end = block.indexOf("\n");
      return { head: block.slice(0, end), lines: block.slice(end + 1).split("\n") };
    });
}

// The header line as the terminal lays it out in width cells: the mark, the
// name, the main parameter and, when 30 cells stay, the pairs; cut with "…",
// or in full when the row is expanded (it wraps, CHAT-053).
function headerLine(view, status, width, expanded = false) {
  if (view.invalid) return `ERROR  ${view.invalid}`;
  const icon = view.forceDone ? "✓" : ICONS[statusMark(status)];
  const prefix = `${icon} ${nameText(view.name)} `;
  const room = width - cells(prefix);
  let params = view.main;
  if (pairsFit(view.pairs, room, cells)) params += ` (${pairsText(view.pairs)})`;
  return (prefix + (expanded ? params : truncate(params, room))).trimEnd();
}

// A golden block's header (its lines up to the first blank one, a wrapped
// header joined back) and its body lines.
function splitBlock(lines) {
  const raw = lines.map((l) => l.trimEnd());
  const blank = raw.indexOf("");
  const head = raw.slice(0, blank < 0 ? raw.length : blank);
  return { header: head.map((l) => l.trim()).join(""), body: blank < 0 ? [] : raw.slice(blank) };
}

// The body's text lines; null for a part drawn as Markdown (the terminal's
// glamour output is not the desktop's).
function partLines(part) {
  switch (part.type) {
    case "early":
    case "empty":
      return [part.text];
    case "error":
      return [`${part.tag}  ${part.text}`];
    case "plain":
      return part.text.split("\n");
    case "code":
      return part.text.split("\n").map((line, i) => `${i + 1 + part.offset}   ${line}`);
    case "image":
      return [`Loaded Image → ${part.mime} ${part.size}`];
    case "servers":
      return [...part.rows.map((r) => `${r.name} ${r.description}`), ...(part.more ? [part.more] : [])];
    case "markdown":
      return null;
  }
  throw new Error(`no text for a ${part.type} part`);
}

const clean = (lines) => lines.map((l) => l.trim()).filter((l) => l !== "");
const squeeze = (lines) => clean(lines).map((l) => l.replace(/\s+/g, " "));

// tool_render_test.go:15-40.
const STANDARD_CASES = [
  ["glob", `{"pattern":"*.go","path":"internal"}`, "a.go\nb.go", ""],
  ["grep", `{"pattern":"func","path":"internal","include":"*.go","literal_text":true}`, "a.go:1: func main()", ""],
  ["ls", `{"path":""}`, "- a.go\n- b.go", ""],
  ["sourcegraph", `{"query":"repo:x foo","count":3,"context_window":2}`, "result", ""],
  ["fetch", `{"url":"https://example.com","format":"html","timeout":5}`, "<p>hi</p>", ""],
  ["web_fetch", `{"url":"https://example.com"}`, "# Title\n\nBody", ""],
  ["web_search", `{"query":"crush"}`, "- [Crush](https://charm.land)", ""],
  ["download", `{"url":"https://example.com/f","file_path":"f.bin","timeout":30}`, "saved", ""],
  ["lsp_references", `{"symbol":"Foo","path":"a.go"}`, "a.go:1:1", ""],
  ["lsp_definition", `{"symbol":"Foo"}`, "a.go:1", `{"file_path":"a.go","content":"func Foo() {}"}`],
  ["lsp_definition", `{"symbol":"Foo"}`, "a.go:1", ""],
  ["lsp_rename", `{"symbol":"Foo","new_name":"Bar","path":"a.go"}`, "renamed 2", ""],
  ["lsp_call_hierarchy", `{"symbol":"Foo","direction":"outgoing"}`, "Foo -> Bar", ""],
  ["lsp_symbols", `{"file_path":"a.go"}`, "Foo\n  Bar", ""],
  ["lsp_diagnostics", `{"file_path":"a.go"}`, "no issues", ""],
  ["lsp_diagnostics", `{}`, "no issues", ""],
  ["lsp_restart", `{"name":"gopls"}`, "restarted", ""],
  ["lsp_restart", `{}`, "restarted", ""],
  ["glob", `not json`, "a.go", ""],
  ["lsp_symbols", `not json`, "Foo", ""],
];

// The states of tool_render_test.go:52-67: [status, result, compact].
function standardState(label, name, content, metadata) {
  const result = { tool_call_id: "call-1", name, content, metadata, is_error: false };
  switch (label) {
    case "running":
      return ["running", null, false];
    case "canceled":
      return ["cancelled", null, false];
    case "success":
    case "expanded":
      return ["done", result, false];
    case "compact":
      return ["done", result, true];
    case "empty":
      return ["done", { tool_call_id: "call-1", content: "", metadata: "", is_error: false }, false];
    case "error":
      return ["error", { tool_call_id: "call-1", content: "boom", metadata: "", is_error: true }, false];
  }
  throw new Error(label);
}

// Render(80) gives the standard tools 74 cells (cappedMessageWidth twice).
const STANDARD_WIDTH = 74;

describe("standard tools (Go golden)", () => {
  const blocks = goldenBlocks("TestStandardToolRendering.golden");
  it("reads every case", () => {
    expect(blocks.length).toBe(STANDARD_CASES.length * 7);
  });
  blocks.forEach((block, n) => {
    const m = /^=== (\S+) (.*) (running|canceled|success|expanded|compact|empty|error)$/.exec(block.head);
    const [name, input, content, metadata] = STANDARD_CASES[Math.floor(n / 7)];
    it(block.head.slice(4), () => {
      expect(m[1]).toBe(name);
      expect(m[2]).toBe(input);
      const [status, result, compact] = standardState(m[3], name, content, metadata);
      const view = toolView({ id: "call-1", name, input, result }, { status, compact });
      const want = clean(block.lines);
      expect(headerLine(view, status, STANDARD_WIDTH)).toBe(want[0]);
      const body = view.body.map(partLines);
      if (body.includes(null)) {
        expect(view.body).toEqual([{ type: "markdown", text: content }]);
        return;
      }
      expect(body.flat()).toEqual(want.slice(1));
    });
  });
});

// docker_mcp_test.go:28-49.
const PNG = Buffer.from("\x89PNG\r\n\x1a\nnot really a picture", "latin1").toString("base64");
const servers = (n) => JSON.stringify({ servers: Array.from({ length: n }, (_, i) => ({ name: `server-${i}`, description: `Server number ${i}` })) });
const DOCKER_CASES = [
  ["mcp-find", `{"query":"github","limit":5,"verbose":true}`, { content: servers(3) }],
  ["mcp-find", `{"query":"github"}`, { content: servers(12) }],
  ["mcp-find", `{"query":"none"}`, { content: servers(0) }],
  ["mcp-find", `{"query":"bad"}`, { content: "not json output" }],
  ["mcp-find", `{"query":"empty"}`, { content: "" }],
  ["mcp-find", `{"query":42}`, { content: servers(1) }],
  ["mcp-add", `{"name":"github","activate":true,"config":{"a":1}}`, { content: "added" }],
  ["mcp-add", `{"name":7}`, { content: "added" }],
  ["mcp-remove", `{"name":"github","force":false}`, { content: "removed" }],
  ["mcp-exec", `{"name":"search","arguments":{"q":"x"}}`, { content: "line one\nline two" }],
  ["mcp-config-set", `{"server":"github","key":"token"}`, { content: "set" }],
  ["code-mode", `{"script":"print(1)"}`, { content: "1" }],
  ["list_tools", `{}`, { content: "a\nb" }],
  ["mcp-exec", `not json`, { content: "ran" }],
  ["mcp-exec", `{"name":"shot"}`, { content: "", data: PNG, mime_type: "image/png" }],
  ["mcp-exec", `{"name":"shot"}`, { content: "with image", data: PNG, mime_type: "image/png" }],
  ["mcp-exec", `{"name":"blob"}`, { content: "", data: PNG, mime_type: "application/octet-stream" }],
];
const DOCKER_STATES = ["success", "expanded", "compact", "no result", "running", "canceled", "error"];

describe("Docker MCP tools (Go golden)", () => {
  const blocks = goldenBlocks("TestDockerMCPToolRendering.golden");
  it("reads every case", () => {
    expect(blocks.length).toBe(DOCKER_CASES.length * DOCKER_STATES.length * 2);
  });
  blocks.forEach((block, n) => {
    const [tool, input, fields] = DOCKER_CASES[Math.floor(n / (DOCKER_STATES.length * 2))];
    const label = DOCKER_STATES[Math.floor(n / 2) % DOCKER_STATES.length];
    const width = n % 2 === 0 ? 40 : 100;
    it(block.head.slice(4), () => {
      expect(block.head).toBe(`=== ${tool} ${input} ${label} @${width}`);
      const name = "mcp_docker_" + tool;
      const success = { tool_call_id: "call-1", name, metadata: "", is_error: false, ...fields };
      const [status, result, compact] = {
        success: ["done", success, false],
        expanded: ["done", success, false],
        compact: ["done", success, true],
        "no result": ["done", null, false],
        running: ["running", null, false],
        canceled: ["cancelled", null, false],
        error: ["error", { tool_call_id: "call-1", content: "boom", metadata: "", is_error: true }, false],
      }[label];
      const view = toolView({ id: "call-1", name, input, result }, { status, compact });
      const want = splitBlock(block.lines);
      // RenderTool(width) lays the header out in cappedMessageWidth(width) cells.
      expect(headerLine(view, status, width - 3, label === "expanded")).toBe(want.header);
      expect(squeeze(view.body.flatMap(partLines))).toEqual(squeeze(want.body));
    });
  });
  it("names the compact rows as the terminal's nested rows", () => {
    const view = toolView({ id: "c", name: "mcp_docker_mcp-remove", input: `{"name":"x"}`, result: null }, { status: "running", compact: true });
    expect(view.name).toEqual({ text: "Docker MCP: remove" });
    expect(view.forceDone).toBe(true);
  });
  it("draws Add as a create and Remove as a destroy", () => {
    expect(toolView({ name: "mcp_docker_mcp-add", input: "{}" }, { status: "done" }).name).toEqual({ docker: "Docker MCP", action: "Add", tone: "create" });
    expect(toolView({ name: "mcp_docker_mcp-remove", input: "{}" }, { status: "done" }).name).toEqual({ docker: "Docker MCP", action: "Remove", tone: "destroy" });
  });
});

describe("parameters", () => {
  it("splits the main parameter from key and value pairs, skipping empty values", () => {
    expect(splitParams(["main", "a", "1", "b", "", "c"])).toEqual({ main: "main", pairs: [["a", "1"]] });
    expect(splitParams([])).toEqual({ main: "", pairs: [] });
  });
  it("shows pairs only when 30 cells stay for the main parameter", () => {
    const pairs = [["path", "internal"]];
    // "path=internal" is 13 cells and "(…)" with its space 3 more.
    expect(pairsFit(pairs, 46, cells)).toBe(true);
    expect(pairsFit(pairs, 45, cells)).toBe(false);
    expect(pairsFit([], 200, cells)).toBe(false);
  });
  it("runs a nested row's parameters together", () => {
    const view = toolView({ name: "bash", input: JSON.stringify({ command: "echo one\n  echo two" }) }, { status: "done", compact: true });
    expect(view.main).toBe("echo one echo two");
  });
  it("names a tool while its input streams", () => {
    expect(pendingName({ name: "multiedit", input: "" })).toEqual({ text: "Multi-Edit" });
    expect(pendingName({ name: "mcp_github_get_issue", input: "" })).toEqual({ server: "Github", tool: "Get Issue" });
    expect(toolView({ name: "view", input: "{" }, { status: "pending" }).pending).toBe(true);
  });
});

describe("bash (bash.go)", () => {
  const bash = (input, result, status = "done", workingDir = "") => toolView({ id: "b", name: "bash", input: JSON.stringify(input), result }, { status, workingDir });
  it("keeps the whole command, wrapped, as bash", () => {
    const view = bash({ command: "echo one\necho two\necho three" }, { name: "bash", content: "out", metadata: "", is_error: false });
    expect(view.main).toBe("echo one\necho two\necho three");
    expect(view.mainLang).toBe("bash");
    expect(view.wrap).toBe(true);
    expect(view.body).toEqual([{ type: "plain", text: "out" }]);
  });
  it("drops the working folder's cd prefix and turns tabs to spaces", () => {
    expect(bash({ command: "cd /work && go\ttest" }, null, "running", "/work").main).toBe("go    test");
  });
  it("says when the command could not be read", () => {
    expect(toolView({ name: "bash", input: "{" }, { status: "running" }).main).toBe("failed to parse command");
  });
  it("marks a background command", () => {
    expect(bash({ command: "sleep 9", run_in_background: true }, null, "running").pairs).toEqual([["background", "true"]]);
    const moved = { name: "bash", content: "x", metadata: JSON.stringify({ background: true, end_time: 0 }), is_error: false };
    expect(bash({ command: "sleep 9" }, moved, "background").pairs).toEqual([["background", "true"]]);
  });
  it("prefers the output in the metadata and hides 'no output'", () => {
    expect(bash({ command: "x" }, { name: "bash", content: "c", metadata: JSON.stringify({ output: "real" }), is_error: false }).body).toEqual([{ type: "plain", text: "real" }]);
    expect(bash({ command: "true" }, { name: "bash", content: "no output", metadata: "", is_error: false }).body).toEqual([]);
  });
  it("draws a Crush job as a Job row", () => {
    const result = { name: "bash", content: "started", metadata: JSON.stringify({ background: true, shell_id: "7", description: "dev server" }), is_error: false };
    const view = bash({ command: "npm run dev" }, result, "background");
    expect(view.name).toEqual({ text: "Job" });
    expect(view.job).toEqual({ action: "Start", pid: "7", description: "dev server" });
    expect(view.body).toEqual([{ type: "plain", text: "Command: npm run dev\nstarted" }]);
  });
  it("draws job output and kill rows", () => {
    const result = { content: "line", metadata: JSON.stringify({ command: "npm run dev" }), is_error: false };
    const view = toolView({ name: "job_output", input: `{"shell_id":"7"}`, result }, { status: "done" });
    expect(view.job).toEqual({ action: "Output", pid: "7", description: "npm run dev" });
    expect(toolView({ name: "job_kill", input: `{"shell_id":7}` }, { status: "running" }).invalid).toBe("Invalid parameters");
  });
});

describe("files (file.go)", () => {
  it("shows a view's code from its offset, with limit and offset pairs", () => {
    const result = { content: "raw", metadata: JSON.stringify({ file_path: "a.go", content: "x := 1" }), is_error: false };
    const view = toolView({ name: "view", input: `{"file_path":"a.go","offset":10,"limit":5}`, result }, { status: "done" });
    expect(view.pairs).toEqual([
      ["limit", "5"],
      ["offset", "10"],
    ]);
    expect(view.body).toEqual([{ type: "code", path: "a.go", text: "x := 1", offset: 10 }]);
  });
  it("shows a picture a view loaded", () => {
    const result = { content: "", data: "AAAA", mime_type: "image/png", metadata: "", is_error: false };
    expect(toolView({ name: "view", input: `{"file_path":"a.png"}`, result }, { status: "done" }).body).toEqual([{ type: "image", data: "AAAA", mime: "image/png", size: "3 B" }]);
  });
  it("shows a skill a view loaded", () => {
    const result = { content: "x", metadata: JSON.stringify({ resource_type: "skill", resource_name: "align", resource_description: "Measure" }), is_error: false };
    expect(toolView({ name: "view", input: `{"file_path":"SKILL.md"}`, result }, { status: "done" }).body).toEqual([{ type: "skill", name: "align", description: "Measure" }]);
  });
  it("shows what a write wrote, and its error with the diff", () => {
    const ok = toolView({ name: "write", input: `{"file_path":"a.txt","content":"hi"}`, result: { content: "ok", metadata: "", is_error: false } }, { status: "done" });
    expect(ok.body).toEqual([{ type: "code", path: "a.txt", text: "hi", offset: 0 }]);
    const diff = "--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-a\n+b\n";
    const failed = toolView({ name: "write", input: `{"file_path":"a.txt","content":"b"}`, result: { content: "stale", metadata: JSON.stringify({ diff }), is_error: true } }, { status: "error" });
    expect(failed.body).toEqual([{ type: "error", tag: "ERROR", text: "stale" }, { type: "diff", files: [{ path: "a.txt", before: "a", after: "b" }] }]);
  });
  it("shows an edit's diff across the full width", () => {
    const result = { content: "ok", metadata: JSON.stringify({ old_content: "a\n", new_content: "b\n" }), is_error: false };
    const view = toolView({ name: "edit", input: `{"file_path":"a.go","old_string":"a","new_string":"b"}`, result }, { status: "done" });
    expect(view.wide).toBe(true);
    expect(view.body).toEqual([{ type: "diff", files: [{ path: "a.go", before: "a\n", after: "b\n" }], cutAbove: false }]);
  });
  it("notes the edits of a multi-edit that failed", () => {
    const meta = { old_content: "a\nb\n", new_content: "A\nb\n", edits_applied: 1, edits_failed: [{ index: 1, error: "not found", edit: { old_string: "z", new_string: "Z" } }] };
    const input = JSON.stringify({ file_path: "a.go", edits: [{ old_string: "a", new_string: "A" }, { old_string: "z", new_string: "Z" }] });
    const view = toolView({ name: "multiedit", input, result: { content: "partial", metadata: JSON.stringify(meta), is_error: false } }, { status: "done" });
    expect(view.pairs).toEqual([["edits", "2"]]);
    expect(view.body[0].cutAbove).toBe(true);
    expect(view.body[1]).toEqual({ type: "note", tag: "Note", text: "1 of 2 edits succeeded" });
  });
  it("shows only the error of an edit refused before any diff (edit_error_test.go)", () => {
    const meta = `{"hook":{"hooks":[{"name":"wakatime","event":"PreToolUse","duration_ms":12}]}}`;
    for (const [name, input] of [
      ["edit", `{"file_path":"/tmp/x.h"}`],
      ["multiedit", `{"file_path":"/tmp/x.h","edits":[]}`],
    ]) {
      const result = { content: "you must read the file before editing it", metadata: meta, is_error: true };
      expect(toolView({ name, input, result }, { status: "error" }).body).toEqual([{ type: "error", tag: "ERROR", text: "you must read the file before editing it" }]);
    }
  });
  it("calls a denial a warning", () => {
    const result = { content: "User denied permission", metadata: "", is_error: true };
    expect(toolView({ name: "glob", input: `{"pattern":"*"}`, result }, { status: "error" }).body).toEqual([{ type: "error", tag: "WARN", text: "User denied permission" }]);
  });
  it("counts the lines an edit, multi-edit or write changed (4.6)", () => {
    const ok = (meta) => ({ content: "ok", metadata: JSON.stringify(meta), is_error: false });
    expect(lineCounts({ name: "edit", result: ok({ additions: 3, removals: 1 }) })).toEqual({ adds: 3, dels: 1 });
    expect(lineCounts({ name: "multiedit", result: ok({ additions: 0, removals: 2 }) })).toEqual({ adds: 0, dels: 2 });
    expect(lineCounts({ name: "write", result: ok({ additions: 5, removals: 0 }) })).toEqual({ adds: 5, dels: 0 });
    expect(lineCounts({ name: "edit", result: ok({ additions: 0, removals: 0 }) })).toBeNull();
    expect(lineCounts({ name: "edit", result: { ...ok({ additions: 3 }), is_error: true } })).toBeNull();
    expect(lineCounts({ name: "edit", result: null })).toBeNull();
    expect(lineCounts({ name: "bash", result: ok({ additions: 3 }) })).toBeNull();
    expect(lineCounts({ name: "edit", result: { content: "", metadata: "{", is_error: false } })).toBeNull();
  });
});

describe("MCP and other tools (mcp.go, generic.go, agent.go)", () => {
  it("names an MCP tool by its server and tool", () => {
    const view = toolView({ name: "mcp_github_get_issue", input: `{"repo":"x","n":1}`, result: { content: '{"a":1}', metadata: "", is_error: false } }, { status: "done" });
    expect(view.name).toEqual({ server: "Github", tool: "Get Issue" });
    expect(view.main).toBe('{"n":1,"repo":"x"}');
    expect(view.body).toEqual([{ type: "code", path: "result.json", text: '{\n  "a": 1\n}', offset: 0 }]);
  });
  it("refuses an MCP name without a tool", () => {
    expect(toolView({ name: "mcp_github", input: "{}" }, { status: "done" }).invalid).toBe("Invalid tool name");
  });
  it("names other tools from their name and shows their input as JSON", () => {
    const view = toolView({ name: "my_tool", input: `{"b":"<x>","a":2}` }, { status: "running" });
    expect(nameText(view.name)).toBe("My Tool");
    expect(view.main).toBe('{"a":2,"b":"\\u003cx\\u003e"}');
    expect(view.body).toEqual([{ type: "early", tone: "waiting", text: "Waiting for tool response..." }]);
  });
  it("hides a picture without text in a generic result (generic.go:60-69)", () => {
    const picture = { content: "", data: "AAAA", mime_type: "image/png", metadata: "", is_error: false };
    expect(toolView({ name: "shot", input: "{}", result: picture }, { status: "done" }).body).toEqual([]);
    expect(toolView({ name: "shot", input: "{}", result: { ...picture, content: "here" } }, { status: "done" }).body[0].type).toBe("image");
  });
  it("shows an agent's task, its activity until a result, then the result", () => {
    const running = toolView({ name: "agent", input: `{"prompt":"look"}`, result: null, children: [] }, { status: "running" });
    expect(running.agent).toEqual({ tag: "Task", prompt: "look", running: true });
    expect(running.body).toEqual([]);
    const done = toolView({ name: "agentic_fetch", input: `{"url":"https://x","prompt":"sum"}`, result: { content: "**ok**", metadata: "", is_error: false } }, { status: "done" });
    expect(done.main).toBe("https://x");
    expect(done.agent).toEqual({ tag: "Prompt", prompt: "sum", running: false });
    expect(done.body).toEqual([{ type: "markdown", text: "**ok**" }]);
  });
  it("draws a replace-symbol diff and drops its file from the header", () => {
    const result = { content: "ok", metadata: JSON.stringify({ old_content: "a", new_content: "b" }), is_error: false };
    const view = toolView({ name: "lsp_replace_symbol", input: `{"symbol":"Foo","file_path":"a.go"}`, result }, { status: "done" });
    expect(view.main).toBe("Foo");
    expect(view.pairs).toEqual([]);
    expect(view.body[0].type).toBe("diff");
  });
});
