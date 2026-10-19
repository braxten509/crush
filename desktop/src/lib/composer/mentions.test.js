import { describe, expect, it } from "vitest";
import { fuzzyFind } from "./fuzzy.js";
import {
  absolutePath,
  fileRows,
  insertPick,
  mentionQuery,
  mentionSpan,
  moveFocus,
  opensAt,
  pickFile,
  pickResource,
  popupColumns,
  previewPick,
  rankRows,
  resourceRows,
  tierOf,
  wordAt,
} from "./mentions.js";
import { NO_PICTURES } from "./paste.js";

const FILES = [
  { path: ".gitignore", is_dir: false },
  { path: "README.md", is_dir: false },
  { path: "docs", is_dir: true },
  { path: "docs/guide.md", is_dir: false },
  { path: "src", is_dir: true },
  { path: "src/area.js", is_dir: false },
  { path: "src/reader.js", is_dir: false },
];

describe("opening and following (TYPE-026, mention.go:25-62)", () => {
  it("opens on @ at the start or after a space", () => {
    expect(opensAt("@", 0)).toBe(true);
    expect(opensAt("see @", 4)).toBe(true);
    expect(opensAt("line\n@", 5)).toBe(true);
    expect(opensAt("mail@", 4)).toBe(false);
    expect(opensAt("see x", 4)).toBe(false);
  });

  it("filters on the word after the @", () => {
    expect(mentionQuery("see @rea", 8, 4)).toBe("rea");
    expect(mentionQuery("see @", 5, 4)).toBe("");
    expect(mentionQuery("see @rea more", 6, 4)).toBe("rea");
  });

  it("closes on a space, before the @, or on another word", () => {
    expect(mentionQuery("see @rea ", 9, 4)).toBeNull();
    expect(mentionQuery("see @rea", 4, 4)).toBeNull();
    expect(mentionQuery("see @rea", 2, 4)).toBeNull();
    expect(mentionQuery("see rea", 7, 4)).toBeNull();
    expect(mentionQuery("@a @b", 5, 0)).toBeNull();
  });

  it("knows the word and the span", () => {
    expect(wordAt("see @rea now", 6)).toEqual({ start: 4, end: 8, text: "@rea" });
    expect(mentionSpan("see @rea now", 4)).toEqual({ start: 4, end: 8 });
  });
});

describe("ranking (TYPE-026, completions.go:225-272)", () => {
  const texts = (ranked) => ranked.map(({ row }) => row.text);

  it("lists folders with a slash and keeps the order for an empty query", () => {
    const rows = fileRows(FILES);
    expect(texts(rankRows(rows, ""))).toEqual([".gitignore", "README.md", "docs/", "docs/guide.md", "src/", "src/area.js", "src/reader.js"]);
  });

  it("puts README.md first for rea", () => {
    const ranked = rankRows(fileRows(FILES), "rea");
    expect(texts(ranked)[0]).toBe("README.md");
    expect(texts(ranked)).toEqual(["README.md", "src/reader.js", "src/area.js"]);
    expect(ranked[0].chars).toEqual([0, 1, 2]);
  });

  it("ranks exact name, then name prefix, then path segment, then the rest", () => {
    expect(tierOf("README.md", "readme")).toBe(0);
    expect(tierOf("docs/guide.md", "guide.md")).toBe(0);
    expect(tierOf("src/reader.js", "rea")).toBe(1);
    expect(tierOf("docs/guide.md", "docs")).toBe(2);
    expect(tierOf("src/area.js", "rea")).toBe(3);
    const ranked = rankRows(fileRows([{ path: "docs/xguide.md" }, { path: "guides/x.md" }, { path: "guide.md" }]), "guide");
    expect(texts(ranked)).toEqual(["guide.md", "guides/x.md", "docs/xguide.md"]);
  });

  it("scores as sahilm/fuzzy does", () => {
    const [match] = fuzzyFind("tk", ["The Black Knight"]);
    expect(match).toMatchObject({ score: 16, chars: [0, 10] });
    expect(fuzzyFind("zz", ["abc"])).toEqual([]);
    expect(fuzzyFind("", ["abc"])).toEqual([]);
  });

  it("sizes the popup between 10 and 100 characters", () => {
    expect(popupColumns(rankRows(fileRows([{ path: "a.md" }]), ""))).toBe(10);
    expect(popupColumns(rankRows(fileRows([{ path: "x".repeat(30) }]), ""))).toBe(32);
    expect(popupColumns(rankRows(fileRows([{ path: "x".repeat(300) }]), ""))).toBe(100);
  });
});

describe("keys (TYPE-028, selection.go:38-122)", () => {
  it("moves up from the best row at the bottom, wrapping both ways", () => {
    expect(moveFocus(0, 3, "up")).toBe(1);
    expect(moveFocus(2, 3, "up")).toBe(0);
    expect(moveFocus(0, 3, "down")).toBe(2);
    expect(moveFocus(2, 3, "down")).toBe(1);
    expect(moveFocus(0, 0, "down")).toBe(0);
  });
});

describe("what a pick puts in the field (TYPE-029)", () => {
  it("puts the path and a space in place of @query, the caret after it", () => {
    expect(insertPick("see @rea", { start: 4, end: 8 }, "README.md")).toEqual({ value: "see README.md ", caret: 14 });
    expect(insertPick("see @rea now", { start: 4, end: 8 }, "README.md")).toEqual({ value: "see README.md now", caret: 14 });
  });

  it("shows Ctrl+N's row in place without a space", () => {
    expect(previewPick("see @rea now", { start: 4, end: 8 }, "src/reader.js")).toEqual({
      value: "see src/reader.js now",
      span: { start: 4, end: 17 },
    });
  });
});

describe("what a pick attaches (TYPE-029, editor.go:294-368)", () => {
  const READ_AT = "2026-10-18T10:00:00-06:00";
  const readMs = Date.parse(READ_AT);
  const files = {
    "README.md": { exists: true, is_dir: false, size: 5, modified: readMs - 60_000 },
    "src/reader.js": { exists: true, is_dir: false, size: 5, modified: readMs + 60_000 },
  };
  const stat = async (path) => files[path] ?? { exists: false };
  const read = async (path) => {
    if (!files[path]) throw { code: "bad_path", message: "no" };
    return { mime: "text/plain; charset=utf-8", base64: "aGk=" };
  };
  // Answers only for the chat's id and the absolute path, as the engine's
  // getFileLastRead reads both (review finding 7).
  const lastRead = async (sid, path) => (sid === "chat-1" && path.startsWith("/p/") ? READ_AT : "0001-01-01T00:00:00Z");
  const base = { absolute: "", supportsImages: false, stat, read, lastRead };

  it("skips a file the chat read and that has not changed since", async () => {
    const reads = [];
    expect(await pickFile("README.md", { ...base, sessionId: "chat-1", absolute: "/p/README.md", reads })).toEqual({ skip: true });
  });

  it("attaches a file changed since the read, or one the chat never read", async () => {
    const reads = [];
    const changed = await pickFile("src/reader.js", { ...base, sessionId: "chat-1", absolute: "/p/src/reader.js", reads });
    expect(changed.attachment).toEqual({ kind: "text", name: "reader.js", mime: "text/plain; charset=utf-8", path: "src/reader.js", base64: "aGk=" });
    const other = await pickFile("README.md", { ...base, sessionId: "chat-2", absolute: "/p/README.md", reads });
    expect(other.attachment?.name).toBe("README.md");
  });

  it("before a chat exists only de-duplicates", async () => {
    const reads = [];
    const first = await pickFile("README.md", { ...base, sessionId: "", absolute: "/p/README.md", reads });
    expect(first.attachment?.name).toBe("README.md");
    expect(await pickFile("README.md", { ...base, sessionId: "", absolute: "/p/README.md", reads })).toEqual({ skip: true });
    expect(reads).toEqual(["/p/README.md"]);
  });

  it("warns for a picture on a text-only model and leaves unreadable files to the AI", async () => {
    expect(await pickFile("shot.png", { ...base, sessionId: "", absolute: "/p/shot.png", reads: [] })).toEqual({ warn: NO_PICTURES });
    expect(await pickFile("gone.txt", { ...base, sessionId: "", absolute: "/p/gone.txt", reads: [] })).toEqual({ skip: true });
  });

  it("makes absolute paths against the project folder", () => {
    expect(absolutePath("/p/", "./src/a.js")).toBe("/p/src/a.js");
    expect(absolutePath("/p", "/etc/hosts")).toBe("/etc/hosts");
  });
});

describe("MCP resources (TYPE-030, editor.go:370-428)", () => {
  const [row] = resourceRows("docs", [{ uri: "file:///guide", name: "Guide" }]);

  it("are listed as server/title", () => {
    expect(row.text).toBe("docs/Guide");
  });

  it("attach their first content, text/plain when no type is given", async () => {
    const readResource = async () => [{ uri: "file:///guide", text: "hello" }];
    expect(await pickResource(row, { supportsImages: true, readResource })).toEqual({
      attachment: { kind: "text", name: "Guide", mime: "text/plain", path: "file:///guide", base64: "aGVsbG8=" },
    });
    expect(await pickResource(row, { supportsImages: true, readResource: async () => [{ text: "" }] })).toEqual({ skip: true });
    const picture = { ...row, mime: "image/png" };
    expect(await pickResource(picture, { supportsImages: false, readResource: async () => [{ blob: "iVBO" }] })).toEqual({ warn: NO_PICTURES });
  });
});
