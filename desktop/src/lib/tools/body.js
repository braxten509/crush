// How much of a tool's body shows (CHAT-052, CHAT-057, CHAT-113): 10 lines
// and "… (N lines hidden) [click or space to expand]" until the tool is
// opened; never a hidden count of one line, so 11 lines show whole. Ports of
// chat/tools.go:17-31 (collapsedMaxLines) and tool_output.go:16-104,162-251
// (the plain, code, diff and Markdown bodies).
import { stripCursorControl } from "../render/ansi.js";
import { normalizeSpace } from "./go.js";

// responseContextHeight.
export const MAX_LINES = 10;

export function collapsedMaxLines(total) {
  return total <= MAX_LINES + 1 ? total : MAX_LINES;
}

// assistantMessageTruncateFormat (assistant.go:20).
export function hiddenText(n) {
  return `… (${n} lines hidden) [click or space to expand]`;
}

// The lines that show of total: {shown, hidden}.
export function collapse(total, expanded) {
  const shown = expanded ? total : collapsedMaxLines(total);
  return { shown, hidden: total - shown };
}

// toolOutputPlainContent: spaces normalized, cursor moves and \r redraws
// applied (colors kept for the color renderer), then the lines that show.
export function plainLines(text, expanded) {
  const lines = stripCursorControl(normalizeSpace(text)).split("\n");
  const { shown, hidden } = collapse(lines.length, expanded);
  return { lines: lines.slice(0, shown), hidden };
}

// toolOutputCodeContent: the lines that show, the first line's number
// (offset + 1) and the gutter's digits, from the last shown number.
export function codeLines(text, offset, expanded) {
  const lines = normalizeSpace(text).split("\n");
  const { shown, hidden } = collapse(lines.length, expanded);
  const start = Math.max(0, Math.floor(Number(offset) || 0));
  return { lines: lines.slice(0, shown), first: start + 1, digits: String(shown + start).length, hidden };
}

// toolOutputMarkdownContent counts the drawn lines; the desktop counts the
// Markdown's own lines and draws the ones that show.
export function markdownLines(text, expanded) {
  const lines = normalizeSpace(text).split("\n");
  const { shown, hidden } = collapse(lines.length, expanded);
  return { text: lines.slice(0, shown).join("\n"), hidden };
}

// A diff body: the rows that show of total formatted rows (the diff's own
// rows, hunk headings included).
export function diffShown(total, maxLines) {
  if (!(maxLines > 0) || total <= maxLines + 1) return { shown: total, hidden: 0 };
  return { shown: maxLines, hidden: total - maxLines };
}
