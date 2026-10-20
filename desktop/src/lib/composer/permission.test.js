import { describe, expect, it } from "vitest";
import {
  BUTTONS,
  TITLE,
  contentOf,
  forcedFull,
  headerLines,
  hints,
  homeOf,
  plainDiff,
  prettyPath,
  promptKey,
  startsSplit,
  toolLabel,
} from "./permission.js";

const HOME = "/home/u";
const ask = (tool_name, params, extra = {}) => ({
  id: "p1",
  session_id: "s1",
  tool_call_id: "c1",
  tool_name,
  description: "",
  action: "execute",
  params,
  path: "/home/u/project",
  ...extra,
});

describe("the header (DLG-119, DLG-121, permissions.go:439-522)", () => {
  it("is titled Permission required", () => {
    expect(TITLE).toBe("Permission required");
  });

  it("names MCP tools as server → tool", () => {
    expect(toolLabel("mcp_my-server_read_file")).toBe("My Server → Read File");
    expect(toolLabel("mcp_docs_SEARCH")).toBe("Docs → Search");
    expect(toolLabel("bash")).toBe("bash");
    expect(toolLabel("mcp_alone")).toBe("mcp_alone");
  });

  it("shortens the home folder", () => {
    expect(prettyPath("/home/u/project/a.go", HOME)).toBe("~/project/a.go");
    expect(prettyPath("/home/u", HOME)).toBe("~");
    expect(prettyPath("/home/user2/x", HOME)).toBe("/home/user2/x");
    expect(prettyPath("/tmp/x", "")).toBe("/tmp/x");
  });

  it("gives a generic Path only where the tool has no file or folder line", () => {
    expect(headerLines(ask("bash", { description: "List files", command: "ls" }), HOME)).toEqual([
      { key: "Tool", value: "bash" },
      { key: "Path", value: "~/project" },
      { key: "Desc", value: "List files" },
    ]);
    expect(headerLines(ask("edit", { file_path: "/home/u/project/a.go" }), HOME)).toEqual([
      { key: "Tool", value: "edit" },
      { key: "File", value: "~/project/a.go" },
    ]);
    expect(headerLines(ask("download", { url: "https://x.dev/f", file_path: "/tmp/f" }), HOME)).toEqual([
      { key: "Tool", value: "download" },
      { key: "URL", value: "https://x.dev/f" },
      { key: "File", value: "/tmp/f" },
    ]);
    expect(headerLines(ask("ls", { path: "/home/u/project/src" }), HOME)).toEqual([
      { key: "Tool", value: "ls" },
      { key: "Directory", value: "~/project/src" },
    ]);
    expect(headerLines(ask("mcp_docs_search", { q: 1 }), HOME)).toEqual([
      { key: "Tool", value: "Docs → Search" },
      { key: "Path", value: "~/project" },
    ]);
  });
});

describe("the content per tool (DLG-122 to DLG-129, content.go)", () => {
  it("shell: the command without the display prefix, highlighted (DLG-122)", () => {
    const content = contentOf(ask("bash", { command: "cd /home/u/project && npm test" }), { workingDir: "/home/u/project", home: HOME });
    expect(content).toEqual({ kind: "text", parts: [{ text: "npm test", lang: "bash" }], text: "npm test" });
  });

  it("file writes: a diff (DLG-123)", () => {
    for (const tool of ["edit", "write", "multiedit", "lsp_replace_symbol"]) {
      expect(contentOf(ask(tool, { file_path: "/home/u/a.txt", old_content: "a\n", new_content: "b\n" }), { home: HOME })).toEqual({
        kind: "diff",
        file: { path: "~/a.txt", before: "a\n", after: "b\n" },
      });
    }
  });

  it("download: URL, file and the timeout when set (DLG-124)", () => {
    expect(contentOf(ask("download", { url: "https://x.dev/f", file_path: "/home/u/f" }), { home: HOME }).text).toBe(
      "URL: https://x.dev/f\nFile: ~/f",
    );
    expect(contentOf(ask("download", { url: "u", file_path: "/f", timeout: 30 })).text).toBe("URL: u\nFile: /f\nTimeout: 30s");
  });

  it("fetch: the URL (DLG-125)", () => {
    expect(contentOf(ask("fetch", { url: "https://x.dev", format: "text" })).text).toBe("https://x.dev");
  });

  it("agentic fetch: the prompt, and the URL when set (DLG-126)", () => {
    expect(contentOf(ask("agentic_fetch", { url: "https://x.dev", prompt: "sum up" })).text).toBe("URL: https://x.dev\n\nPrompt: sum up");
    expect(contentOf(ask("agentic_fetch", { prompt: "find it" })).text).toBe("Prompt: find it");
  });

  it("view: file, start line and a limit other than 2,000 (DLG-127)", () => {
    expect(contentOf(ask("view", { file_path: "/a.go" })).text).toBe("File: /a.go");
    expect(contentOf(ask("view", { file_path: "/a.go", offset: 9, limit: 2000 })).text).toBe("File: /a.go\nStarting from line: 10");
    expect(contentOf(ask("view", { file_path: "/a.go", offset: 0, limit: 50 })).text).toBe("File: /a.go\nLines to read: 50");
  });

  it("list: folder and ignore patterns (DLG-128)", () => {
    expect(contentOf(ask("ls", { path: "/src" })).text).toBe("Directory: /src");
    expect(contentOf(ask("ls", { path: "/src", ignore: ["*.o", "dist"] })).text).toBe("Directory: /src\nIgnore patterns: *.o, dist");
  });

  it("other tools: description and pretty JSON, MCP tools without the description (DLG-129)", () => {
    const other = contentOf(ask("sourcegraph", { query: "x", count: 2 }, { description: "Search code" }));
    expect(other.text).toBe('Search code\n\n{\n  "count": 2,\n  "query": "x"\n}');
    expect(other.parts[1].lang).toBe("json");
    expect(contentOf(ask("mcp_docs_search", '{"q":"hello"}', { description: "hidden" })).text).toBe('{\n  "q": "hello"\n}');
    expect(contentOf(ask("mcp_docs_search", "not json")).text).toBe("not json");
    expect(contentOf(ask("mcp_docs_search", null))).toBeNull();
  });
});

describe("keys (DLG-130 to DLG-138, permissions.go:225-293)", () => {
  const at = (selected, diff = false) => ({ selected, diff });

  it("starts on Allow and moves with ←/→, h/l and Tab, wrapping", () => {
    expect(BUTTONS.map((b) => b.label)).toEqual(["Allow", "Allow for session", "Deny"]);
    expect(promptKey("ArrowRight", at(0))).toEqual({ selected: 1 });
    expect(promptKey("Tab", at(2))).toEqual({ selected: 0 });
    expect(promptKey("l", at(1))).toEqual({ selected: 2 });
    expect(promptKey("ArrowLeft", at(0))).toEqual({ selected: 2 });
    expect(promptKey("h", at(2))).toEqual({ selected: 1 });
  });

  it("confirms the selected button with Enter or Ctrl+Y", () => {
    expect(promptKey("Enter", at(0))).toEqual({ answer: "allow" });
    expect(promptKey("Ctrl+Y", at(1))).toEqual({ answer: "allow_session" });
    expect(promptKey("Enter", at(2))).toEqual({ answer: "deny" });
  });

  it("answers with the letter keys; Esc denies", () => {
    for (const key of ["a", "A", "Ctrl+A"]) expect(promptKey(key, at(2))).toEqual({ answer: "allow" });
    for (const key of ["s", "S", "Ctrl+S"]) expect(promptKey(key, at(0))).toEqual({ answer: "allow_session" });
    for (const key of ["d", "D", "Escape"]) expect(promptKey(key, at(0))).toEqual({ answer: "deny" });
  });

  it("scrolls with Shift+arrows and J/K/H/L; page keys do nothing", () => {
    expect(promptKey("Shift+ArrowDown", at(0))).toEqual({ scrollY: 1 });
    expect(promptKey("K", at(0))).toEqual({ scrollY: -1 });
    expect(promptKey("L", at(0, true))).toEqual({ scrollX: 5 });
    expect(promptKey("Shift+ArrowLeft", at(0, true))).toEqual({ scrollX: -5 });
    expect(promptKey("PageDown", at(0))).toEqual({});
  });

  it("t and f act on diffs only", () => {
    expect(promptKey("t", at(0, true))).toEqual({ toggleSplit: true });
    expect(promptKey("t", at(0, false))).toEqual({});
    expect(promptKey("f", at(0, true))).toEqual({ toggleFull: true });
    expect(promptKey("f", at(0, false))).toEqual({});
    expect(promptKey("x", at(0))).toBeNull();
  });

  it("starts split from diff_mode, else from a 140-character box; fills the column when small", () => {
    expect(startsSplit("unified", 200)).toBe(false);
    expect(startsSplit("split", 60)).toBe(true);
    expect(startsSplit("", 140)).toBe(true);
    expect(startsSplit(undefined, 139)).toBe(false);
    expect(forcedFull(77, 40)).toBe(true);
    expect(forcedFull(78, 20)).toBe(true);
    expect(forcedFull(78, 21)).toBe(false);
  });

  it("shows only the hints that apply", () => {
    expect(hints({ diff: false, scrollable: false })).toBe("a allow · s allow for session · d deny");
    expect(hints({ diff: true, scrollable: true })).toBe("a allow · s allow for session · d deny · shift+↑↓ scroll · t split/unified · f full screen");
  });
});

describe("the home folder and the plain diff", () => {
  it("finds the home folder in the project's path", () => {
    expect(homeOf("/home/u/project")).toBe("/home/u");
    expect(homeOf("/Users/ana/src/app")).toBe("/Users/ana");
    expect(homeOf("/root")).toBe("/root");
    expect(homeOf("/srv/project")).toBe("");
    expect(homeOf("/homestead/x")).toBe("");
    expect(prettyPath("/home/u/project/a.go", homeOf("/home/u/project"))).toBe("~/project/a.go");
  });

  it("lists the changed lines between the unchanged start and end", () => {
    expect(plainDiff({ before: "a\nb\nc\n", after: "a\nB\nc\n" })).toBe("- b\n+ B");
    expect(plainDiff({ before: "", after: "x\r\ny" })).toBe("+ x\n+ y");
    expect(plainDiff({ before: "same", after: "same" })).toBe("");
    expect(plainDiff({ before: "a\nb", after: "a\nb\nc" })).toBe("+ c");
  });
});
