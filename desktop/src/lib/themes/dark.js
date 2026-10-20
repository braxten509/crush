// Dark mode (owner, 2026-10-20: "switches the cream and black colors"). A
// light theme's view turned dark: paper and ink trade places, every gray
// between them keeps its place on the way from paper to ink (so a band that
// was a little darker than the paper is now a little lighter), and the
// program colors are lifted until they read on the dark band. Colored roles
// (vermilion, links, errors, …) stay; the contrast pass (contrast.js) moves
// any that no longer read. A theme that is already dark is left as it is.
// Pure: returns a new view.
import { contrast, isDark, luminance, mix } from "./tokens.js";

// The roles that are grays between paper and ink.
const NEUTRAL = [
  "fg_base", "fg_subtle", "fg_more_subtle", "fg_most_subtle",
  "bg_base", "bg_most_visible", "bg_less_visible", "bg_least_visible",
  "separator", "button_subtle", "button_inactive", "button_hovered",
];

// The program colors' light and dark ends trade places too.
const ANSI_SWAP = [["ansi_black", "ansi_white"], ["ansi_bright_black", "ansi_bright_white"]];

const ANSI = [
  "ansi_black", "ansi_red", "ansi_green", "ansi_yellow", "ansi_blue", "ansi_magenta", "ansi_cyan", "ansi_white",
  "ansi_bright_black", "ansi_bright_red", "ansi_bright_green", "ansi_bright_yellow", "ansi_bright_blue",
  "ansi_bright_magenta", "ansi_bright_cyan", "ansi_bright_white",
];

// Diff backgrounds → [role, the hue they tint, how much of it].
const DIFF_TINTS = [
  ["diff_insert_code_bg", "diff_insert_fg", 0.16],
  ["diff_insert_gutter_bg", "diff_insert_fg", 0.24],
  ["diff_delete_code_bg", "diff_delete_fg", 0.16],
  ["diff_delete_gutter_bg", "diff_delete_fg", 0.24],
];

// Where a gray sits from paper (0) to ink (1), by luminance.
function place(color, paper, ink) {
  const span = luminance(ink) - luminance(paper);
  if (!span) return 0;
  return Math.min(1, Math.max(0, (luminance(color) - luminance(paper)) / span));
}

// Colors nearly as dark as the ink on light paper (the edition's navy links
// and choice marks) are ink-like: on dark paper they become nearly as light as
// the new ink, keeping a trace of their hue.
const INK_LIKE = 10;

function inkLike(color, paper, ink) {
  for (let step = 0; step <= 50; step++) {
    const value = step === 0 ? color : mix(ink, color, step * 0.02);
    if (contrast(value, paper) >= INK_LIKE) return value;
  }
  return ink;
}

// Lifts a color toward white until it reads at 4.5:1 on the background.
function lift(color, background) {
  for (let step = 0; step <= 50; step++) {
    const value = step === 0 ? color : mix("#ffffff", color, step * 0.02);
    if (contrast(value, background) >= 4.5) return value;
  }
  return "#ffffff";
}

// ThemeView → the same view on dark paper (or the view itself when dark).
export function darkView(view) {
  if (!view?.resolved || isDark(view)) return view;
  const old = view.resolved;
  const paper = old.fg_base;
  const ink = old.bg_base;
  if (!paper || !ink) return view;
  const resolved = { ...old };
  for (const role of NEUTRAL) {
    if (!old[role]) continue;
    resolved[role] = mix(ink, paper, place(old[role], old.bg_base, old.fg_base));
  }
  for (const role of Object.keys(old)) {
    if (NEUTRAL.includes(role) || ANSI.includes(role) || !/^#[0-9a-f]{6}$/i.test(String(old[role]))) continue;
    if (contrast(old[role], old.bg_base) >= INK_LIKE) resolved[role] = inkLike(old[role], paper, ink);
  }
  for (const [a, b] of ANSI_SWAP) {
    if (old[a] && old[b]) [resolved[a], resolved[b]] = [old[b], old[a]];
  }
  // The light diff tints would glare on dark paper: the same hue, faint.
  for (const [role, hue, amount] of DIFF_TINTS) {
    if (old[role] && old[hue]) resolved[role] = mix(old[hue], paper, amount);
  }
  const band = resolved.bg_less_visible || paper;
  for (const role of ANSI) if (resolved[role]) resolved[role] = lift(resolved[role], band);
  return { ...view, resolved, dark: true };
}

// The stored choice ("light", "dark" or "system") → whether to show dark.
export function wantsDark(mode, systemDark) {
  return mode === "dark" || (mode === "system" && systemDark === true);
}
