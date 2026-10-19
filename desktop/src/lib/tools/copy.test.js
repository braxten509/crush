// CHAT-044, CHAT-056: the copy text of every tool kind, against the terminal's
// own answer file (internal/ui/chat/testdata/TestToolMessageItemRegistry.golden,
// made by tool_registry_test.go), plus the shell copy.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { copyName, copyParams, shellCopyText, SHELL_COPIED, TOOL_COPIED, toolCopyText } from "./copy.js";
import { toolStatus } from "./status.js";

const GOLDEN = new URL("../../../../internal/ui/chat/testdata/TestToolMessageItemRegistry.golden", import.meta.url);

// The golden's blocks: "=== name input state (*chat.Type)\n<copy>\n\n".
function goldenCases() {
  const text = readFileSync(GOLDEN, "utf8");
  const cases = [];
  for (const block of text.split(/^(?==== )/m)) {
    if (block === "") continue;
    const end = block.indexOf("\n");
    const m = /^=== (\S+) (.*) (pending|canceled|result|error|image) \(\*chat\.\w+\)$/.exec(block.slice(0, end));
    if (!m) throw new Error(`unreadable golden block: ${block.slice(0, 80)}`);
    if (!block.endsWith("\n\n")) throw new Error(`golden block without its blank line: ${m[0]}`);
    cases.push({ name: m[1], input: m[2], state: m[3], copy: block.slice(end + 1, -2) });
  }
  return cases;
}

// The states of tool_registry_test.go:70-80.
function callFor({ name, input, state }) {
  const call = { id: "call-1", name, input, messageId: "msg-1", status: "running", result: null, hooks: [], children: [] };
  if (state === "canceled") call.status = "cancelled";
  if (state === "result") call.result = { tool_call_id: "call-1", name, content: "line 1\nline 2", metadata: "", is_error: false };
  if (state === "error") call.result = { tool_call_id: "call-1", name, content: "boom", metadata: "", is_error: true };
  if (state === "image") call.result = { tool_call_id: "call-1", name, content: "", data: "AAAA", mime_type: "image/png", metadata: "", is_error: false };
  return call;
}

describe("tool copy text (Go golden)", () => {
  const cases = goldenCases();
  it("reads every case of the golden", () => {
    expect(cases.length).toBe(33 * 5);
  });
  for (const c of cases) {
    it(`${c.name} ${c.input} ${c.state}`, () => {
      const call = callFor(c);
      expect(toolCopyText(call, toolStatus(call))).toBe(c.copy);
    });
  }
});

describe("copy names and parameters", () => {
  it("humanizes names without a copy name", () => {
    expect(copyName("lsp_diagnostics")).toBe("Lsp Diagnostics");
    expect(copyName("mcp_github_get-issue")).toBe("Mcp Github Get Issue");
    expect(copyName("web_search")).toBe("Search");
  });
  it("prints generic values as Go's %v does", () => {
    expect(copyParams("my_tool", '{"n":1000000,"f":0.5,"big":1234567,"small":0.00001,"list":[1,"a",true,null],"map":{"b":2,"a":"x"}}')).toBe(
      ["**N:** 1e+06", "**F:** 0.5", "**Big:** 1.234567e+06", "**Small:** 1e-05", "**List:** [1 a true <nil>]", "**Map:** map[a:x b:2]"].join("\n"),
    );
  });
  it("formats download timeouts as Go durations", () => {
    expect(copyParams("download", '{"url":"u","file_path":"f","timeout":90}')).toBe("**URL:** u\n**File Path:** f\n**Timeout:** 1m30s");
  });
  it("copies an edit as a diff with its counts", () => {
    const call = { id: "e", name: "edit", input: '{"file_path":"a.go","old_string":"x","new_string":"y"}' };
    call.result = { content: "ok", metadata: JSON.stringify({ old_content: "a\nx\n", new_content: "a\ny\n", additions: 1, removals: 1 }), is_error: false };
    expect(toolCopyText(call, "done")).toBe(
      ["## Edit Tool Call", "### Parameters:", "**File:** a.go", "### Result:", "Changes: +1 -1\n```diff\n--- a/a.go\n+++ b/a.go\n@@ -1,2 +1,2 @@\n a\n-x\n+y\n\n```"].join("\n\n"),
    );
  });
  it("copies a view result in a fence named by the file's extension", () => {
    const call = { id: "v", name: "view", input: '{"file_path":"x.PY"}' };
    call.result = { content: "raw", metadata: JSON.stringify({ file_path: "src/x.PY", content: "print(1)" }), is_error: false };
    expect(toolCopyText(call, "done")).toBe("## View Tool Call\n\n### Parameters:\n\n**File:** x.PY\n\n### Result:\n\n```python\nprint(1)\n```");
  });
  it("keeps the bash output from the metadata and drops 'no output'", () => {
    const call = { id: "b", name: "bash", input: '{"command":"true"}', result: { content: "no output", metadata: "", is_error: false } };
    expect(toolCopyText(call, "done")).toBe("## Bash Tool Call\n\n### Parameters:\n\n**Command:** true\n\n### Result:");
    call.result = { content: "x", metadata: JSON.stringify({ output: "real" }), is_error: false };
    expect(toolCopyText(call, "done")).toMatch(/```bash\nreal\n```$/);
  });
});

describe("shell copy (CHAT-044)", () => {
  it("copies the command and the output without escape codes", () => {
    expect(shellCopyText("ls --color", "\x1b[31mred\x1b[0m\n\x1b]8;;http://x\x07link\x1b]8;;\x07")).toBe("$ ls --color\nred\nlink");
  });
  it("names the notices", () => {
    expect(SHELL_COPIED).toBe("Shell output copied to clipboard");
    expect(TOOL_COPIED).toBe("Tool content copied to clipboard");
  });
});
