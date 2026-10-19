// @vitest-environment jsdom
// CHAT-110 and CHAT-112: diff code colored by the file's language (else a
// guess over at most 20 KB), control characters drawn as pictures, and the
// side-scroll cut of the colored line.
import { describe, expect, it } from "vitest";
import { cutLeft } from "../diff.js";
import { codeLine, cutCells, diffLanguage, GUESS_LIMIT } from "./highlight.js";

const GO = 'package main\n\nimport "fmt"\n\nfunc main() {\n\tfmt.Println("hello")\n}\n';

describe("diff languages", () => {
  it("takes the file name's language first", () => {
    expect(diffLanguage("internal/a.go", "")).toBe("go");
    expect(diffLanguage("Dockerfile", "")).toBe("dockerfile");
    expect(diffLanguage("notes.txt", GO)).toBe("plaintext");
  });
  it("guesses from the text only up to 20 KB", () => {
    expect(diffLanguage("noext", GO)).toBe("go");
    expect(diffLanguage("noext", GO.repeat(Math.ceil(GUESS_LIMIT / GO.length) + 1))).toBe("plaintext");
    expect(diffLanguage("noext", "")).toBe("plaintext");
  });
});

describe("diff lines", () => {
  it("never puts a control character in the page (NUL, DEL, ESC)", () => {
    const fragment = codeLine("a\x00b\x7fc\x1b[31m", "plaintext");
    expect(fragment.textContent).toBe("a␀b␡c␛[31m");
    expect(fragment.textContent).not.toMatch(/[\x00-\x1f\x7f]/);
  });
  it("colors code with spans and classes only", () => {
    const fragment = codeLine('fmt.Println("hi")', "go");
    expect(fragment.textContent).toBe('fmt.Println("hi")');
    const div = document.createElement("div");
    div.append(fragment);
    expect(div.querySelector("span[class]")).not.toBeNull();
    for (const el of div.querySelectorAll("*")) expect([...el.attributes].map((a) => a.name)).toEqual(["class"]);
  });
  it("cuts the colored line by cells, as the plain cut does", () => {
    for (const [text, n] of [
      ['fmt.Println("hello")', 5],
      ["日本語のテキスト", 3],
      ["étude", 1],
      ["short", 9],
      ["abc", 0],
    ]) {
      const fragment = cutCells(codeLine(text, "go"), n);
      expect(fragment.textContent, `${text} ${n}`).toBe(cutLeft(text, n));
    }
  });
});
