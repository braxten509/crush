// CHAT-080 to CHAT-085: what a tool group's row says, against the terminal's
// own answer file (internal/ui/chat/testdata/TestToolGroupHeaderStates.golden
// from toolgroup_header_test.go) and the cases of file_change_label_test.go,
// changes_exclusions_test.go and changes_transfers_test.go.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { cells } from "../diff.js";
import { hiddenReviewPath } from "./review.js";
import { groupStatus, toolStatus } from "./status.js";
import { countLabel, fileChangeLabel, groupChanges, groupExtras, groupMark, groupRow, shortFileName, storedSummary, tally, toolVerbs } from "./summary.js";

let nextGroup = 0;
const tool = (id, name, input, result, status = "done") => ({
  id,
  name,
  input,
  messageId: "m-" + id,
  status,
  result: result ? { tool_call_id: id, name, metadata: "", is_error: false, ...result } : null,
  hooks: [],
  children: [],
});
const ok = (content) => ({ content });
const bash = (id) => tool(id, "bash", `{"command":"ls"}`, ok("ok"));
const failed = (id) => tool(id, "bash", `{"command":"false"}`, { content: "exit 1", is_error: true });
const running = (id, name, input) => tool(id, name, input, null, "running");
const edit = (id, path) =>
  tool(id, "edit", `{"file_path":"${path}","old_string":"x","new_string":"y"}`, {
    content: "ok",
    metadata: `{"additions":1,"removals":1,"old_content":"a\\nx\\n","new_content":"a\\ny\\nz\\n"}`,
  });
const legacyEdit = (id, path) => tool(id, "write", `{"file_path":"${path}"}`, ok("ok"));
const reviewed = (id, ...changes) => tool(id, "bash", `{"command":"script"}`, { content: "ok", review: { root: "/w", changes } });
const stored = (id, summary) => tool(id, "bash", `{"command":"script"}`, { content: "ok", review: { id, root: "", summary } });
const text = (s) => ({ content: s, mode: 0o644, size: s.length });
const moved = (id) => tool(id, "bash", `{"command":"sleep 9"}`, { content: "moved", metadata: `{"background":true}` });
const THINKING = { thinking: true };

// toolgroup_header_test.go:65-99.
const CASES = [
  ["one action", [bash("a")]],
  ["expanded", [bash("a"), bash("b")]],
  ["last failed", [bash("a"), failed("b")]],
  ["earlier failed", [failed("a"), failed("b"), bash("c")]],
  ["backgrounded", [moved("a")]],
  ["backgrounded earlier", [moved("a"), bash("b")]],
  ["edits", [edit("a", "/p/a.go"), edit("b", "/p/b.go")]],
  ["one edit", [edit("a", "/p/a-very-long-file-name-for-the-summary.go")]],
  ["legacy edits", [legacyEdit("a", "/p/a.go"), legacyEdit("b", "/p/b.go"), bash("c")]],
  ["review edit", [reviewed("a", { path: "/w/a.txt", before: text("a\n"), after: text("b\n") })]],
  ["review removed", [reviewed("a", { path: "/w/a.txt", before: text("a\n") })]],
  ["review mixed", [reviewed("a", { path: "/w/a.txt", before: text("a\n") }, { path: "/w/b.txt", before: text("a\n"), after: text("b\nc\n") })]],
  ["stored summary counted", [stored("a", { files: 2, paths: ["/w/a", "/w/b"], adds: 3, dels: 1, counted: true })]],
  ["stored summary uncounted", [stored("a", { files: 1, paths: ["/w/a"] })]],
  ["stored deletions", [stored("a", { deletions: true, files: 0 })]],
  ["idle live", [bash("a")], { live: true }],
  ["busy between steps", [bash("a")], { live: true, busy: true }],
  ["thinking", [bash("a"), THINKING], { live: true, busy: true }],
  ["running command", [bash("a"), running("b", "bash", `{"command":"make"}`)], { live: true, busy: true }],
  ["running edit after edits", [edit("a", "/p/a.go"), running("b", "edit", `{"file_path":"/p/b.go"}`)], { live: true, busy: true }],
  ["running after legacy edit", [legacyEdit("a", "/p/a.go"), running("b", "view", `{"file_path":"/p/b.go"}`)], { live: true, busy: true }],
  ["running mcp", [running("a", "mcp_github_search", `{}`)], { live: true, busy: true }],
  ["awaiting permission", [tool("a", "bash", `{"command":"rm"}`, null, "permission")], { live: true, busy: true }],
  ["failed while busy", [failed("a"), bash("b")], { live: true, busy: true }],
];

// The row in the terminal's words, to compare with its golden: its "N
// actions" for the desktop's "Ran N tools", ✓ where the desktop shows no
// mark (CHAT-081: "no mark when done").
const GO_MARKS = { activity: "●", cross: "×", gear: "⚙", null: "✓" };
function terminalText(row, status) {
  const count = row.count.replace(/^Ran (\d+) tools?$/, (_, n) => (n === "1" ? "1 action" : `${n} actions`));
  let line = `${GO_MARKS[row.mark]} ${count}`;
  if (row.summary) {
    const counts = row.summary.adds === null ? "" : `+${row.summary.adds} −${row.summary.dels}`;
    line += " · " + [row.summary.label, counts].filter(Boolean).join(" ");
  }
  for (const extra of row.extras) line += ` · ${extra.text}`;
  if (status) line += " | <activity>";
  return line;
}

function goldenRows() {
  const raw = readFileSync(new URL("../../../../internal/ui/chat/testdata/TestToolGroupHeaderStates.golden", import.meta.url), "utf8");
  const plain = raw.replace(/\x1b\[[0-9;:]*[A-Za-z]/g, "").replace(/\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)/g, "");
  const rows = new Map();
  for (const block of plain.split(/^(?==== )/m)) {
    const m = /^=== (.+) @120 cols=\[(\d+) (\d+)\] status="(.*)"\n(.*)\n/.exec(block);
    if (m) rows.set(m[1], { changes: m[2] !== "0", status: m[4], line: m[5].replace(/^[▸▾] /, "") });
  }
  return rows;
}

describe("group rows (Go golden)", () => {
  const golden = goldenRows();
  it("reads every case", () => {
    expect(golden.size).toBe(CASES.length);
  });
  for (const [name, steps, { live = false, busy = false } = {}] of CASES) {
    it(name, () => {
      const calls = steps.filter((s) => s !== THINKING);
      const lastStep = steps[steps.length - 1];
      const last =
        lastStep === THINKING
          ? { kind: "step", compacting: false, canceled: false, spinning: true, activity: "thinking" }
          : { kind: "tool", name: lastStep.name, status: toolStatus(lastStep) };
      const status = groupStatus({ live, busy, last });
      const row = groupRow({ groupKey: `golden-${nextGroup++}`, calls, status });
      const want = golden.get(name);
      expect(status).toBe(want.status);
      expect(terminalText(row, status)).toBe(want.line);
      expect(row.hasChanges).toBe(want.changes);
    });
  }
});

describe("group parts", () => {
  it("counts the tools (PLAN.md:147 wording)", () => {
    expect(countLabel(1)).toBe("Ran 1 tool");
    expect(countLabel(4)).toBe("Ran 4 tools");
  });
  it("lists backgrounded before failed, the failed count as an error (CHAT-082)", () => {
    expect(groupExtras({ moved: 1, failed: 2 })).toEqual([
      { text: "1 backgrounded", tone: "" },
      { text: "2 failed", tone: "error" },
    ]);
    expect(groupExtras({ moved: 0, failed: 0 })).toEqual([]);
  });
  it("marks a group by its last tool only (CHAT-081)", () => {
    expect(groupMark({ working: false, calls: [failed("a"), bash("b")] })).toBeNull();
    expect(groupMark({ working: false, calls: [bash("a"), failed("b")] })).toBe("cross");
    expect(groupMark({ working: false, calls: [moved("a")] })).toBe("gear");
    expect(groupMark({ working: true, calls: [failed("a")] })).toBe("activity");
    expect(groupMark({ working: false, calls: [tool("a", "view", "{}", null, "pending")] })).toBe("activity");
  });
  it("counts edited files once, skipping deletions and hidden paths", () => {
    const del = tool("d", "write", `{"file_path":"/p/x.go","delete_file":true}`, ok("ok"));
    const hidden = legacyEdit("h", "/p/.codex/settings.json");
    const none = tool("n", "edit", `{"file_path":"/p/y.go"}`, { content: "ok", review: { root: "/p", changes: [] } });
    expect(tally([legacyEdit("a", "/p/a.go"), legacyEdit("b", "/p/a.go"), del, hidden, none]).edited).toEqual(["a.go"]);
  });
});

describe("file change labels (file_change_label_test.go)", () => {
  it("names one file, counts several of a kind, calls a mix changed", () => {
    expect(fileChangeLabel({ edited: 1 }, "main.go")).toBe("edited main.go");
    expect(fileChangeLabel({ moved: 1 }, "notes.md")).toBe("moved notes.md");
    expect(fileChangeLabel({ removed: 1 }, "")).toBe("removed 1 file");
    expect(fileChangeLabel({ moved: 3 }, "")).toBe("moved 3 files");
    expect(fileChangeLabel({ edited: 4, copied: 0 }, "")).toBe("edited 4 files");
    expect(fileChangeLabel({ edited: 3, moved: 2, removed: 1 }, "")).toBe("6 files changed");
    expect(fileChangeLabel({}, "")).toBe("");
  });
  it("shortens a long name in the middle to 24 cells, keeping its end", () => {
    const name = fileChangeLabel({ edited: 1 }, "a_really_long_generated_file_name_handler.rs").replace(/^edited /, "");
    expect(cells(name)).toBeLessThanOrEqual(24);
    expect(name.endsWith(".rs")).toBe(true);
    expect(name).toContain("…");
    expect(shortFileName("short.go")).toBe("short.go");
    expect(shortFileName("a-very-long-file-name-for-the-summary.go")).toBe("a-very-long-fil…mmary.go");
  });
  it("reads saved summaries, which outlive their diffs", () => {
    expect(storedSummary([stored("a", { files: 1, paths: ["/w/a.go"], moved: 1 })]).label).toBe("moved a.go");
    expect(storedSummary([stored("a", { files: 3, paths: ["/w/a.go"], moved: 3 })]).label).toBe("moved 3 files");
    expect(storedSummary([stored("a", { files: 2, paths: ["/w/a", "/w/b"], removed: 2, deletions: true })])).toMatchObject({ label: "removed 2 files", deletions: true });
    expect(storedSummary([stored("a", { files: 1, paths: [], counted: true, adds: 2, dels: 0, no_lines: true })])).toMatchObject({ counted: false });
    // Without paths, the file a tool named stands in, unless it is hidden.
    const named = tool("a", "edit", `{"file_path":"/w/x.go"}`, { content: "ok", review: { id: "a", summary: { files: 1 } } });
    expect(storedSummary([named]).label).toBe("edited x.go");
    const hidden = tool("a", "edit", `{"file_path":"/w/.claude/x.md"}`, { content: "ok", review: { id: "a", summary: { files: 1 } } });
    expect(storedSummary([hidden]).label).toBe("");
  });
});

describe("hidden review paths (changes_exclusions_test.go)", () => {
  it("hides agent folders, dot folders and agent state", () => {
    for (const dir of [".agents", ".claude", ".codex", ".config", ".crush", ".gemini", ".grok", ".opencode", ".cursor", ".agy", ".antigravity"]) {
      expect(hiddenReviewPath(dir + "/settings.json", "/workspace"), dir).toBe(true);
      expect(hiddenReviewPath("/home/reviewer/" + dir + "/settings.json", ""), dir).toBe(true);
      expect(hiddenReviewPath(dir.slice(1) + "/main.go", "/workspace"), dir).toBe(false);
    }
    for (const path of [".claude-flow/metrics/learning.json", ".github/workflows/test.yml", "src/.vite/deps/a.js", "agentdb.rvf", "agentdb.rvf.lock", "skills-lock.json", "claude-flow.config.json"]) {
      expect(hiddenReviewPath(path, "/workspace"), path).toBe(true);
    }
    for (const path of [".gitignore", ".env", "AGENTS.md", "CLAUDE.md", ".claude/CLAUDE.md", "docs/agents.md", "internal/agent/agent.go", "src/agentdb/main.go"]) {
      expect(hiddenReviewPath(path, "/workspace"), path).toBe(false);
    }
    expect(hiddenReviewPath(".local/share/agent-memory/note.md", "/home/reviewer")).toBe(true);
    expect(hiddenReviewPath("/home/reviewer/.local/bin/pa-dev", "")).toBe(false);
    expect(hiddenReviewPath("/home/reviewer/.local/lib/tool.py", "")).toBe(true);
  });
  it("keeps project files and hides only the real device folders", () => {
    for (const path of ["/workspace/.env", "/workspace/.gitignore", "/workspace/config/settings.json", "/home/reviewer/.local/bin/helper", "/app-data-source/main.go", "/workspace/dev/tty.go"]) {
      expect(hiddenReviewPath(path, ""), path).toBe(false);
    }
    for (const path of ["/dev/tty", "/dev/pts/2", "/dev/shm/x", "/proc/self/oom_score_adj", "/sys/fs/cgroup/app.slice/cgroup.procs", "/tmp/x", "/var/tmp/y"]) {
      expect(hiddenReviewPath(path, ""), path).toBe(true);
    }
  });
  it("hides scratch folders", () => {
    for (const path of ["scratch/pa-tap/app/build.gradle.kts", "/home/reviewer/scratch/notes.md", "project/tmp/out.txt", "Temp/a.go", "src/Scratch/x.go"]) {
      expect(hiddenReviewPath(path, "/home/reviewer"), path).toBe(true);
    }
    for (const path of ["scratch.go", "src/scratchpad/main.go", "internal/temperature/temp.go", "tmp.txt"]) {
      expect(hiddenReviewPath(path, "/workspace"), path).toBe(false);
    }
  });
  it("leaves hidden files out of the summary and the changes", () => {
    const saved = tool("install", "bash", "", {
      content: "",
      review: { root: "/workspace", changes: [{ path: ".agents/skills/example/SKILL.md", after: { content: "skill\n" } }, { path: "main.go", after: { content: "package main\n" } }] },
    });
    const legacy = tool("edit", "edit", `{"file_path":"/home/reviewer/.codex/settings.json"}`, { content: "", metadata: `{"old_content":"old","new_content":"new"}` });
    const row = groupRow({ groupKey: `hide-${nextGroup++}`, calls: [saved, legacy] });
    expect(groupChanges(`hide-${nextGroup++}`, [saved, legacy]).map((f) => f.path)).toEqual(["main.go"]);
    expect(row.summary).toEqual({ label: "edited main.go", adds: 1, dels: 0 });
  });
  it("leaves test paths out of the summary and the Changes button", () => {
    const saved = tool("tests", "bash", "", {
      content: "",
      review: { root: "/workspace", changes: [{ path: "src/app_test.go", after: { content: "package app\n" } }, { path: "tests/run.py", after: { content: "print(1)\n" } }] },
    });
    expect(groupChanges(`tests-${nextGroup++}`, [saved])).toEqual([]);
    const row = groupRow({ groupKey: `tests-${nextGroup++}`, calls: [saved] });
    expect(row.hasChanges).toBe(false);
    expect(row.summary).toBe(null);
  });
});

describe("transfers (changes_transfers_test.go)", () => {
  it("shows a saved clone as checked out, without line counts", () => {
    const clone = tool("clone", "bash", `{"command":"git clone source destination"}`, {
      content: "",
      review: { root: "/workspace", changes: [{ path: "/workspace/destination/main.go", after: { content: "line\n".repeat(100_000) } }] },
    });
    const row = groupRow({ groupKey: `clone-${nextGroup++}`, calls: [clone] });
    expect(row.summary).toEqual({ label: "checked out main.go", adds: null, dels: null });
    expect(row.hasChanges).toBe(true);
    expect(clone.result.review.changes[0].transfer).toBeUndefined();
  });
  it("calls a single cp a copy", () => {
    const cp = tool("cp", "bash", `{"command":"cp a.txt b.txt"}`, { content: "", review: { root: "/w", changes: [{ path: "/w/b.txt", after: { content: "x\n" } }] } });
    expect(groupRow({ groupKey: `cp-${nextGroup++}`, calls: [cp] }).summary.label).toBe("copied b.txt");
  });
});

describe("verbs when nothing changed (CHAT-083, desktop-design 4.6)", () => {
  it("joins at most three verbs, then …", () => {
    expect(toolVerbs([tool("a", "view", `{"file_path":"/p/session.go"}`, ok("x")), bash("b")])).toBe("read session.go, ran a command");
    expect(toolVerbs([tool("a", "view", `{"file_path":"/p/a.go"}`, ok("x")), tool("b", "view", `{"file_path":"/p/b.go"}`, ok("x"))])).toBe("read 2 files");
    expect(toolVerbs([bash("a"), bash("b"), tool("c", "grep", "{}", ok("x")), tool("d", "glob", "{}", ok("x"))])).toBe("ran 2 commands, searched");
    expect(toolVerbs([tool("a", "fetch", `{"url":"https://example.com/x"}`, ok("x"))])).toBe("fetched example.com");
    expect(
      toolVerbs([
        tool("a", "web_search", "{}", ok("x")),
        tool("b", "todos", "{}", ok("x")),
        tool("c", "mcp_github_search", "{}", ok("x")),
        tool("d", "agent", "{}", ok("x")),
        tool("e", "lsp_references", "{}", ok("x")),
      ]),
    ).toBe("searched the web, updated the plan, used github, …");
    expect(toolVerbs([tool("a", "agent", "{}", ok("x")), tool("b", "lsp_restart", "{}", ok("x"))])).toBe("asked a helper, checked code");
  });
  it("shows verbs only when done and nothing changed", () => {
    const calls = [tool("a", "view", `{"file_path":"/p/session.go"}`, ok("x"))];
    expect(groupRow({ groupKey: `verbs-${nextGroup++}`, calls }).verbs).toBe("read session.go");
    expect(groupRow({ groupKey: `verbs-${nextGroup++}`, calls, status: "Thinking" }).verbs).toBe("");
    expect(groupRow({ groupKey: `verbs-${nextGroup++}`, calls: [edit("a", "/p/a.go")] }).verbs).toBe("");
  });
});
