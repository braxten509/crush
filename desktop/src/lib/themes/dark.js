// Dark mode, Graphite (owner, 2026-10-20: "switches the cream and black
// colors", then "it loses the soft vibe" and the Graphite pick). A light
// theme's view turned dark: soft graphite paper with no color cast, the light
// paper dimmed to a soft linen ink (about 11:1, not 18:1), every gray placed
// between them by how light it looks, so quiet text stays quiet and rules
// stay faint, and the colors kept to their hue but calmer. The contrast pass
// (contrast.js) moves any that no longer read. A theme that is already dark
// is left as it is. Pure: returns a new view.
import { contrast, isDark, mix, parseHex, toHex } from "./tokens.js";

// The paper: graphite, gray with no cast. The ink: the light paper's own
// hue, nearly gray, this light (OKLab).
export const PAPER = "#232321";
const INK_LIGHTNESS = 0.885;
const INK_CHROMA = 0.01;

// Surfaces (bands, controls) sit this much farther from the paper than
// their place alone gives, or the bands vanish on dark paper.
const SURFACE_GAIN = 1.5;

// Colors: at least this light, this much of their saturation.
const COLOR_LIGHTNESS = 0.65;
const COLOR_CHROMA = 0.7;
// Colors nearly as dark as the ink on light paper (the edition's navy links
// and choice marks) read as ink: on dark paper they are light, faintly hued.
const INK_LIKE = 10;
const INK_LIKE_LIGHTNESS = 0.8;
const INK_LIKE_CHROMA = 0.045;
// Below this saturation a color is a gray.
const GRAY_CHROMA = 0.03;

// The roles that are grays between paper and ink.
const NEUTRAL = [
  "fg_base", "fg_subtle", "fg_more_subtle", "fg_most_subtle",
  "bg_base", "bg_most_visible", "bg_less_visible", "bg_least_visible",
  "separator", "button_subtle", "button_inactive", "button_hovered",
];
const SURFACE = (role) => role.startsWith("bg_") || role.startsWith("button_");

// The program colors' light and dark ends trade places.
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

// -- OKLCH, where lightness steps look even --

const linear = (v) => (v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4);
const encode = (v) => 255 * (v <= 0.0031308 ? 12.92 * v : 1.055 * v ** (1 / 2.4) - 0.055);

// "#rrggbb" → [lightness, chroma, hue in degrees]
export function oklch(hex) {
  const [r, g, b] = parseHex(hex).map((v) => linear(v / 255));
  const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
  const L = 0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s;
  const A = 1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s;
  const B = 0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s;
  return [L, Math.hypot(A, B), ((Math.atan2(B, A) * 180) / Math.PI + 360) % 360];
}

// [lightness, chroma, hue] → "#rrggbb", losing chroma until it fits sRGB.
export function fromOklch([L, C, h]) {
  const angle = (h * Math.PI) / 180;
  for (let c = C; ; c = Math.max(0, c - 0.002)) {
    const A = c * Math.cos(angle);
    const B = c * Math.sin(angle);
    const l = (L + 0.3963377774 * A + 0.2158037573 * B) ** 3;
    const m = (L - 0.1055613458 * A - 0.0638541728 * B) ** 3;
    const s = (L - 0.0894841775 * A - 1.291485548 * B) ** 3;
    const rgb = [
      4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
      -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
      -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s,
    ];
    if (c === 0 || rgb.every((v) => v >= -0.0005 && v <= 1.0005)) {
      return toHex(rgb.map((v) => encode(Math.min(1, Math.max(0, v)))));
    }
  }
}

const isHex = (value) => /^#[0-9a-f]{6}$/i.test(String(value));

// Lifts a color toward white until it reads at 4.5:1 on the background.
function lift(color, background) {
  for (let step = 0; step <= 50; step++) {
    const value = step === 0 ? color : mix("#ffffff", color, step * 0.02);
    if (contrast(value, background) >= 4.5) return value;
  }
  return "#ffffff";
}

// The same hue, calmer: lighter where it was dark, less saturated.
function calm(color) {
  const [L, C, h] = oklch(color);
  return fromOklch([Math.max(L, COLOR_LIGHTNESS), C * COLOR_CHROMA, h]);
}

// ThemeView → the same view on graphite paper (or the view itself when dark).
export function darkView(view) {
  if (!view?.resolved || isDark(view)) return view;
  const old = view.resolved;
  if (!isHex(old.fg_base) || !isHex(old.bg_base)) return view;
  const [lightPaper, paperChroma, paperHue] = oklch(old.bg_base);
  const [lightInk] = oklch(old.fg_base);
  const [darkPaper, grayChroma, grayHue] = oklch(PAPER);
  const ink = fromOklch([INK_LIGHTNESS, Math.min(paperChroma, INK_CHROMA), paperHue]);
  const [darkInk] = oklch(ink);
  // Where a color sits from the light paper (0) to the light ink (1), by
  // how light it looks; one lighter than the paper (a raised surface) sits
  // as far on the other side, so it is a step lighter here too.
  const span = lightPaper - lightInk;
  const placeOf = (color, gain = 1) => {
    if (!span) return 0;
    return Math.min(1, (Math.abs(lightPaper - oklch(color)[0]) / span) * gain);
  };
  const gray = (t) => fromOklch([darkPaper + (darkInk - darkPaper) * t, grayChroma, grayHue]);

  const resolved = { ...old };
  for (const role of NEUTRAL) {
    if (isHex(old[role])) resolved[role] = gray(placeOf(old[role], SURFACE(role) ? SURFACE_GAIN : 1));
  }
  resolved.bg_base = PAPER;
  resolved.fg_base = ink;
  const tinted = new Set(DIFF_TINTS.map(([role]) => role));
  for (const role of Object.keys(old)) {
    if (NEUTRAL.includes(role) || ANSI.includes(role) || tinted.has(role) || !isHex(old[role])) continue;
    const [, C, h] = oklch(old[role]);
    if (C < GRAY_CHROMA) resolved[role] = gray(placeOf(old[role]));
    else if (contrast(old[role], old.bg_base) >= INK_LIKE) resolved[role] = fromOklch([INK_LIKE_LIGHTNESS, Math.min(C, INK_LIKE_CHROMA), h]);
    else resolved[role] = calm(old[role]);
  }
  // Text and icons on the red fill: the paper, as on the light edition.
  if (old.on_primary) resolved.on_primary = PAPER;
  // The light diff tints would glare on dark paper: the same hue, faint.
  for (const [role, hue, amount] of DIFF_TINTS) {
    if (old[role] && resolved[hue]) resolved[role] = mix(resolved[hue], PAPER, amount);
  }
  for (const [a, b] of ANSI_SWAP) {
    if (old[a] && old[b]) [resolved[a], resolved[b]] = [old[b], old[a]];
  }
  const band = resolved.bg_less_visible || PAPER;
  for (const role of ANSI) {
    if (!isHex(resolved[role])) continue;
    const color = oklch(resolved[role])[1] < GRAY_CHROMA ? resolved[role] : calm(resolved[role]);
    resolved[role] = lift(color, band);
  }
  return { ...view, resolved, dark: true, darkened: true };
}

// The stored choice ("light", "dark" or "system") → whether to show dark.
export function wantsDark(mode, systemDark) {
  return mode === "dark" || (mode === "system" && systemDark === true);
}
