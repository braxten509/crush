// Every scroll area that shows a bar uses one plain bar (owner, 2026-10-26:
// "use THIS design EVERYWHERE", then "previously the simple rectangle"): a
// thin grey rectangle, no arrows, no track. It is drawn once, in app.css, for
// the whole app; no other style may draw its own. Elsewhere a scrollbar rule
// may only hide the bar (display: none, 0 wide, scrollbar-width: none), for
// the areas that show none on purpose (chat list, strip, terminal, command
// output) and the Settings choice "never".
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const SRC = join(import.meta.dirname, "..");
const APP_CSS = join(SRC, "app.css");

function styleFiles(dir) {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return styleFiles(path);
    return /\.(svelte|css)$/.test(name) ? [path] : [];
  });
}

// Each rule as [selector, body], comments left out.
function rules(path) {
  const text = readFileSync(path, "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
  return [...text.matchAll(/([^{}]*)\{([^{}]*)\}/g)].map(([, selector, body]) => [selector.trim(), body]);
}

// Each rule outside app.css whose selector names a scrollbar part and does
// more than hide it, as "file: selector { body }".
function offenders(path) {
  if (path === APP_CSS) return [];
  const found = [];
  for (const [selector, body] of rules(path)) {
    const where = `${path.slice(SRC.length + 1)}: ${selector}`;
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

// The body of app.css's rule for exactly this selector, spaces squeezed.
function appRule(selector) {
  const found = rules(APP_CSS).find(([s]) => s.split(",").map((part) => part.trim()).includes(selector));
  return found ? found[1].replace(/\s+/g, " ").trim() : "";
}

describe("scrollbars", () => {
  it("app.css draws the one plain bar: a thin grey rectangle, no arrows, no track", () => {
    expect(appRule("::-webkit-scrollbar")).toMatch(/width: calc\(6 \* var\(--px\)\)/);
    expect(appRule("::-webkit-scrollbar")).toMatch(/height: calc\(6 \* var\(--px\)\)/);
    expect(appRule("::-webkit-scrollbar-thumb")).toMatch(/background: var\(--rule-control\)/);
    expect(appRule("::-webkit-scrollbar-button")).toMatch(/display: none/);
    expect(appRule("::-webkit-scrollbar-track")).toMatch(/background: transparent/);
    expect(appRule("::-webkit-scrollbar-corner")).toMatch(/background: transparent/);
  });

  it("no other style draws its own scrollbar; only hiding one is allowed", () => {
    expect(styleFiles(SRC).flatMap(offenders)).toEqual([]);
  });
});
