import { describe, expect, it } from "vitest";
import { fitHelp, helpText } from "./helpfit.js";

// u:kit.help_truncate (desktop-design-5-6 2.1, 7.3). With one unit per
// character, the footer must be the line the terminal's ShortHelpLine prints
// at that width (internal/ui/dialog/internal/kit/help.go:26-65): the
// commands sheet's pairs (internal/ui/dialog/commands.go ShortHelp).
const PAIRS = [
  { key: "tab", text: "switch selection" },
  { key: "↑/↓", text: "choose" },
  { key: "enter", text: "confirm" },
  { key: "esc", text: "cancel" },
];
const CELLS = { pair: (p) => [...`${p.key} ${p.text}`].length, separator: 3, space: 1, ellipsis: 1 };
const line = (pairs, width) => helpText(fitHelp(pairs, width, CELLS));

describe("the help footer's one line", () => {
  it("help_truncate", () => {
    const full = "tab switch selection · ↑/↓ choose · enter confirm · esc cancel";
    expect(line(PAIRS, 100)).toBe(full);
    expect(line(PAIRS, full.length)).toBe(full);
    // One cell short: the last pair drops, the line ends on a hint plus " …".
    expect(line(PAIRS, full.length - 1)).toBe("tab switch selection · ↑/↓ choose · enter confirm …");
    // "enter confirm" no longer fits, but its " …" does.
    expect(line(PAIRS, 37)).toBe("tab switch selection · ↑/↓ choose …");
    expect(line(PAIRS, 35)).toBe("tab switch selection · ↑/↓ choose …");
    expect(line(PAIRS, 30)).toBe("tab switch selection …");
    // " …" itself does not fit after the shown pairs: no ellipsis, never " ·".
    expect(line(PAIRS, 21)).toBe("tab switch selection");
    expect(line(PAIRS, 34)).toBe("tab switch selection · ↑/↓ choose");
    // Not even the first pair fits: the ellipsis alone, with no space before it.
    expect(line(PAIRS, 5)).toBe("…");
    expect(line(PAIRS, 0)).toBe("");
    // Disabled pairs are left out, and the line closes up behind them.
    const disabled = [PAIRS[0], { key: "ctrl+d", text: "delete", disabled: true }, ...PAIRS.slice(1)];
    expect(line(disabled, 100)).toBe(full);
    expect(fitHelp(disabled, 100, CELLS).shown.some((p) => p.disabled)).toBe(false);
    for (let width = 0; width <= full.length + 2; width++) {
      const text = line(PAIRS, width);
      expect(text.length).toBeLessThanOrEqual(width);
      expect(text.endsWith("·")).toBe(false);
      expect(text.includes("· …")).toBe(false);
    }
  });
});
