// A resolved theme → the app's color tokens (desktop-design-5-6 3.1). Direct
// tokens copy a role; derived tokens mix two roles in sRGB; picked tokens
// take whichever of two roles reads better on a third. A derived token keeps
// the root built-in's exact value while every role it comes from still holds
// that built-in's value (3.1 rule 3), so the approved picture stays exact.

// The 55 roles in the terminal's order (internal/ui/styles/palette.go:47-104).
export const ROLES = [
  "primary", "secondary", "accent", "keyword", "fg_base", "fg_subtle", "fg_more_subtle", "fg_most_subtle",
  "bg_base", "bg_most_visible", "bg_less_visible", "bg_least_visible", "on_primary", "separator", "destructive",
  "error", "warning", "warning_subtle", "attention", "busy", "info", "info_more_subtle", "info_most_subtle",
  "success", "success_more_subtle", "success_most_subtle", "diff_insert_fg", "diff_insert_code_bg",
  "diff_insert_gutter_bg", "diff_delete_fg", "diff_delete_code_bg", "diff_delete_gutter_bg", "yolo", "plan",
  "plan_more_subtle", "button", "button_subtle", "button_inactive", "button_hovered",
  "ansi_black", "ansi_red", "ansi_green", "ansi_yellow", "ansi_blue", "ansi_magenta", "ansi_cyan", "ansi_white",
  "ansi_bright_black", "ansi_bright_red", "ansi_bright_green", "ansi_bright_yellow", "ansi_bright_blue",
  "ansi_bright_magenta", "ansi_bright_cyan", "ansi_bright_white",
];

export const DEFAULT_THEME = "scholars-edition";

// The edition's roles that derived tokens read (3.7), for the exact-table rule.
const EDITION_ROLES = {
  fg_base: "#0f0f0f",
  bg_base: "#f9f8ef",
  primary: "#e52b0e",
  warning: "#e0a523",
  plan: "#2f5d73",
};

// The edition's exact derived values (3.7: app.css's values).
const EXACT = {
  "scholars-edition": {
    "--rule-frame": "#aaa9a3",
    "--rule-faint": "#cfcec7",
    "--rule-control": "#908f89",
    "--rule-table": "#7a7973",
    "--rule-table-inner": "#aaa9a3",
    "--vermilion-wash": "#faf5f0",
    "--meter": "#454640",
    "--meter-edge": "#bdbdb9",
    "--selection": "#e6d9a8",
    "--skill-tint": "#ece6d2",
  },
};
const ROOT_ROLES = { "scholars-edition": EDITION_ROLES };

// Direct tokens: token → role.
const DIRECT = {
  "--paper": "bg_base",
  "--paper-raised": "bg_least_visible",
  "--band": "bg_less_visible",
  "--ink": "fg_base",
  "--slate": "fg_more_subtle",
  "--link": "info",
  "--rule": "separator",
  "--mark": "fg_most_subtle",
  "--figure": "fg_subtle",
  "--vermilion": "primary",
  "--vermilion-fill": "button",
  "--vermilion-ink": "accent",
  "--amber": "warning",
  "--meter-track": "bg_most_visible",
  "--error-ink": "error",
  "--amber-ink": "warning_subtle",
  "--diff-add-ink": "diff_insert_fg",
  "--diff-add-bg": "diff_insert_code_bg",
  "--diff-add-gutter": "diff_insert_gutter_bg",
  "--diff-del-bg": "diff_delete_code_bg",
  "--diff-del-gutter": "diff_delete_gutter_bg",
  "--choice": "secondary",
  "--on-accent": "on_primary",
  "--keyword": "keyword",
  "--attention": "attention",
  "--busy": "busy",
  "--info-strong": "info_more_subtle",
  "--info-soft": "info_most_subtle",
  "--plan": "plan",
  "--control-rest": "button_subtle",
  "--control-hover": "button_hovered",
  "--control-disabled": "button_inactive",
  "--disabled-band": "bg_most_visible",
};

// Derived tokens: token → [a, b, t] for mix(a, b, t).
const DERIVED = {
  "--rule-frame": ["fg_base", "bg_base", 0.338],
  "--rule-faint": ["fg_base", "bg_base", 0.18],
  "--rule-control": ["fg_base", "bg_base", 0.45],
  "--rule-table": ["fg_base", "bg_base", 0.545],
  "--rule-table-inner": ["fg_base", "bg_base", 0.338],
  "--vermilion-wash": ["primary", "bg_base", 0.04],
  "--meter": ["fg_base", "bg_base", 0.77],
  "--meter-edge": ["fg_base", "bg_base", 0.25],
  "--selection": ["warning", "bg_base", 0.35],
  "--skill-tint": ["plan", "bg_base", 0.12],
};

// Dark mode (dark.js) draws these rules a quarter softer: on graphite paper
// the light edition's amounts read loud (owner, 2026-10-20). Control borders
// keep their strength.
const DARK_SOFTER = new Set(["--rule-frame", "--rule-faint", "--rule-table-inner", "--meter-edge"]);
const DARK_RULE_SCALE = 0.75;

// The eight code classes (3.1) → their syntax keys.
const SYNTAX = {
  "--hl-keyword": "keyword",
  "--hl-string": "literal_string",
  "--hl-number": "literal_number",
  "--hl-comment": "comment",
  "--hl-function": "name_function",
  "--hl-type": "keyword_type",
  "--hl-attr": "name_attribute",
  "--hl-meta": "comment_preproc",
};

const ANSI = ROLES.slice(ROLES.indexOf("ansi_black"));

export function parseHex(hex) {
  const m = /^#?([0-9a-f]{6})$/i.exec(String(hex ?? "").trim());
  if (!m) return null;
  const n = parseInt(m[1], 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

export function toHex([r, g, b]) {
  const c = (v) => Math.max(0, Math.min(255, Math.round(v))).toString(16).padStart(2, "0");
  return `#${c(r)}${c(g)}${c(b)}`;
}

// mix(a, b, t): per channel round(b + (a − b) × t).
export function mix(a, b, t) {
  const x = parseHex(a);
  const y = parseHex(b);
  if (!x || !y) return a ?? b;
  return toHex(x.map((v, i) => y[i] + (v - y[i]) * t));
}

// WCAG relative luminance and contrast ratio.
export function luminance(hex) {
  const rgb = parseHex(hex);
  if (!rgb) return 0;
  const [r, g, b] = rgb.map((v) => {
    const s = v / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

export function contrast(a, b) {
  const x = luminance(a);
  const y = luminance(b);
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
}

// ThemeView → {"--paper": "#…", …} before the contrast pass.
export function tokensOf(view) {
  const resolved = view?.resolved ?? {};
  const role = (name) => resolved[name] ?? "";
  const tokens = {};
  for (const [token, name] of Object.entries(DIRECT)) if (role(name)) tokens[token] = role(name);
  const exact = EXACT[view?.root] ?? null;
  const rootRoles = ROOT_ROLES[view?.root] ?? null;
  const softer = view?.darkened === true;
  for (const [token, [a, b, t]] of Object.entries(DERIVED)) {
    const untouched = exact && rootRoles && [a, b].every((name) => String(role(name)).toLowerCase() === rootRoles[name]);
    const amount = softer && DARK_SOFTER.has(token) ? t * DARK_RULE_SCALE : t;
    tokens[token] = untouched ? exact[token] : mix(role(a), role(b), amount);
  }
  const fg = role("fg_base");
  const bg = role("bg_base");
  const warning = role("warning");
  tokens["--on-amber"] = contrast(fg, warning) >= contrast(bg, warning) ? fg : bg;
  const syntax = view?.syntax ?? {};
  for (const [token, key] of Object.entries(SYNTAX)) if (syntax[key]) tokens[token] = syntax[key];
  const code = view?.markdown?.code || role("destructive");
  if (code) tokens["--code-inline"] = code;
  ANSI.forEach((name, i) => {
    if (role(name)) tokens[`--ansi-${i}`] = role(name);
  });
  const paper = parseHex(bg);
  if (paper) tokens["--scrim"] = `rgb(${paper.join(" ")} / 0.92)`;
  return tokens;
}

// Rec. 709 luma of bg_base below half (changes.go:575-581); the engine also sends `dark`.
export function isDark(view) {
  if (typeof view?.dark === "boolean") return view.dark;
  const rgb = parseHex(view?.resolved?.bg_base);
  if (!rgb) return false;
  const [r, g, b] = rgb.map((v) => v / 255);
  return 0.2126 * r + 0.7152 * g + 0.0722 * b < 0.5;
}
