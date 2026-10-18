// CHAT-004: your text as typed, with the map back to the stored text.
import { describe, expect, test } from "vitest";
import { sourceMapOf } from "./sourcemap.js";
import { userText, userTextInto } from "./usertext.js";

describe("userText", () => {
  test("marks stay as typed", () => {
    for (const typed of ["<tag>", "*x*", "`x`", "<b>x</b> *y* `z`", "# not a heading", "[a](b)"]) {
      expect(userText(typed).text).toBe(typed);
    }
  });

  test("\\r\\n and lone \\r become \\n; a tab becomes four spaces", () => {
    expect(userText("a\r\nb\rc").text).toBe("a\nb\nc");
    expect(userText("a\tb").text).toBe("a    b");
  });

  test("escape codes are removed", () => {
    expect(userText("\x1b[31mred\x1b[0m text").text).toBe("red text");
    expect(userText("a\x1b]8;;http://x\x07b").text).toBe("ab");
  });

  test("Go's TrimSpace first, then Strip, then the replacer", () => {
    expect(userText("  \n hi \t\n").text).toBe("hi");
    // A color code shields the spaces after it from TrimSpace, as in the terminal.
    expect(userText("\x1b[31m  hi").text).toBe("  hi");
    // \r and \n split by an escape code still make one newline.
    expect(userText("a\r\x1b[0m\nb").text).toBe("a\nb");
    // U+FEFF is not space for Go.
    expect(userText("﻿x").text).toBe("﻿x");
  });

  test("works anywhere a string does", () => {
    const shown = userText("x\ty");
    expect(`${shown}`).toBe("x    y");
    expect(String(shown)).toBe("x    y");
    expect(shown.length).toBe(6);
  });

  test("offsets map back to the stored text", () => {
    const stored = "  \x1b[1mbold\x1b[0m\tend\r\nnext ";
    const shown = userText(stored);
    expect(shown.text).toBe("bold    end\nnext");
    // Every character of a one-to-one piece points at itself.
    for (const word of ["bold", "end", "next"]) {
      const at = shown.text.indexOf(word);
      expect(stored.slice(shown.toSource(at), shown.toSource(at + word.length, "end"))).toBe(word);
    }
    // A point inside the tab's spaces takes the whole tab.
    const tab = stored.indexOf("\t");
    expect(shown.toSource(5, "start")).toBe(tab);
    expect(shown.toSource(6, "end")).toBe(tab + 1);
    // \r\n is one shown character standing for two.
    const nl = shown.text.indexOf("\n");
    expect(shown.toSource(nl)).toBe(stored.indexOf("\r\n"));
    expect(shown.toSource(nl + 1, "end")).toBe(stored.indexOf("\r\n") + 2);
    // The ends.
    expect(shown.toSource(0)).toBe(stored.indexOf("bold"));
    expect(shown.toSource(shown.length, "end")).toBe(stored.indexOf("next") + 4);
    // And back.
    expect(shown.toShown(stored.indexOf("end"))).toBe(shown.text.indexOf("end"));
    expect(shown.toShown(0)).toBe(0);
    expect(shown.toShown(stored.length)).toBe(shown.length);
  });

  test("an empty or all-space message", () => {
    expect(userText("").text).toBe("");
    expect(userText(" \n ").text).toBe("");
    expect(userText(" \n ").toSource(0)).toBe(3);
  });
});

describe("userTextInto", () => {
  test("text nodes only, registered in a source map", () => {
    const el = document.createElement("p");
    const stored = "<b>x</b> *y* `z`\tt";
    userTextInto(el, stored);
    expect(el.textContent).toBe("<b>x</b> *y* `z`    t");
    expect(el.children.length).toBe(0);
    const map = sourceMapOf(el);
    expect(map).not.toBeNull();
    const range = document.createRange();
    const node = el.firstChild;
    range.setStart(node, node.data.indexOf("*y*"));
    range.setEnd(node, node.data.indexOf("*y*") + 3);
    const cut = map.rangeToSource(range);
    expect(stored.slice(cut.start, cut.end)).toBe("*y*");
  });

  test("decorations wrap their text", () => {
    const el = document.createElement("p");
    const stored = "one two three";
    userTextInto(el, stored, { decorations: [{ kind: "branch", start: 4, end: 7 }] });
    const deco = el.querySelector("span.md-deco");
    expect(deco.textContent).toBe("two");
    expect(deco.classList.contains("md-deco-branch")).toBe(true);
    expect(el.textContent).toBe(stored);
  });
});
