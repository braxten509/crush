// Math (1.7 item 5): KaTeX with trust off, cleaned before insertion; the
// marked extensions that find math in a reply.
import { Marked } from "marked";
import { describe, expect, test } from "vitest";
import { KATEX_OPTIONS, mathExtensions, mathTitle, renderMath } from "./math.js";

function lex(src) {
  return new Marked({ gfm: true }).use({ extensions: mathExtensions }).lexer(src);
}

function inlineTypes(src) {
  return lex(src)[0].tokens.map((t) => [t.type, t.type === "mathInline" ? t.text : t.raw]);
}

describe("renderMath", () => {
  test("the options of 1.7", () => {
    expect(KATEX_OPTIONS).toMatchObject({ trust: false, throwOnError: false, strict: "ignore", maxExpand: 1000, output: "htmlAndMathml" });
  });

  test("renders inline and display math, MathML kept", () => {
    const inline = renderMath("x^2");
    expect(inline.querySelector(".katex .katex-html")).not.toBeNull();
    expect(inline.querySelector("math")).not.toBeNull();
    const display = renderMath("\\int_0^1 x\\,dx", { display: true });
    expect(display.querySelector(".katex-display")).not.toBeNull();
  });

  test("\\href, \\url and \\includegraphics make no link and no picture", () => {
    for (const tex of ["\\href{javascript:alert(1)}{x}", "\\url{https://example.com}", "\\includegraphics{http://127.0.0.1/beacon}", "\\htmlClass{x}{y}", "\\htmlData{onclick=alert(1)}{y}"]) {
      const fragment = renderMath(tex);
      expect(fragment.querySelector("a, img, [href], [src]")).toBeNull();
      for (const el of fragment.querySelectorAll("*")) {
        for (const attr of el.attributes) expect(attr.name).not.toMatch(/^on/i);
      }
    }
  });

  test("broken TeX shows in the error color and never throws", () => {
    const fragment = renderMath("\\frac{");
    expect(fragment.querySelector(".katex-error")).not.toBeNull();
    expect(fragment.textContent).toContain("\\frac{");
  });

  test("a title is the first 24 characters", () => {
    expect(mathTitle("E = mc^2")).toBe("E = mc^2");
    expect(mathTitle("\\sum_{i=1}^{n} i = \\frac{n(n+1)}{2}")).toBe("\\sum_{i=1}^{n} i = \\frac…");
    expect(mathTitle("a\n  b")).toBe("a b");
  });
});

describe("finding math", () => {
  test("display blocks: $$ and \\[ on their own lines, no blank line inside", () => {
    const tokens = lex("Before:\n$$\nx^2\n$$\nafter\n\n\\[\na+b\n\\]\n");
    expect(tokens.map((t) => t.type)).toEqual(["paragraph", "mathBlock", "paragraph", "space", "mathBlock"]);
    expect(tokens[1].text).toBe("x^2");
    expect(tokens[4].text).toBe("a+b");
    expect(lex("$$x^2$$")[0].type).toBe("mathBlock");
  });

  test("a blank line or a missing closer is no block, and the paragraph stays whole", () => {
    expect(lex("$$\nx\n\ny\n$$").some((t) => t.type === "mathBlock")).toBe(false);
    const tokens = lex("a\n$$ not closed\nb");
    expect(tokens.map((t) => t.type)).toEqual(["paragraph"]);
    expect(tokens[0].raw).toBe("a\n$$ not closed\nb");
    expect(lex("$$\nx\n\\]").some((t) => t.type === "mathBlock")).toBe(false);
  });

  test("inline math, with the currency rule", () => {
    expect(inlineTypes("a $x^2$ b")).toEqual([
      ["text", "a "],
      ["mathInline", "x^2"],
      ["text", " b"],
    ]);
    expect(inlineTypes("costs $5 and $10 now")).toEqual([["text", "costs $5 and $10 now"]]);
    expect(inlineTypes("$ x$ and $x $")).toEqual([["text", "$ x$ and $x $"]]);
    expect(inlineTypes("\\(a+b\\) and $$c$$")).toEqual([
      ["mathInline", "a+b"],
      ["text", " and "],
      ["mathInline", "c"],
    ]);
    expect(inlineTypes("price \\$5 and $y$")).toEqual([
      ["text", "price "],
      ["escape", "\\$"],
      ["text", "5 and "],
      ["mathInline", "y"],
    ]);
  });

  test("math inside code stays code", () => {
    const tokens = lex("`$x$` and\n\n```\n$$\nx\n$$\n```");
    expect(tokens[0].tokens.map((t) => t.type)).toEqual(["codespan", "text"]);
    expect(tokens.at(-1).type).toBe("code");
  });
});
