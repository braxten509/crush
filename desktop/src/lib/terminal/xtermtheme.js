// The terminal widget's colors and type, read from the desktop's theme
// tokens (desktop-design-5-6 2.25 LIST-019, 3.1, look 5.18): background
// --paper, text and cursor --ink, selection --selection with the text in its
// own color (no selection foreground, 9.4 D14), and the 16 program colors
// --ansi-0 to --ansi-15. The session host recolors its shell tabs the same
// way when the theme changes (internal/ui/sessionhost/terminal.go:61-80).

const ANSI = [
  "black",
  "red",
  "green",
  "yellow",
  "blue",
  "magenta",
  "cyan",
  "white",
  "brightBlack",
  "brightRed",
  "brightGreen",
  "brightYellow",
  "brightBlue",
  "brightMagenta",
  "brightCyan",
  "brightWhite",
];

// read(name) gives a token's value ("--paper" → "#f9f8ef").
export function xtermTheme(read) {
  const theme = {
    background: read("--paper"),
    foreground: read("--ink"),
    cursor: read("--ink"),
    // The letter under the block cursor: the paper, as on an inverted cell.
    cursorAccent: read("--paper"),
    selectionBackground: read("--selection"),
    selectionInactiveBackground: read("--selection"),
    // The scrollbar as 5.0's: a thumb in --rule-control, no track.
    scrollbarSliderBackground: read("--rule-control"),
    scrollbarSliderHoverBackground: read("--rule-control"),
    scrollbarSliderActiveBackground: read("--rule-control"),
  };
  ANSI.forEach((name, i) => {
    theme[name] = read(`--ansi-${i}`);
  });
  return theme;
}

// The theme as the page's tokens are now.
export function themeFromPage(root = document.documentElement) {
  const style = getComputedStyle(root);
  return xtermTheme((name) => style.getPropertyValue(name).trim());
}

// Prompt themes (powerlevel10k, starship) draw their arrows and icons from
// the Nerd Fonts' private-use letters, which Inconsolata lacks and Chromium
// never finds on its own (owner, 2026-10-20): these installed faces fill in,
// letter by letter, before the generic family.
export const NERD_FALLBACK = '"Symbols Nerd Font Mono", "MesloLGS NF", "MesloLGS Nerd Font Mono", "FantasqueSansM Nerd Font Mono"';

// --face-mono with the Nerd faces put in ahead of its generic families.
export function terminalFamily(face) {
  const names = (face || "monospace").split(",").map((name) => name.trim());
  const generic = names.findIndex((name) => /^(ui-monospace|monospace)$/.test(name));
  const at = generic === -1 ? names.length : generic;
  return [...names.slice(0, at), NERD_FALLBACK, ...names.slice(at)].join(", ");
}

// The text size: --face-mono at 18/24 picture pixels, never below 11 real
// pixels (look 5.18). zoom is the root size over 16 (lib/zoom.js); xterm takes
// the line height as a multiple of the size, at least 1.
export function xtermType(zoom, root = document.documentElement) {
  const fontSize = Math.max(11, 18 * zoom);
  return {
    fontFamily: terminalFamily(getComputedStyle(root).getPropertyValue("--face-mono").trim()),
    fontSize,
    lineHeight: Math.max(1, (24 * zoom) / fontSize),
  };
}

// The page's zoom: the root's font size over 16 (app.css: 16px × --zoom).
export function pageZoom(root = document.documentElement) {
  return parseFloat(getComputedStyle(root).fontSize) / 16 || 1;
}
