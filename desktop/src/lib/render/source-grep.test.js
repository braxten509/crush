// Desktop-design 1.7 item 9: every HTML string the codebase parses goes
// through lib/render/sanitize.js, the only module that may import DOMPurify.
// This reads every source file under src/ (and the page itself) and fails on
// any other way of putting an HTML string into the page.
import { expect, test } from "vitest";

const files = import.meta.glob(["/src/**/*.js", "/src/**/*.svelte", "/index.html"], { query: "?raw", import: "default", eager: true });
const SANITIZE = "/src/lib/render/sanitize.js";

// Built from pieces, so this file does not find itself.
const WORDS = [
  ["inner", "HTML"],
  ["outer", "HTML"],
  ["insertAdjacent", "HTML"],
  ["{@", "html"],
  ["createContextual", "Fragment"],
  ["DOM", "Parser"],
  ["document.", "write"],
  ["setHTML", "Unsafe"],
  ["parseHTML", "Unsafe"],
].map((parts) => parts.join(""));
const IMPORTS_PURIFY = new RegExp(["from\\s*[\"']", "dompurify", "[\"']|import\\(\\s*[\"']", "dompurify", "[\"']"].join(""));

test("the whole source tree is read", () => {
  expect(Object.keys(files).length).toBeGreaterThan(50);
  expect(files["/index.html"]).toContain("<div id=");
  expect(files[SANITIZE]).toMatch(IMPORTS_PURIFY);
  // A component is read as written, not as Svelte compiled it.
  expect(files["/src/components/render/MarkdownView.svelte"]).toMatch(/<script/);
});

test("no file but sanitize.js parses an HTML string or imports DOMPurify", () => {
  const found = [];
  for (const [path, text] of Object.entries(files)) {
    if (path === SANITIZE) continue;
    for (const word of WORDS) if (text.includes(word)) found.push(`${path}: ${word}`);
    if (IMPORTS_PURIFY.test(text)) found.push(`${path}: imports DOMPurify`);
  }
  expect(found).toEqual([]);
});
