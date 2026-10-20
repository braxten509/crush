// The composer's `/` menu (owner, 2026-10-20): every command the Ctrl+P
// palette has, System then User then MCP, in one list. The palette stays as
// it is; this is the way in from the typing box. Loaded on first use, so the
// composer's store never imports the palette's modules at start.
import { initProject, summarize, toggleYolo } from "./actions.js";
import { entryFilter, systemEntries } from "./entries.js";
import { mcpEntries, userEntries } from "./run.js";
import { readCommands, watchCommands } from "./store.svelte.js";

let system = null;

// Rows for the popup: {text (shown), match (what the filter reads), shortcut, entry}.
export function slashRows() {
  system ??= systemEntries({ summarize, toggleYolo, initProject });
  const entries = [...system.filter((entry) => entry.shown()), ...userEntries(), ...mcpEntries()];
  return entries.map((entry) => ({ text: entry.label, match: entryFilter(entry), shortcut: entry.shortcut ?? "", entry }));
}

// Keeps the User and MCP lists fresh while the menu is open (MCP changes
// re-read them); returns the stop.
export function watchSlash() {
  return watchCommands();
}

// The User and MCP lists, read now; resolves when they arrived.
export function readSlash() {
  return readCommands();
}
