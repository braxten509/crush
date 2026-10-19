// Code highlighting (1.7 item 7; CHAT-057's gutter width).
import { describe, expect, test } from "vitest";
import { gutterWidth, highlight, highlightLines, languageFor, languageForPath, showControls, splitLines } from "./code.js";

const elements = (node) => [...node.querySelectorAll("*")];

describe("highlight", () => {
  test("spans with classes only, and exactly the code's text", () => {
    const code = 'func main() {\n\tfmt.Println("hi <b>") // done\n}';
    const fragment = highlight(code, "go");
    expect(fragment.textContent).toBe(code);
    const spans = elements(fragment);
    expect(spans.length).toBeGreaterThan(2);
    for (const el of spans) {
      expect(el.tagName).toBe("SPAN");
      expect([...el.attributes].map((a) => a.name)).toEqual(["class"]);
      expect(el.className).toMatch(/^hljs-/);
    }
    expect(spans.some((el) => el.className === "hljs-keyword" && el.textContent === "func")).toBe(true);
    expect(spans.some((el) => el.className === "hljs-string" && el.textContent === '"hi <b>"')).toBe(true);
  });

  test("markup in code stays text", () => {
    const code = '<img src=x onerror="alert(1)"><script>alert(2)</script>';
    for (const lang of ["html", "xml", "javascript", null, "nonsense"]) {
      const fragment = highlight(code, lang);
      expect(fragment.textContent).toBe(code);
      expect(fragment.querySelector?.("img, script") ?? null).toBeNull();
      for (const el of elements(fragment)) expect(el.tagName).toBe("SPAN");
    }
  });

  test("an unknown language or plain text gives a single text node", () => {
    for (const lang of ["nonsense", "text", "plaintext"]) {
      const fragment = highlight("a < b", lang);
      expect(fragment.childNodes.length).toBe(1);
      expect(fragment.firstChild.nodeType).toBe(3);
    }
  });

  test("a fence without a language is guessed only when short", () => {
    const python = "def f(x):\n    return [i for i in range(x) if i % 2]\n\nprint(f(10))\n";
    expect(elements(highlight(python, "")).length).toBeGreaterThan(0);
    const long = python.repeat(200);
    expect(long.length).toBeGreaterThan(4096);
    expect(highlight(long, "").childNodes.length).toBe(1);
  });

  test("control characters are shown, never hidden", () => {
    expect(showControls("a\x1b[31mb\x00\x7f\tc\nd\re")).toBe("a\u241b[31mb\u2400\u2421\tc\nd\re");
    expect(highlight("x\x1by", "bash").textContent).toBe("x\u241by");
  });
});

describe("lines", () => {
  test("a token that crosses lines is split with its classes on each line", () => {
    const lines = highlightLines("/* one\ntwo */\nx = 1", "javascript");
    expect(lines.map((line) => line.textContent)).toEqual(["/* one", "two */", "x = 1"]);
    expect(lines[0].firstChild.className).toBe("hljs-comment");
    expect(lines[1].firstChild.className).toBe("hljs-comment");
  });

  test("an empty last line is kept, and nested spans keep both classes", () => {
    const fragment = document.createDocumentFragment();
    const outer = document.createElement("span");
    outer.className = "hljs-string";
    const inner = document.createElement("span");
    inner.className = "hljs-subst";
    inner.append("${a}\n");
    outer.append("`x ", inner);
    fragment.append(outer);
    const lines = splitLines(fragment);
    expect(lines.length).toBe(2);
    expect(lines[0].lastChild.className).toBe("hljs-string hljs-subst");
    expect(lines[1].childNodes.length).toBe(0);
  });
});

describe("languages", () => {
  test("fence info strings and aliases", () => {
    expect(languageFor("go")).toBe("go");
    expect(languageFor("Python {linenos}")).toBe("python");
    expect(languageFor("{.rust}")).toBe("rust");
    expect(languageFor("sh")).toBe("bash");
    expect(languageFor("js")).toBe("javascript");
    expect(languageFor("ts")).toBe("typescript");
    expect(languageFor("toml")).toBe("ini");
    expect(languageFor("html")).toBe("xml");
    expect(languageFor("mermaid")).toBeNull();
    expect(languageFor("")).toBeNull();
    expect(languageFor(null)).toBeNull();
  });

  test("file paths", () => {
    expect(languageForPath("internal/ui/chat/user.go")).toBe("go");
    expect(languageForPath("/x/Dockerfile")).toBe("dockerfile");
    expect(languageForPath("Makefile")).toBe("makefile");
    expect(languageForPath("a/b.test.js")).toBe("javascript");
    expect(languageForPath("C:\\x\\y.rs")).toBe("rust");
    expect(languageForPath("README")).toBeNull();
    expect(languageForPath(".bashrc")).toBeNull();
  });
});

describe("gutterWidth", () => {
  test("the digits of the last number", () => {
    expect(gutterWidth(9)).toBe(1);
    expect(gutterWidth(10)).toBe(2);
    expect(gutterWidth(999)).toBe(3);
    expect(gutterWidth(1000)).toBe(4);
    expect(gutterWidth(0)).toBe(1);
  });
});
