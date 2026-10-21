// Every CSS animation that ends must also run --paper-tick from 0 to 1, or
// WebKitGTK draws one stale frame as it ends (app.css, FLASHING.md 16-21).
// Endless loops never end while shown, so they are left out.
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { CLOSE_MS, drawer, scrim, settle, slidePage, unfold } from "./lib/motion.js";

const SRC = new URL(".", import.meta.url).pathname;

function files(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return files(path);
    return /\.(css|svelte)$/.test(entry.name) ? [path] : [];
  });
}

// Each @keyframes block in a file, with its name and body.
function keyframes(text) {
  const found = [];
  for (const match of text.matchAll(/@keyframes\s+([\w-]+)\s*\{/g)) {
    let depth = 1;
    let i = match.index + match[0].length;
    while (depth > 0 && i < text.length) {
      if (text[i] === "{") depth++;
      else if (text[i] === "}") depth--;
      i++;
    }
    found.push({ name: match[1], body: text.slice(match.index, i) });
  }
  return found;
}

// True when every `animation:` line naming the keyframes repeats forever.
function loops(text, name) {
  const uses = [...text.matchAll(/animation:\s*([^;]*)/g)].map((m) => m[1]).filter((value) => new RegExp(`(^|[\\s,])${name}([\\s,]|$)`).test(value));
  return uses.length > 0 && uses.every((value) => value.includes("infinite"));
}

const all = files(SRC).flatMap((path) => {
  const text = readFileSync(path, "utf8");
  return keyframes(text).map((frames) => ({ ...frames, path: path.slice(SRC.length), loops: loops(text, frames.name) }));
});

describe("animation ticks", () => {
  it("finds the shared paper animations", () => {
    expect(all.map((frames) => frames.name)).toEqual(expect.arrayContaining(["paper-settle"]));
  });

  it.each(all.filter((frames) => !frames.loops).map((frames) => [`${frames.path} ${frames.name}`, frames.body]))("%s runs --paper-tick 0 to 1", (_, body) => {
    expect(body).toMatch(/--paper-tick:\s*0/);
    expect(body).toMatch(/--paper-tick:\s*1/);
  });

  // Svelte transitions become the same kind of animation, both ways; closing
  // is quicker and, but for the scrim, ends wholly clipped away (the scrim fades
  // out, the drawer leaves the window).
  it.each([["unfold", unfold], ["slidePage", slidePage], ["settle", settle], ["scrim", scrim], ["drawer", drawer]])("%s runs --paper-tick opening and closing", (name, motion) => {
    for (const direction of ["in", "out"]) {
      const { css, duration } = motion(null)({ direction });
      expect(css(0, 1)).toMatch(/--paper-tick: 0\.000;/);
      expect(css(1, 0)).toMatch(/--paper-tick: 1\.000;/);
      if (direction === "out") {
        expect(duration).toBe(CLOSE_MS);
        if (name === "drawer") expect(css(0, 1)).toMatch(/translateX\(100\.00%\)/);
        else if (name !== "scrim") expect(css(0, 1)).toMatch(/clip-path: inset\([^)]*100\.00%/);
      }
    }
  });
});
