import { describe, expect, it } from "vitest";
import { PAPER, darkView, oklch, wantsDark } from "./dark.js";
import { contrast, isDark, luminance } from "./tokens.js";

const light = {
  name: "scholars-edition",
  dark: false,
  resolved: {
    fg_base: "#0f0f0f",
    fg_more_subtle: "#55544f",
    bg_base: "#f9f8ef",
    bg_less_visible: "#efeee4",
    separator: "#cfcec7",
    primary: "#e52b0e",
    secondary: "#14323f",
    on_primary: "#f9f8ef",
    ansi_black: "#000000",
    ansi_white: "#e5e5e5",
    ansi_yellow: "#c4a000",
    diff_insert_fg: "#2e7d32",
    diff_insert_code_bg: "#eaf0dc",
  },
};

const hueGap = (a, b) => Math.abs(((oklch(a)[2] - oklch(b)[2] + 540) % 360) - 180);

describe("darkView (Graphite)", () => {
  it("lays soft linen ink on graphite paper and keeps each gray's place between them", () => {
    const view = darkView(light);
    const r = view.resolved;
    expect(view.dark).toBe(true);
    expect(isDark(view)).toBe(true);
    expect(r.bg_base).toBe(PAPER);
    // The ink reads well on the paper.
    expect(contrast(r.fg_base, r.bg_base)).toBeGreaterThanOrEqual(7);
    // Quiet text stays quieter than the ink and still reads.
    expect(contrast(r.fg_more_subtle, r.bg_base)).toBeGreaterThanOrEqual(4.5);
    expect(contrast(r.fg_more_subtle, r.bg_base)).toBeLessThan(contrast(r.fg_base, r.bg_base));
    // A band a little darker than the light paper is a little lighter here, below the rules.
    expect(luminance(r.bg_less_visible)).toBeGreaterThan(luminance(r.bg_base));
    expect(luminance(r.bg_less_visible)).toBeLessThan(luminance(r.separator));
    // The grays carry no color.
    expect(oklch(r.bg_less_visible)[1]).toBeLessThan(0.01);
    expect(oklch(r.separator)[1]).toBeLessThan(0.01);
  });

  it("keeps each color's hue but calms it, and trades the program's black and white", () => {
    const r = darkView(light).resolved;
    expect(hueGap(r.primary, light.resolved.primary)).toBeLessThan(10);
    expect(contrast(r.primary, r.bg_base)).toBeGreaterThanOrEqual(3);
    expect(r.on_primary).toBe(PAPER);
    expect(r.ansi_black).not.toBe("#000000");
    expect(contrast(r.ansi_yellow, r.bg_less_visible)).toBeGreaterThanOrEqual(4.5);
  });

  it("turns ink-like colors light, with a trace of their hue", () => {
    const r = darkView(light).resolved;
    expect(r.secondary).not.toBe("#14323f");
    expect(contrast(r.secondary, r.bg_base)).toBeGreaterThanOrEqual(7);
    expect(hueGap(r.secondary, light.resolved.secondary)).toBeLessThan(15);
  });

  it("leaves a dark theme alone", () => {
    const dark = { ...light, dark: true };
    expect(darkView(dark)).toBe(dark);
  });

  it("follows the stored mode", () => {
    expect(wantsDark("dark", false)).toBe(true);
    expect(wantsDark("light", true)).toBe(false);
    expect(wantsDark("system", true)).toBe(true);
    expect(wantsDark("system", false)).toBe(false);
    expect(wantsDark(undefined, true)).toBe(false);
  });
});
