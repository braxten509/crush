// The file changes behind the group summary and the Changes sheet (ports of
// diffreview/diffreview_test.go and diffreview/transfers_test.go).
import { describe, expect, it } from "vitest";
import {
  ADD,
  ADDED,
  anyCounted,
  buildReview,
  buildReviewChanges,
  commandTransferKind,
  CONTEXT,
  COPIED,
  counted,
  DEL,
  DELETED,
  fileModeString,
  hasEdits,
  HUNK,
  kindName,
  MODIFIED,
  MOVED,
  removedCount,
  reviewSummary,
  stats,
} from "./review.js";

const line = (kind, old, nw, text) => ({ kind, old, new: nw, text });
const state = (text, fields = {}) => ({ content: text, size: text.length, mode: 0o644, ...fields });
const snap = (path, before, after, fields = {}) => ({ path, snapshot: { path, before, after, ...fields } });

describe("Build (diffreview_test.go)", () => {
  it("combines whole-file edits of one file, with real line numbers", () => {
    const v1 = "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\n";
    const v2 = "a\nB\nc\nd\ne\nf\ng\nh\ni\nj\n";
    const v3 = "a\nB\nc\nd\ne\nf\ng\nh\ni\nJ\n";
    const files = buildReview([
      { path: "/p/x.txt", before: v1, after: v2, full: true },
      { path: "/p/new.txt", before: "", after: "one\ntwo\n", full: true },
      { path: "/p/x.txt", before: v2, after: v3, full: true },
    ]);
    expect(files).toHaveLength(2);
    const [x, n] = files;
    expect(x).toMatchObject({ path: "/p/x.txt", kind: MODIFIED, adds: 2, dels: 2 });
    expect(x.lines[0]).toEqual(line(HUNK, 0, 0, "@@ -1,5 +1,5 @@"));
    expect(x.lines[2]).toEqual(line(DEL, 2, 0, "b"));
    expect(x.lines[3]).toEqual(line(ADD, 0, 2, "B"));
    expect(x.lines.at(-1)).toEqual(line(ADD, 0, 10, "J"));
    expect(n.kind).toBe(ADDED);
    expect(n.adds).toBe(2);
    expect(n.lines[1]).toEqual(line(ADD, 0, 1, "one"));
    expect(stats(files)).toEqual({ adds: 4, dels: 2 });
  });
  it("tells empty from absent and keeps only net changes", () => {
    const files = buildReview([
      snap("emptied", state("text\n"), state("")),
      snap("filled", state(""), state("text\n")),
      snap("new-empty", null, state("")),
      snap("gone-empty", state(""), null),
      snap("temporary", null, state("temporary")),
      snap("temporary", state("temporary"), null),
      snap("reverted", state("old"), state("new")),
      snap("reverted", state("new"), state("old")),
      snap("twice", state("first\n"), state("second\n")),
      snap("twice", state("second\n"), state("last\n")),
    ]);
    expect(files).toHaveLength(5);
    expect(files[0].kind).toBe(MODIFIED);
    expect(files[1].kind).toBe(MODIFIED);
    expect(files[2].kind).toBe(ADDED);
    expect(files[2].lines[0].text).toBe("Added empty file");
    expect(files[3].kind).toBe(DELETED);
    expect(files[3].lines[0].text).toBe("Deleted empty file");
    expect(files[4]).toMatchObject({ adds: 1, dels: 1 });
    expect(files[4].lines[1].text).toBe("first");
    expect(files[4].lines[2].text).toBe("last");
  });
  it("notes binary and mode changes without counts", () => {
    const files = buildReview([
      snap("image", { digest: "old", size: 50, mode: 0o644, omitted: "Binary file" }, { digest: "new", size: 50, mode: 0o644, omitted: "Binary file" }),
      snap("script", { content: "script", mode: 0o644 }, { content: "script", mode: 0o755 }),
    ]);
    expect(files).toHaveLength(2);
    expect(files[0].lines[0].text).toBe("Binary file; 50 → 50 bytes");
    expect(files[1].lines[0].text).toContain("-rw-r--r-- → -rwxr-xr-x");
    expect(stats(files)).toEqual({ adds: 0, dels: 0 });
  });
  it("orders parallel snapshots by when they were taken", () => {
    const files = buildReview([snap("file", state("middle\n"), state("last\n"), { order: 2 }), snap("file", state("first\n"), state("middle\n"), { order: 1 })]);
    expect(files).toHaveLength(1);
    expect(files[0].lines[1].text).toBe("first");
    expect(files[0].lines[2].text).toBe("last");
  });
  it("shows snippet edits on their own, without numbers", () => {
    const files = buildReview([
      { path: "/p/x.go", before: "return 5", after: "return 500" },
      { path: "/p/x.go", before: "same", after: "same" },
    ]);
    expect(files).toHaveLength(1);
    expect(files[0].lines).toEqual([line(DEL, 0, 0, "return 5"), line(ADD, 0, 0, "return 500")]);
  });
  it("keeps a unified diff's hunks and numbers", () => {
    const diff = "--- a/p/x.go\n+++ b/p/x.go\n@@ -10,3 +10,3 @@\n ctx\n-old\n+new\n ctx2\n\\ No newline at end of file\n";
    expect(buildReview([{ path: "/p/x.go", unified: diff }])[0].lines).toEqual([
      line(HUNK, 0, 0, "@@ -10,3 +10,3 @@"),
      line(CONTEXT, 10, 10, "ctx"),
      line(DEL, 11, 0, "old"),
      line(ADD, 0, 11, "new"),
      line(CONTEXT, 12, 12, "ctx2"),
    ]);
    expect(buildReview([{ path: "/p/n.go", unified: "--- /dev/null\n+++ b/p/n.go\n@@ -0,0 +1 @@\n+hi\n" }])[0].kind).toBe(ADDED);
  });
  it("ignores special files whose timestamps moved", () => {
    const tty = (stamp) => ({ digest: "stat:0:" + stamp, mode: 0o20620, omitted: "Special file (Dc)" });
    expect(buildReview([snap("/dev/tty", tty("1"), tty("2"))])).toEqual([]);
  });
  it("gives removed and uncompared files no line counts", () => {
    const big = (digest, size) => ({ digest, size, mode: 0o755, omitted: "File exceeds 1048576 byte text preview limit" });
    const files = buildReview([snap("/w/crush-readonly.new", big("new", 136414586), null), snap("/w/crush", big("old", 136422753), big("new", 136414586))]);
    expect(files).toHaveLength(2);
    expect(files[0].kind).toBe(DELETED);
    expect(hasEdits(files[0])).toBe(false);
    expect(counted(files[1])).toBe(false);
    expect(anyCounted(files)).toBe(false);
    expect(removedCount(files)).toBe(1);
    expect(reviewSummary(files)).toBe("removed 1 file · 1 file edited");
    const text = buildReview([snap("/w/a.go", { content: "one\ntwo\n", mode: 0o644 }, null), snap("/w/b.go", { content: "x\n", mode: 0o644 }, { content: "y\n", mode: 0o644 })]);
    expect(stats(text)).toEqual({ adds: 1, dels: 1 });
    expect(reviewSummary(text)).toBe("removed 1 file · 1 file edited");
  });
});

describe("transfers (transfers_test.go)", () => {
  it("counts only edits made after an import", () => {
    const base = { content: "one\ntwo\nthree\n", mode: 0o644 };
    const edited = { content: "one\nchanged\nthree\n", mode: 0o644 };
    const imported = { path: "checkout/file", order: 1, after: base, transfer: { kind: "checkout", baseline: base } };
    let files = buildReview([{ path: imported.path, snapshot: imported }]);
    expect(files).toHaveLength(1);
    expect(files[0]).toMatchObject({ kind: COPIED, adds: 0 });
    expect(hasEdits(files[0])).toBe(false);
    expect(files[0].lines).toHaveLength(1);
    expect(reviewSummary(files)).toBe("copied checkout: 1 file");
    const edit = { path: imported.path, order: 2, before: base, after: edited };
    files = buildReview([
      { path: edit.path, snapshot: edit },
      { path: imported.path, snapshot: imported },
    ]);
    expect(files[0]).toMatchObject({ adds: 1, dels: 1 });
    expect(reviewSummary(files)).toBe("copied checkout: 1 file · 1 file edited");
  });
  it("keeps a moved file's edits without counting its source as removed", () => {
    const base = { content: "before\n", mode: 0o644 };
    const after = { content: "after\n", mode: 0o644 };
    const files = buildReview([
      { path: "source", snapshot: { path: "source", before: base } },
      { path: "destination", snapshot: { path: "destination", after, transfer: { kind: "move", source: "source", baseline: base } } },
    ]);
    expect(files).toHaveLength(1);
    expect(files[0]).toMatchObject({ kind: MOVED, adds: 1, dels: 1 });
    expect(files[0].lines[0].text).toBe("Moved from source");
  });
  it("reads a single literal cp or git clone in an older saved command", () => {
    expect(commandTransferKind("cp a b")).toBe("copy");
    expect(commandTransferKind("/bin/cp -r 'a dir' \"b\"")).toBe("copy");
    expect(commandTransferKind("git clone https://x/y.git dest")).toBe("checkout");
    expect(commandTransferKind("git -C /w clone x")).toBe("checkout");
    expect(commandTransferKind("git status")).toBe("");
    for (const other of ["cp a b && rm a", "cp $A b", "cp a b; cp c d", "X=1 cp a b", "cp a b > log", "echo hi"]) {
      expect(commandTransferKind(other), other).toBe("");
    }
  });
});

describe("review inputs (chat/changes.go)", () => {
  it("skips saved summaries, errors and hidden files, and notes omitted ones", () => {
    const files = buildReviewChanges([
      { call: { name: "bash", input: "{}" }, result: { review: { id: "r", summary: { files: 1 } } } },
      { call: { name: "edit", input: `{"file_path":"/w/a.go"}` }, result: { is_error: true, metadata: `{"old_content":"a","new_content":"b"}` } },
      { call: { name: "edit", input: `{"file_path":"/w/.claude/x.md"}` }, result: { metadata: `{"old_content":"a","new_content":"b","additions":1,"removals":1}` } },
      { call: { name: "write", input: `{"file_path":"/w/big.bin"}` }, result: { metadata: `{"review_omitted":"Binary file"}` } },
      { call: { name: "edit", input: `{"file_path":"/w/b.go","old_string":"x","new_string":"y"}` }, result: { metadata: `{"old_content":"x\\n","new_content":"y\\n","additions":1,"removals":1}` } },
    ]);
    expect(files.map((f) => f.path)).toEqual(["/w/b.go", "/w/big.bin"]);
    expect(files[1].lines[0].text).toBe("Binary file");
  });
  it("names the kinds", () => {
    expect([MODIFIED, ADDED, DELETED, COPIED, MOVED, 5].map(kindName)).toEqual(["Changed", "Added", "Deleted", "Copied", "Moved", "Generated"]);
    expect(fileModeString(0o755)).toBe("-rwxr-xr-x");
  });
});
