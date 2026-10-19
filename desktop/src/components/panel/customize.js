// The Customize Composer form (SIDE-016 to SIDE-024): the terminal's
// multi-choice question as composer_footer.go:142-181 opens it, with its
// words, its keys (internal/ui/dialog/questionform/multi.go:47-124,
// choice_base.go:105-257) and its help (multi.go:157-166). Pure:
// CustomizeForm keeps the state and does what a key's answer asks.
//
//   CUSTOMIZE                   the form's label, question and description
//   CUSTOMIZE_HELP              its help entries (the help sheet's FORM group)
//   CUSTOMIZE_COLLAPSED         Tab's words while the form is collapsed (form_draw.go:48)
//   startCustomize(items)       {cursor, picked}: the saved choices picked, the cursor on the first row
//   customizeKey(state, combo)  → {state, done, handled}; done: null, "save" or "cancel"
//   customizeClick(state, i)    a click on row i: the cursor moves there and the row toggles
//   customizeSave(state)        what Enter saves (footerAnswer: the rows' order, or ["none"])
//
// The terminal's form also has a free-text row and notes (alt+n), which this
// answer never reads (composer_footer.go:158-178 takes only the picked ids),
// so the desktop form leaves both out: the cursor wraps over the eight rows.
import { FOOTER_CHOICES, footerAnswer } from "../../lib/footer.js";

export const CUSTOMIZE = {
  label: "Composer",
  text: "What should show under the composer?",
  description: "Space toggles a row, Enter saves. Usage limits show only for agent CLIs that report them.",
};

// The number keys reach at most 9 rows (single.go:134-143).
const QUICK = Math.min(9, FOOTER_CHOICES.length);

export const CUSTOMIZE_HELP = [
  { key: "↑", action: "up" },
  { key: "↓", action: "down" },
  { key: "space", action: "toggle" },
  { key: `1-${QUICK}`, action: "quick select" },
  { key: "enter", action: "done" },
  { key: "esc", action: "exit" },
];

export const CUSTOMIZE_COLLAPSED = "answer questions";

const UP = new Set(["ArrowUp", "k"]);
const DOWN = new Set(["ArrowDown", "j"]);
const CLOSE = new Set(["Escape", "Alt+Escape"]); // dialog.CloseKey (internal/ui/dialog/dialog.go:14-17)

export function startCustomize(items) {
  const list = Array.isArray(items) ? items : [];
  return { cursor: 0, picked: FOOTER_CHOICES.filter((c) => list.includes(c.id)).map((c) => c.id) };
}

function toggled(picked, id) {
  return picked.includes(id) ? picked.filter((x) => x !== id) : [...picked, id];
}

function at(state, index) {
  return { cursor: index, picked: toggled(state.picked, FOOTER_CHOICES[index].id) };
}

export function customizeKey(state, combo) {
  const count = FOOTER_CHOICES.length;
  const quick = /^[1-9]$/u.test(combo) ? Number(combo) : 0;
  if (quick > 0) {
    if (quick > QUICK) return { state, done: null, handled: false };
    return { state: at(state, quick - 1), done: null, handled: true };
  }
  if (CLOSE.has(combo)) return { state, done: "cancel", handled: true };
  if (combo === "Enter") return { state, done: "save", handled: true };
  if (combo === "Space") return { state: at(state, state.cursor), done: null, handled: true };
  if (UP.has(combo)) return { state: { ...state, cursor: (state.cursor - 1 + count) % count }, done: null, handled: true };
  if (DOWN.has(combo)) return { state: { ...state, cursor: (state.cursor + 1) % count }, done: null, handled: true };
  return { state, done: null, handled: false };
}

export function customizeClick(state, index) {
  return index >= 0 && index < FOOTER_CHOICES.length ? at(state, index) : state;
}

export function customizeSave(state) {
  return footerAnswer(state.picked);
}
