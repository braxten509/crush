import { describe, expect, it } from "vitest";
import { darkView, wantsDark } from "./dark.js";
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
    ansi_black: "#000000",
    ansi_white: "#e5e5e5",
    ansi_yellow: "#c4a000",
    diff_insert_fg: "#2e7d32",
    diff_insert_code_bg: "#eaf0dc",
  },
};

describe("darkView", () => {
  it("swaps paper and ink and keeps each gray's place between them", () => {
    const view = darkView(light);
    expect(view.dark).toBe(true);
    expect(isDark(view)).toBe(true);
    expect(view.resolved.bg_base).toBe("#0f0f0f");
    expect(view.resolved.fg_base).toBe("#f9f8ef");
    // A band a little darker than the paper is now a little lighter.
    expect(luminance(view.resolved.bg_less_visible)).toBeGreaterThan(luminance(view.resolved.bg_base));
    expect(luminance(view.resolved.bg_less_visible)).toBeLessThan(luminance(view.resolved.separator));
  });

  it("leaves colored roles, trades the program's black and white, and lifts colors to read", () => {
    const view = darkView(light);
    expect(view.resolved.primary).toBe("#e52b0e");
    expect(view.resolved.ansi_black).not.toBe("#000000");
    expect(contrast(view.resolved.ansi_yellow, view.resolved.bg_less_visible)).toBeGreaterThanOrEqual(4.5);
    expect(luminance(view.resolved.diff_insert_code_bg)).toBeLessThan(0.1);
  });

  it("turns ink-like colors nearly as light as the new ink", () => {
    const view = darkView(light);
    expect(view.resolved.secondary).not.toBe("#14323f");
    expect(contrast(view.resolved.secondary, view.resolved.bg_base)).toBeGreaterThanOrEqual(10);
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
