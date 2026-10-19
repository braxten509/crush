import { describe, expect, it } from "vitest";
import { readable } from "./contrast.js";
import { contrast, mix, tokensOf } from "./tokens.js";

// Theme colors (desktop-design-5-6 3.1, 3.6, 3.7): a slip in this math gives
// a theme that looks a little off or hard to read, with no error anywhere.
const EDITION = { fg_base: "#0f0f0f", bg_base: "#f9f8ef", primary: "#e52b0e", warning: "#e0a523", plan: "#2f5d73" };

describe("color math", () => {
  it("mixes per channel with rounding, and measures WCAG contrast", () => {
    expect(mix("#000000", "#ffffff", 0.5)).toBe("#808080");
    expect(mix("#ff0000", "#0000ff", 0.25)).toBe("#4000bf");
    expect(contrast("#000000", "#ffffff")).toBeCloseTo(21, 5);
    expect(contrast("#777777", "#777777")).toBeCloseTo(1, 5);
  });
});

describe("tokensOf", () => {
  it("keeps the edition's exact derived values while its roles are untouched", () => {
    const tokens = tokensOf({ root: "scholars-edition", resolved: EDITION });
    expect(tokens["--rule-frame"]).toBe("#aaa9a3");
    expect(tokens["--selection"]).toBe("#e6d9a8");
    expect(tokens["--paper"]).toBe("#f9f8ef");
  });

  it("mixes derived values once a role they read has changed", () => {
    const tokens = tokensOf({ root: "scholars-edition", resolved: { ...EDITION, bg_base: "#ffffff" } });
    expect(tokens["--rule-frame"]).toBe(mix("#0f0f0f", "#ffffff", 0.338));
    // The wash reads the paper too, so it is mixed as well.
    expect(tokens["--vermilion-wash"]).toBe(mix("#e52b0e", "#ffffff", 0.04));
  });

  it("puts the more readable of ink and paper on the warning color", () => {
    const light = tokensOf({ resolved: { fg_base: "#111111", bg_base: "#fafafa", warning: "#f0c040" } });
    expect(light["--on-amber"]).toBe("#111111");
    const dark = tokensOf({ resolved: { fg_base: "#202020", bg_base: "#fefefe", warning: "#303030" } });
    expect(dark["--on-amber"]).toBe("#fefefe");
  });
});

describe("readable", () => {
  it("moves a hard-to-read text color until it passes, and never a background", () => {
    const tokens = { "--paper": "#1a1a1a", "--band": "#222222", "--ink": "#333333", "--slate": "#eeeeee" };
    const { tokens: out, adjusted } = readable(tokens);
    expect(adjusted).toContain("--ink");
    expect(contrast(out["--ink"], "#1a1a1a")).toBeGreaterThanOrEqual(4.5);
    expect(contrast(out["--ink"], "#222222")).toBeGreaterThanOrEqual(4.5);
    expect(out["--paper"]).toBe("#1a1a1a");
    expect(out["--slate"]).toBe("#eeeeee");
    expect(adjusted).not.toContain("--slate");
  });

  it("leaves the edition as it is", () => {
    const { adjusted } = readable(tokensOf({ root: "scholars-edition", resolved: { ...EDITION, bg_least_visible: "#fbfaf2", bg_less_visible: "#f3f1e6" } }));
    expect(adjusted).toEqual([]);
  });
});
