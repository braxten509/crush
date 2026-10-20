// Puts a resolved theme on :root in one frame (desktop-design-5-6 3.1 rule 1,
// 3.3): every token, then color-scheme.
import { readable } from "./contrast.js";
import { isDark, tokensOf } from "./tokens.js";

// ThemeView → {tokens, dark}
export function themeTokens(view) {
  return { tokens: readable(tokensOf(view)).tokens, dark: isDark(view) };
}

export function applyTokens(tokens, dark) {
  const root = globalThis.document?.documentElement;
  if (!root) return;
  for (const [name, value] of Object.entries(tokens)) root.style.setProperty(name, value);
  root.style.colorScheme = dark ? "dark" : "light";
  root.dataset.mode = dark ? "dark" : "light";
}

export function applyView(view) {
  const { tokens, dark } = themeTokens(view);
  applyTokens(tokens, dark);
  return { tokens, dark };
}
