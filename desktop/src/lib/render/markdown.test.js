// CHAT-014, CHAT-017, CHAT-033 and 1.7 items 1-4: the DOM shape of every
// block type, built from tokens with createElement and text nodes only.
import { describe, expect, test } from "vitest";
import { PLAN_READY_MARKER, PLAN_START_MARKER } from "./plan.js";
import { renderMarkdown } from "./markdown.js";

const render = (text, options) => renderMarkdown(text, { messageId: "m1", sessionId: "s1", ...options });

// The renderer's own elements and attributes (1.7 item 1). KaTeX's output (in
// .md-math) is cleaned by DOMPurify and checked in math.test.js.
const ELEMENTS = new Set(["P", "H1", "H2", "H3", "H4", "H5", "H6", "UL", "OL", "LI", "BLOCKQUOTE", "PRE", "CODE", "A", "EM", "STRONG", "DEL", "BR", "HR", "SPAN"]);
const ATTRIBUTES = /^(class|href|draggable|rel|data-[a-z-]+)$/;

function ownElements(root) {
  const out = [];
  const walk = (node) => {
    for (const child of node.children) {
      out.push(child);
      if (!child.classList.contains("md-math")) walk(child);
    }
  };
  walk(root);
  return out;
}

function expectSafeShape(root) {
  for (const el of ownElements(root)) {
    expect(ELEMENTS.has(el.tagName), `element ${el.tagName}`).toBe(true);
    for (const attr of el.attributes) expect(attr.name, `attribute on ${el.tagName}`).toMatch(ATTRIBUTES);
  }
}

const blocks = (root) => [...root.children].map((el) => el.tagName.toLowerCase());

describe("blocks", () => {
  test("paragraphs: one per block, soft breaks kept as text, entities decoded", () => {
    const root = render("one &amp; two &copy;\nsame paragraph\n\nnext");
    expect(blocks(root)).toEqual(["p", "p"]);
    expect(root.children[0].textContent).toBe("one & two ©\nsame paragraph");
    expectSafeShape(root);
  });

  test("a hard line break", () => {
    const root = render("a  \nb\\\nc");
    expect(root.querySelectorAll("p > br").length).toBe(2);
    expect(root.textContent).toBe("abc");
  });

  test("headings, without their marks", () => {
    const root = render("# One\n## Two #\n### Three\n#### Four\n##### Five\n###### Six\n\nSetext\n===");
    expect(blocks(root)).toEqual(["h1", "h2", "h3", "h4", "h5", "h6", "h1"]);
    expect([...root.children].map((h) => h.textContent)).toEqual(["One", "Two", "Three", "Four", "Five", "Six", "Setext"]);
    expectSafeShape(root);
  });

  test("bullet and numbered lists, tight and loose, nested", () => {
    const root = render("- a\n- b\n  - nested\n\n3. three\n4. four\n\n   more\n");
    expect(blocks(root)).toEqual(["ul", "ol"]);
    const [ul, ol] = root.children;
    expect([...ul.children].map((li) => li.firstChild.textContent)).toEqual(["a", "b"]);
    expect(ul.children[1].querySelector("ul > li").textContent).toBe("nested");
    expect([...ol.children].map((li) => li.dataset.n)).toEqual(["3", "4"]);
    expect([...ol.children[1].children].map((el) => el.tagName)).toEqual(["P", "P"]);
    expectSafeShape(root);
  });

  test("task boxes, tight and loose", () => {
    const root = render("- [ ] todo\n- [x] done\n\n1. [x] loose\n\n2. [ ] second\n");
    const tasks = root.querySelectorAll("li.md-task");
    expect(tasks.length).toBe(4);
    expect([...tasks].map((li) => li.querySelector(".md-taskbox").dataset.checked)).toEqual(["false", "true", "true", "false"]);
    expect(tasks[0].textContent).toBe("todo");
    expect(root.textContent).not.toMatch(/\[[ x]\]/);
    expect(root.querySelector("input")).toBeNull();
    expectSafeShape(root);
  });

  test("links: kept ones are guarded anchors; refused ones are plain text", () => {
    const root = render("[site](https://example.com/a) and [bad](javascript:alert(1)) and <https://auto.example> and www.example.org");
    const anchors = [...root.querySelectorAll("a")];
    expect(anchors.map((a) => [a.textContent, a.getAttribute("href"), a.getAttribute("draggable"), a.getAttribute("rel")])).toEqual([
      ["site", "https://example.com/a", "false", "noreferrer"],
      ["https://auto.example", "https://auto.example", "false", "noreferrer"],
      ["www.example.org", "http://www.example.org", "false", "noreferrer"],
    ]);
    expect(anchors.some((a) => a.hasAttribute("target") || a.hasAttribute("title"))).toBe(false);
    const refused = root.querySelector(".md-link-refused");
    expect(refused.tagName).toBe("SPAN");
    expect(refused.textContent).toBe("bad");
    expectSafeShape(root);
  });

  test("an entity in a link target is read before the check", () => {
    const root = render("[x](&#106;avascript:alert(1)) [y](&#104;ttps://example.com)");
    expect(root.querySelector(".md-link-refused").textContent).toBe("x");
    expect(root.querySelector("a").getAttribute("href")).toBe("https://example.com");
  });

  test("code blocks: highlighted, the language label inside, no fences shown", () => {
    const root = render('```go\nfunc main() {\n\tprintln("<b>")\n}\n```');
    expect(blocks(root)).toEqual(["pre"]);
    const pre = root.firstChild;
    expect(pre.querySelector(".md-code-lang").textContent).toBe("go");
    const body = pre.querySelector("code.md-code-body");
    expect(body.textContent).toBe('func main() {\n\tprintln("<b>")\n}');
    expect(body.querySelector("span.hljs-keyword").textContent).toBe("func");
    expect(pre.textContent).not.toContain("```");
    expectSafeShape(root);
  });

  test("an indented code block and a fence without a language", () => {
    const root = render("    plain <i>code</i>\n\n```\nno language\n```");
    expect(blocks(root)).toEqual(["pre", "pre"]);
    expect(root.children[0].querySelector("code").textContent).toBe("plain <i>code</i>");
    expect(root.children[0].querySelector(".md-code-lang")).toBeNull();
    expect(root.children[1].querySelector(".md-code-lang").textContent).toBe("");
  });

  test("a fence still open is not highlighted yet", () => {
    const root = render("```js\nconst a = 1;", { finished: false });
    expect(root.querySelector(".md-code-body").textContent).toBe("const a = 1;");
    expect(root.querySelector(".md-code-body span")).toBeNull();
  });

  test("inline code: no backticks, no box (CHAT-017)", () => {
    const root = render("run `go test ./...` now and `` a`b ``");
    const codes = [...root.querySelectorAll("code.md-icode")];
    expect(codes.map((c) => c.textContent)).toEqual(["go test ./...", "a`b"]);
    expect(root.textContent).toBe("run go test ./... now and a`b");
  });

  test("emphasis, strong and strikethrough", () => {
    const root = render("*em* **strong** ~~gone~~ ***both***");
    expect(root.querySelector("em").textContent).toBe("em");
    expect(root.querySelector("strong").textContent).toBe("strong");
    expect(root.querySelector("del").textContent).toBe("gone");
    expect(root.textContent).toBe("em strong gone both");
  });

  test("raw HTML shows as text with every tag removed (1.7 item 2)", () => {
    const root = render('<div onclick="x()">\nhi <b>there</b> &amp; you\n</div>\n\na <i>b</i> <img src=x onerror=alert(1)> c\n\n<script>alert(1)</script>');
    expect(blocks(root)).toEqual(["p", "p"]);
    expect(root.children[0].textContent).toBe("hi there & you");
    expect(root.children[1].textContent).toBe("a b  c");
    expect(root.querySelector("div, b, i, img, script")).toBeNull();
    expectSafeShape(root);
  });
});

describe("visuals become chips (4.3)", () => {
  const text = [
    "| Name | Kind | Size | More |",
    "|---|---|---|---|",
    "| a | b | c | d |",
    "",
    "$$",
    "\\int_0^1 x^2 \\, dx = \\frac{1}{3} + \\text{a long tail}",
    "$$",
    "",
    "```mermaid",
    "flowchart LR",
    "  A[Sign in] --> B[Home]",
    "```",
    "",
    "![A chart](data:image/png;base64,AAAA)",
    "",
    "Inline ![shot](https://images.example.com/a/shot.png) here, and [![in a link](x.png)](https://example.com).",
  ].join("\n");

  test("in source order, each where its block was, ids from the message", () => {
    const root = render(text);
    const chips = [...root.querySelectorAll("[data-kind]")];
    expect(chips.map((c) => [c.dataset.kind, c.dataset.figure])).toEqual([
      ["table", "m1:0"],
      ["math", "m1:1"],
      ["diagram", "m1:2"],
      ["picture", "m1:3"],
      ["picture", "m1:4"],
    ]);
    // The four block visuals stand where their blocks were, at the top level.
    expect([...root.children].slice(0, 4).map((el) => el.dataset.kind)).toEqual(["table", "math", "diagram", "picture"]);
    // The web picture is a link chip that opens the browser; the linked picture is its alt text.
    expect(chips[4].tagName).toBe("A");
    expect(chips[4].getAttribute("href")).toBe("https://images.example.com/a/shot.png");
    expect(root.querySelector("a:not([data-kind])").textContent).toBe("in a link");
    expect(root.querySelector("table, img, svg")).toBeNull();
    expectSafeShape(root);
  });

  test("a visual past the stable boundary has no figure yet", () => {
    const root = render("para\n\n| a | b |\n|---|---|\n| 1 | 2 |", { finished: false });
    const chip = root.querySelector("[data-kind]");
    expect(chip.dataset.kind).toBe("table");
    expect(chip.dataset.figure).toBeUndefined();
  });

  test("the caller can draw its own chip", () => {
    const seen = [];
    const root = render("$$\nx\n$$", {
      visual(info) {
        seen.push(info);
        const node = document.createElement("span");
        node.className = "custom";
        return node;
      },
    });
    expect(root.firstChild.className).toBe("custom");
    expect(seen[0]).toMatchObject({ kind: "math", index: 0, figureId: "m1:0", title: "x", source: "x", inline: false });
    expect(seen[0].figure).toEqual({ id: "m1:0", sessionId: "s1", messageId: "m1", index: 0, kind: "math", title: "x", source: "x" });
  });
});

describe("plan markers never show (CHAT-033)", () => {
  test("start and ready markers, fenced or bare", () => {
    const root = render(`${PLAN_START_MARKER}\n# Plan\n\n- step\n\n\`\`\`\n${PLAN_READY_MARKER}\n\`\`\``, { plan: "closed" });
    expect(root.textContent).not.toContain("CRUSH_PLAN");
    expect(root.querySelector("pre")).toBeNull();
  });

  test("a real fence just above the ready marker stays", () => {
    const root = render(`plan\n\n\`\`\`go\nx := 1\n\`\`\`\n${PLAN_READY_MARKER}`);
    expect(root.querySelector("pre .md-code-body").textContent).toBe("x := 1");
    expect(root.textContent).not.toContain("CRUSH_PLAN");
  });
});
