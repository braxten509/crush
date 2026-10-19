// CHAT-059: what a generic result is (ports of tool_result_content.go,
// unified_diff.go and diff/detect.go, with the cases of mcp_test.go and
// tool_result_content_test.go).
import { describe, expect, it } from "vitest";
import { indentJSON, isUnifiedDiff, looksLikeMarkdown, parseUnifiedDiff, sniffBody, sniffKind, unifiedBody } from "./sniff.js";

// mcp_test.go:11-133 (TestLooksLikeDiff).
const DIFF_CASES = [
  ["simple unified diff", 'diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1,5 +1,6 @@\n package main\n \n+import "fmt"\n+\n func main() {\n-    println("hello")\n+    fmt.Println("hello")\n }\n', true],
  ["plain text", "This is just some plain text with no diff markers.", false],
  ["empty string", "", false],
  ["markdown with headers", "# Title\n\nSome content here.\n\n## Subtitle\n\nMore content with **bold** text.\n", false],
  ["diff with mixed content", "diff --git a/file.txt b/file.txt\n--- a/file.txt\n+++ b/file.txt\n@@ -1 +1 @@\n-old line\n+new line\n", true],
  ["only plus/minus", "Hello world\n---\nThis is not really a diff\nJust some text with a few symbols\n+ another line\nMore regular content here\nAnd even more content\n", false],
  ["GitHub PR diff", "diff --git a/src/app.ts b/src/app.ts\nindex abc1234..def5678 100644\n--- a/src/app.ts\n+++ b/src/app.ts\n@@ -10,6 +10,8 @@ function handleRequest() {\n   const data = getData();\n+  validate(data);\n+  log(data);\n   return process(data);\n }\n", true],
  ["non-git patch", "--- a/old.c\n+++ b/old.c\n@@ -1,3 +1,4 @@\n #include <stdio.h>\n-int main() {\n+int main(int argc, char **argv) {\n     return 0;\n }\n", true],
  ["headers without hunks", "--- a/somefile.txt\n+++ b/somefile.txt\nJust some content here\nNo hunk markers at all\n", false],
  ["hunks without headers", "@@ -1,3 +1,4 @@\n some line\n-another line\n+changed line\n", false],
  ["markdown list with plus signs", "- Item one\n- Item two\n+ Bonus item\n- Item three\n", false],
];

// mcp_test.go:149-362 (TestParseUnifiedDiff).
const PARSE_CASES = [
  [
    "simple diff",
    'diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1,5 +1,6 @@\n package main\n \n+import "fmt"\n+\n func main() {\n-    println("hello")\n+    fmt.Println("hello")\n }\n',
    [{ path: "main.go", before: 'package main\n\nfunc main() {\n    println("hello")\n}', after: 'package main\n\nimport "fmt"\n\nfunc main() {\n    fmt.Println("hello")\n}' }],
  ],
  ["new file", "diff --git a/newfile.go b/newfile.go\nnew file mode 100644\n--- /dev/null\n+++ b/newfile.go\n@@ -0,0 +1,3 @@\n+package main\n+\n+func main() {}\n", [{ path: "newfile.go", before: "", after: "package main\n\nfunc main() {}" }]],
  ["deleted file", "diff --git a/oldfile.go b/oldfile.go\ndeleted file mode 100644\n--- a/oldfile.go\n+++ /dev/null\n@@ -1,3 +0,0 @@\n-package main\n-\n-func main() {}\n", [{ path: "oldfile.go", before: "package main\n\nfunc main() {}", after: "" }]],
  ["not a diff", "Just some regular text", []],
  ["timestamps", "diff --git a/config.yml b/config.yml\n--- a/config.yml\t2024-01-15 10:30:00\n+++ b/config.yml\t2024-01-15 10:31:00\n@@ -1,3 +1,4 @@\n name: myapp\n-version: 1.0\n+version: 1.1\n+debug: true\n", [{ path: "config.yml", before: "name: myapp\nversion: 1.0", after: "name: myapp\nversion: 1.1\ndebug: true" }]],
  [
    "two files",
    "diff --git a/one.txt b/one.txt\n--- a/one.txt\n+++ b/one.txt\n@@ -1,3 +1,3 @@\n line one\n-line two\n+line two updated\n line three\ndiff --git a/two.txt b/two.txt\n--- a/two.txt\n+++ b/two.txt\n@@ -1,2 +1,3 @@\n alpha\n+beta\n gamma\n",
    [
      { path: "one.txt", before: "line one\nline two\nline three", after: "line one\nline two updated\nline three" },
      { path: "two.txt", before: "alpha\ngamma", after: "alpha\nbeta\ngamma" },
    ],
  ],
  ["non-git patch", "--- old.c\n+++ old.c\n@@ -1,3 +1,4 @@\n #include <stdio.h>\n-int main() {\n+int main(int argc, char **argv) {\n     return 0;\n }\n", [{ path: "old.c", before: "#include <stdio.h>\nint main() {\n    return 0;\n}", after: "#include <stdio.h>\nint main(int argc, char **argv) {\n    return 0;\n}" }]],
  ["non-git new file", "--- /dev/null\n+++ newfile.txt\n@@ -0,0 +1,2 @@\n+hello\n+world\n", [{ path: "newfile.txt", before: "", after: "hello\nworld" }]],
  ["only +++ header", "+++ brand_new.go\n@@ -0,0 +1,3 @@\n+package main\n+\n+func main() {}\n", [{ path: "brand_new.go", before: "", after: "package main\n\nfunc main() {}" }]],
  [
    "two hunks",
    'diff --git a/big.go b/big.go\n--- a/big.go\n+++ b/big.go\n@@ -1,4 +1,5 @@\n package main\n+import "os"\n \n func init() {\n@@ -10,3 +11,3 @@\n-    println("done")\n+    fmt.Println("done")\n }\n',
    [{ path: "big.go", before: 'package main\n\nfunc init() {\n    println("done")\n}', after: 'package main\nimport "os"\n\nfunc init() {\n    fmt.Println("done")\n}' }],
  ],
  ["header-like lines", "diff --git a/file.txt b/file.txt\n--- a/file.txt\n+++ b/file.txt\n@@ -1,3 +1,3 @@\n---- tricky\n++++ newer\n keep\n", [{ path: "file.txt", before: "--- tricky\nkeep", after: "+++ newer\nkeep" }]],
];

describe("unified diffs", () => {
  for (const [name, content, want] of DIFF_CASES) {
    it(`detects: ${name}`, () => expect(isUnifiedDiff(content)).toBe(want));
  }
  for (const [name, content, want] of PARSE_CASES) {
    it(`parses: ${name}`, () => expect(parseUnifiedDiff(content)).toEqual(want));
  }
  it("is a diff, not Markdown (TestLooksLikeDiffVersusMarkdown)", () => {
    const content = ["diff --git a/README.md b/README.md", "--- a/README.md", "+++ b/README.md", "@@ -1,3 +1,3 @@", " # Title", "-Old subtitle", "+New subtitle", " Some content"].join("\n");
    expect(sniffKind(content)).toBe("diff");
  });
  it("shows a diff without files as diff code", () => {
    expect(unifiedBody("@@ -1 +1 @@\n-a\n+b\n")).toEqual({ type: "code", path: "result.diff", text: "@@ -1 +1 @@\n-a\n+b\n", offset: 0 });
    // A file header alone is still a (blank) file, as in the terminal.
    expect(unifiedBody("--- a\n+++ b\n")).toEqual({ type: "diff", files: [{ path: "a", before: "", after: "" }] });
  });
});

describe("JSON (json.MarshalIndent of the raw value)", () => {
  it("indents objects and arrays, keeping key order and number text", () => {
    expect(indentJSON('{"b":1,"a":[1,2.50,{"c":null}],"e":{},"f":[]}')).toBe('{\n  "b": 1,\n  "a": [\n    1,\n    2.50,\n    {\n      "c": null\n    }\n  ],\n  "e": {},\n  "f": []\n}');
    expect(indentJSON("  42  ")).toBe("42");
    expect(indentJSON('"x"')).toBe('"x"');
    expect(indentJSON("true")).toBe("true");
  });
  it("escapes HTML characters and line separators in strings", () => {
    expect(indentJSON('{"h":"<a>& "}')).toBe('{\n  "h": "\\u003ca\\u003e\\u0026\\u2028"\n}');
    expect(indentJSON('{"k":"\\u0041\\n"}')).toBe('{\n  "k": "\\u0041\\n"\n}');
  });
  it("refuses what is not one JSON value", () => {
    for (const bad of ["", "{", '{"a":1,}', "[1 2]", "01", "1.", "-", "tru", '"\n"', '{"a" 1}', "1 2", "{} x", "nul", "+1", ".5"]) {
      expect(indentJSON(bad), JSON.stringify(bad)).toBeNull();
    }
    expect(indentJSON("[".repeat(20000) + "]".repeat(20000))).toBeNull();
  });
});

describe("result kinds", () => {
  it("re-indents JSON, draws diffs, shows Markdown as its source, and the rest as text", () => {
    expect(sniffBody('{"a":1}')).toEqual({ type: "code", path: "result.json", text: '{\n  "a": 1\n}', offset: 0 });
    expect(sniffBody("# Title\n\ntext")).toEqual({ type: "code", path: "result.md", text: "# Title\n\ntext", offset: 0 });
    expect(sniffBody("hello")).toEqual({ type: "plain", text: "hello" });
    expect(sniffKind("--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n")).toBe("diff");
    expect(sniffKind("[1, 2]")).toBe("json");
    expect(sniffKind("plain")).toBe("plain");
  });
  it("knows Markdown by its marks", () => {
    for (const s of ["# h", "## h", "**b**", "```", "- a", "1. a", "> q", "---", "***"]) expect(looksLikeMarkdown(s), s).toBe(true);
    expect(looksLikeMarkdown("plain words")).toBe(false);
  });
});
