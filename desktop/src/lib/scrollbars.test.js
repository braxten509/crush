// Every scroll area that shows a bar uses the system's own bar (owner,
// 2026-10-26: "use THIS design EVERYWHERE": arrows and a grey handle), so no
// style may draw its own: no thin bar, no colors, no thumb or track rules. A
// `::-webkit-scrollbar` rule may only hide the bar (display: none, or 0 wide),
// for the areas that show none on purpose (chat list, strip, terminal, command
// output) and the Settings choice "never".
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const SRC = join(import.meta.dirname, "..");

function styleFiles(dir) {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return styleFiles(path);
    return /\.(svelte|css)$/.test(name) ? [path] : [];
  });
}

// Each rule whose selector names a scrollbar part, as "file: selector { body }".
function offenders(path) {
  const text = readFileSync(path, "utf8");
  const found = [];
  for (const [, selector, body] of text.matchAll(/([^{}]*)\{([^{}]*)\}/g)) {
    const where = `${path.slice(SRC.length + 1)}: ${selector.trim()}`;
    for (const [rule] of body.matchAll(/scrollbar-width:\s*thin|scrollbar-color:[^;]*/g)) found.push(`${where} { ${rule} }`);
    if (/::-webkit-scrollbar-(thumb|track|button|corner)/.test(selector)) found.push(where);
    else if (/::-webkit-scrollbar\b/.test(selector)) {
      const hides = body
        .split(";")
        .map((part) => part.trim())
        .filter(Boolean)
        .every((part) => /^display:\s*none$/.test(part) || /^(width|height):\s*0(px)?$/.test(part));
      if (!hides) found.push(`${where} { ${body.trim()} }`);
    }
  }
  return found;
}

describe("scrollbars", () => {
  it("no style draws its own scrollbar; only hiding one is allowed", () => {
    expect(styleFiles(SRC).flatMap(offenders)).toEqual([]);
  });
});
