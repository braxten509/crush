// Figures (CHAT-014, 1.5.6, 4.3): which blocks are visuals, their ids in
// source order, their titles, and that nothing past the stable boundary
// counts while a reply streams.
import { describe, expect, test } from "vitest";
import { extractFigures, pictureOf, visualBlocks } from "./figures.js";
import { lexMarkdown } from "./lex.js";
import { renderMarkdown } from "./markdown.js";

const TEXT = [
  "Intro with an inline ![first shot](./shots/one%20a.png) picture.",
  "",
  "| Property | Current | Fixed | Note |",
  "|---|---|---|---|",
  "| SameSite | Strict | Lax | x |",
  "",
  "- a list item with a diagram:",
  "",
  "  ```mermaid",
  "  ---",
  "  title: Sign-in flow",
  "  ---",
  "  flowchart LR",
  "    A[Sign in] --> B[Home]",
  "  ```",
  "",
  "> $$",
  "> E = mc^2",
  "> $$",
  "",
  "```math",
  "a^2 + b^2 = c^2",
  "```",
  "",
  "[![linked picture](x.png)](https://example.com) and ![bad](javascript:alert(1)) and ![web](https://cdn.example.com/p/cat.jpg)",
  "",
  "| a | ![in a table](t.png) |",
  "|---|---|",
].join("\n");

describe("visualBlocks", () => {
  test("tables, display math, Mermaid fences and pictures, in source order", () => {
    const visuals = visualBlocks(lexMarkdown(TEXT));
    expect(visuals.map((v) => [v.index, v.kind, v.title])).toEqual([
      [0, "picture", "first shot"],
      [1, "table", "Property · Current · Fixed"],
      [2, "diagram", "Sign-in flow"],
      [3, "math", "E = mc^2"],
      [4, "math", "a^2 + b^2 = c^2"],
      [5, "picture", "web"],
      [6, "table", "a · in a table"],
    ]);
  });

  test("pictures inside links, inside tables, or with a refused address are not visuals", () => {
    const visuals = visualBlocks(lexMarkdown(TEXT));
    expect(visuals.some((v) => v.title === "linked picture" || v.title === "bad")).toBe(false);
    expect(visuals.filter((v) => v.kind === "picture").map((v) => v.title)).toEqual(["first shot", "web"]);
  });
});

describe("extractFigures", () => {
  test("ids <message id>:<n>, the panel's fields", () => {
    const figures = extractFigures("s1", "m1", TEXT);
    expect(figures.map((f) => f.id)).toEqual(["m1:0", "m1:1", "m1:2", "m1:3", "m1:4", "m1:5", "m1:6"]);
    expect(figures[0]).toEqual({ id: "m1:0", sessionId: "s1", messageId: "m1", index: 0, kind: "picture", title: "first shot", source: "./shots/one%20a.png", src: "./shots/one%20a.png" });
    expect(figures[1].source).toBe("| Property | Current | Fixed | Note |\n|---|---|---|---|\n| SameSite | Strict | Lax | x |");
    expect(figures[2].source).toBe("---\ntitle: Sign-in flow\n---\nflowchart LR\n  A[Sign in] --> B[Home]");
    expect(figures[3].source).toBe("E = mc^2");
    expect(figures[5]).toMatchObject({ kind: "picture", src: "https://cdn.example.com/p/cat.jpg", host: "cdn.example.com" });
  });

  test("the chips in the chat carry the same ids, in the same order", () => {
    const root = renderMarkdown(TEXT, { messageId: "m1", sessionId: "s1" });
    const chips = [...root.querySelectorAll(".md-chip")].map((c) => [c.dataset.figure, c.dataset.kind]);
    expect(chips).toEqual(extractFigures("s1", "m1", TEXT).map((f) => [f.id, f.kind]));
  });

  test("while streaming, nothing past the stable boundary counts; an index never changes", () => {
    const text = "Intro.\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\nBetween.\n\n$$\nx\n$$\n\nAfter.\n\n```mermaid\nflowchart LR\nA-->B\n```\n\nEnd.\n";
    const seen = new Map(); // id → kind, the first time it appeared
    let previous = [];
    for (let at = 1; at <= text.length; at++) {
      const figures = extractFigures("s", "m", text.slice(0, at), { finished: false });
      // Figures only ever get added at the end; none changes or disappears.
      expect(figures.slice(0, previous.length).map((f) => f.id)).toEqual(previous.map((f) => f.id));
      for (const f of figures) {
        if (seen.has(f.id)) expect(seen.get(f.id)).toBe(f.kind);
        else seen.set(f.id, f.kind);
      }
      previous = figures;
    }
    expect([...seen.entries()]).toEqual([
      ["m:0", "table"],
      ["m:1", "math"],
      ["m:2", "diagram"],
    ]);
    // A table appears only once the text after it makes it stable.
    const justTable = "Intro.\n\n| a | b |\n|---|---|\n| 1 | 2 |";
    expect(extractFigures("s", "m", justTable, { finished: false })).toEqual([]);
    expect(extractFigures("s", "m", justTable, { finished: true }).map((f) => f.kind)).toEqual(["table"]);
  });

  test("the chat's chips get figure ids exactly when the panel's figures exist", () => {
    const text = "Intro.\n\n$$\nx\n$$\n\nNext paragraph\n\n$$\ny\n$$";
    for (let at = 1; at <= text.length; at++) {
      const prefix = text.slice(0, at);
      const ids = extractFigures("s", "m", prefix, { finished: false }).map((f) => f.id);
      const root = renderMarkdown(prefix, { messageId: "m", finished: false });
      const chipIds = [...root.querySelectorAll(".md-chip[data-figure]")].map((c) => c.dataset.figure);
      expect(chipIds, JSON.stringify(prefix)).toEqual(ids);
    }
  });

  test("a retry that rewrites earlier text replaces the figures", () => {
    expect(extractFigures("s", "m", "$$\na\n$$\n\n| t |\n|---|\n| 1 |").map((f) => f.kind)).toEqual(["math", "table"]);
    expect(extractFigures("s", "m", "| t |\n|---|\n| 1 |").map((f) => f.kind)).toEqual(["table"]);
  });

  test("plan markers and \\r\\n do not change the ids", () => {
    const plain = extractFigures("s", "m", "Plan\n\n$$\nx\n$$\n");
    const marked = extractFigures("s", "m", "<!-- CRUSH_PLAN_START -->\r\nPlan\r\n\r\n$$\r\nx\r\n$$\r\n<!-- CRUSH_PLAN_READY -->");
    expect(marked).toEqual(plain);
  });
});

describe("pictureOf", () => {
  test("which addresses can be pictures", () => {
    expect(pictureOf("data:image/png;base64,AAAA")).toMatchObject({ kind: "data" });
    expect(pictureOf("DATA:image/svg+xml,<svg/>")).toMatchObject({ kind: "data" });
    expect(pictureOf("blob:abc")).toMatchObject({ kind: "blob" });
    expect(pictureOf("https://example.com/a/b.png")).toMatchObject({ kind: "web", host: "example.com", name: "b.png" });
    expect(pictureOf("./shot%201.png")).toMatchObject({ kind: "file", src: "./shot%201.png", name: "shot 1.png" });
    expect(pictureOf("file:///tmp/x%20y.png")).toMatchObject({ kind: "file", src: "/tmp/x y.png", name: "x y.png" });
    expect(pictureOf("~/pics/a.png")).toMatchObject({ kind: "file", name: "a.png" });
    for (const refused of ["javascript:alert(1)", "data:text/html,x", "vbscript:x", "file://other-host/x.png", "", "   "]) {
      expect(pictureOf(refused)).toBeNull();
    }
  });
});
