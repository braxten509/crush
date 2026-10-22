// Readable contrast in every theme (desktop-design-5-6 3.6): each listed
// pair is checked with the WCAG ratio; a failing foreground is mixed toward
// black (light background) or white (dark background) in 2 % steps until
// every background in its list passes. Backgrounds, rules and the ANSI
// colors are never adjusted. Pure: returns a new object.
import { contrast, luminance, mix } from "./tokens.js";

export const PAIRS = [
  [["--ink"], ["--paper", "--paper-raised", "--band", "--control-rest", "--control-hover", "--selection", "--pick"], 4.5],
  [["--slate"], ["--paper", "--band"], 4.5],
  [["--link", "--keyword", "--error-ink", "--amber-ink", "--diff-add-ink", "--info-soft", "--code-inline"], ["--paper"], 4.5],
  [["--hl-keyword", "--hl-string", "--hl-number", "--hl-comment", "--hl-function", "--hl-type", "--hl-attr", "--hl-meta", "--code-inline"], ["--code-bg"], 4.5],
  [["--mark"], ["--paper"], 4.5],
  [["--vermilion-ink"], ["--paper", "--vermilion-wash"], 4.5],
  [["--on-accent"], ["--info-strong"], 4.5],
  [["--on-amber"], ["--amber"], 4.5],
  [["--on-accent"], ["--vermilion-fill"], 3],
  [["--vermilion", "--rule-control", "--choice", "--busy", "--attention", "--plan"], ["--paper"], 3],
];
// "--paper on --attention" (WARN tag text) adjusts the tag fill's text, which
// is --paper itself, a background: it is checked but never moved.
const CHECK_ONLY = [[["--paper"], ["--attention"], 4.5]];

function adjust(fg, backgrounds, minimum) {
  let value = fg;
  const pass = (v) => backgrounds.every((bg) => contrast(v, bg) >= minimum);
  if (pass(value)) return value;
  const dark = backgrounds.reduce((sum, bg) => sum + luminance(bg), 0) / backgrounds.length > 0.18;
  const target = dark ? "#000000" : "#ffffff";
  for (let step = 1; step <= 50; step++) {
    value = mix(target, fg, step * 0.02);
    if (pass(value)) return value;
  }
  return target;
}

// → {tokens, adjusted: [token…]}
export function readable(tokens) {
  const out = { ...tokens };
  const bgOf = (name) => (name === "--code-bg" ? out["--band"] : out[name]);
  const adjusted = [];
  for (const [fgs, bgs, minimum] of PAIRS) {
    const backgrounds = bgs.map(bgOf).filter(Boolean);
    for (const name of fgs) {
      if (!out[name] || backgrounds.length === 0) continue;
      const next = adjust(out[name], backgrounds, minimum);
      if (next !== out[name]) {
        out[name] = next;
        if (!adjusted.includes(name)) adjusted.push(name);
      }
    }
  }
  return { tokens: out, adjusted, checkOnly: CHECK_ONLY };
}
