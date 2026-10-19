// What a `!` shell item shows (CHAT-038 to CHAT-043). Port of
// chat/shell.go:22-25,212-362.
import { stripCursorControl } from "../render/ansi.js";

export const SHELL_MAX_LINES = 10;
// Shift+←/→ and H/L move the output this many characters.
export const SHELL_STEP = 5;

// The command on one line: newlines as spaces, tabs as four spaces.
export function shellCommandLine(command) {
  return String(command ?? "").replaceAll("\n", " ").replaceAll("\t", "    ");
}

// The mark after the command once it ended: "(Canceled)" for 130 (also an
// Esc while it ran), "(exit N)" for another non-zero code.
export function exitMark(exitCode, running) {
  if (running || exitCode === null || exitCode === undefined || exitCode === 0) return null;
  if (exitCode === 130) return { text: "(Canceled)", tone: "cancelled" };
  return { text: `(exit ${exitCode})`, tone: "error" };
}

// Trailing spaces, newlines and bare color resets go first: programs end
// with "\x1b[0m\n", which a plain trim misses.
export function trimOutput(output) {
  let raw = String(output ?? "");
  for (;;) {
    let trimmed = raw.replace(/[ \t\r\n]+$/, "");
    if (trimmed.endsWith("\x1b[0m")) trimmed = trimmed.slice(0, -4);
    if (trimmed === raw) return raw;
    raw = trimmed;
  }
}

// The first count lines of s (without the newline after them).
export function firstLines(s, count) {
  if (count <= 0) return "";
  let end = 0;
  for (let i = 0; i < count; i++) {
    const next = s.indexOf("\n", end);
    if (next < 0) return s;
    if (i === count - 1) return s.slice(0, next);
    end = next + 1;
  }
  return s;
}

// The last count lines of s.
export function lastLines(s, count) {
  if (count <= 0) return "";
  let start = s.length;
  for (let i = 0; i < count; i++) {
    const previous = start > 0 ? s.lastIndexOf("\n", start - 1) : -1;
    if (previous < 0) return s;
    start = previous;
  }
  return s.slice(start + 1);
}

// The item's parts:
//   command  the header's command line
//   mark     null or {text, tone}: the exit mark
//   lines    the output lines to draw (escape codes kept for the color
//            renderer; cursor moves and \r redraws already applied)
//   above    "… N earlier lines" while streaming and cut, else ""
//   below    "… N more lines" when done and cut, else ""
//   spinner  the activity mark shows (under the output while it runs)
export function shellView({ command, output, exitCode, running, expanded }) {
  const view = { command: shellCommandLine(command), mark: exitMark(exitCode, running), lines: [], above: "", below: "", spinner: false };
  const full = String(output ?? "");
  if (running && full.length === 0) return { ...view, spinner: true };
  if (full.length === 0) return view;
  let raw = trimOutput(full);
  if (raw === "") return view;
  const total = raw.split("\n").length;
  let cut = 0;
  if (!expanded && total > SHELL_MAX_LINES) {
    cut = total - SHELL_MAX_LINES;
    raw = running ? lastLines(raw, SHELL_MAX_LINES) : firstLines(raw, SHELL_MAX_LINES);
  }
  view.lines = stripCursorControl(raw).split("\n");
  if (cut > 0 && running) view.above = `… ${cut} earlier lines`;
  if (cut > 0 && !running) view.below = `… ${cut} more lines`;
  view.spinner = running;
  return view;
}
