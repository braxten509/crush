import { describe, expect, it } from "vitest";
import { DEFAULT_FOOTER_ITEMS, FOOTER_CHOICES } from "../../lib/footer.js";
import { CUSTOMIZE, CUSTOMIZE_COLLAPSED, CUSTOMIZE_HELP, customizeClick, customizeKey, customizeSave, startCustomize } from "./customize.js";

// SIDE-016 to SIDE-024: the terminal's Customize Composer form
// (composer_footer.go:142-181, questionform/multi.go, choice_base.go).

const ids = FOOTER_CHOICES.map((c) => c.id);

function press(state, ...combos) {
  let s = state;
  let done = null;
  for (const combo of combos) {
    const answer = customizeKey(s, combo);
    s = answer.state;
    done = answer.done ?? done;
  }
  return { state: s, done };
}

describe("Customize Composer", () => {
  it("has the terminal's words and help", () => {
    expect(CUSTOMIZE).toEqual({
      label: "Composer",
      text: "What should show under the composer?",
      description: "Space toggles a row, Enter saves. Usage limits show only for agent CLIs that report them.",
    });
    expect(CUSTOMIZE_HELP.map((x) => `${x.key} ${x.action}`)).toEqual(["↑ up", "↓ down", "space toggle", "1-8 quick select", "enter done", "esc exit"]);
    expect(CUSTOMIZE_COLLAPSED).toBe("answer questions");
    expect(FOOTER_CHOICES.map((c) => c.label)).toEqual([
      "Model name",
      "Reasoning, fast and Ultracode",
      "Tokens used",
      "Context left",
      "5-hour usage left",
      "Weekly usage left",
      "Other usage limits",
      "When usage limits reset",
    ]);
  });

  it("starts with the saved rows picked and the cursor on the first", () => {
    expect(startCustomize(DEFAULT_FOOTER_ITEMS)).toEqual({ cursor: 0, picked: DEFAULT_FOOTER_ITEMS });
    expect(startCustomize(["none"])).toEqual({ cursor: 0, picked: [] });
    expect(startCustomize(["resets", "model", "bogus"]).picked).toEqual(["model", "resets"]);
    expect(startCustomize(undefined).picked).toEqual([]);
  });

  it("↑ ↓ k j move the cursor and wrap over the eight rows", () => {
    const start = startCustomize([]);
    expect(press(start, "ArrowUp").state.cursor).toBe(7);
    expect(press(start, "ArrowDown", "j", "ArrowDown").state.cursor).toBe(3);
    expect(press(start, "k", "k").state.cursor).toBe(6);
    expect(press(start, ...Array(8).fill("ArrowDown")).state.cursor).toBe(0);
  });

  it("Space toggles the row under the cursor", () => {
    const start = startCustomize(DEFAULT_FOOTER_ITEMS);
    const off = press(start, "ArrowDown", "Space").state;
    expect(off.picked).not.toContain("settings");
    expect(press(off, "Space").state.picked).toContain("settings");
  });

  it("a number key moves the cursor there and toggles that row; 9 does nothing", () => {
    const { state } = press(startCustomize([]), "8");
    expect(state).toEqual({ cursor: 7, picked: ["resets"] });
    expect(press(state, "8").state.picked).toEqual([]);
    const nine = customizeKey(state, "9");
    expect(nine.handled).toBe(false);
    expect(nine.state).toBe(state);
  });

  it("Enter saves, Esc cancels, other keys are not taken", () => {
    expect(customizeKey(startCustomize([]), "Enter").done).toBe("save");
    expect(customizeKey(startCustomize([]), "Escape").done).toBe("cancel");
    expect(customizeKey(startCustomize([]), "Alt+Escape").done).toBe("cancel");
    expect(customizeKey(startCustomize([]), "x").handled).toBe(false);
    expect(customizeKey(startCustomize([]), "Ctrl+G").handled).toBe(false);
  });

  it("toggling two rows and saving writes the rest in the rows' order", () => {
    const { state, done } = press(startCustomize(DEFAULT_FOOTER_ITEMS), "1", "3", "Enter");
    expect(done).toBe("save");
    expect(customizeSave(state)).toEqual(["settings", "context", "limit_5h", "limit_weekly", "limit_other"]);
  });

  it("nothing picked saves an explicit none (composer_footer.go:168-170)", () => {
    const all = startCustomize(ids);
    const none = press(all, "1", "2", "3", "4", "5", "6", "7", "8").state;
    expect(none.picked).toEqual([]);
    expect(customizeSave(none)).toEqual(["none"]);
  });

  it("a click moves the cursor to the row and toggles it", () => {
    const state = customizeClick(startCustomize([]), 4);
    expect(state).toEqual({ cursor: 4, picked: ["limit_5h"] });
    expect(customizeClick(state, 99)).toBe(state);
  });
});
