// The folder picker's rules (desktop-design-5-6 2.21; LIST-007, LIST-038 to
// LIST-042), ported from the terminal's new-session box
// (internal/ui/sessionhost/picker.go). A picker is a plain object the store
// keeps in $state:
//   {kind: "session" | "terminal", base, home, input, cursor, sel, matches, recent, err, version}
// input is the typed path and cursor a place in it (a string index); sel is
// the picked row of the matches followed by the recent folders, -1 the typed
// path. Matches come from listing a folder (VIEW's dir_list), which is async:
// matchFolders() answers them for the path it was given, and the caller keeps
// them only while version still names that input (edited() bumps it).

export const PICKER_ROWS = 5;

const byName = (a, b) => (a < b ? -1 : a > b ? 1 : 0);

// filepath.Clean for "/" paths.
export function clean(path) {
  if (path === "") return ".";
  const rooted = path.startsWith("/");
  const out = [];
  for (const part of path.split("/")) {
    if (part === "" || part === ".") continue;
    if (part === "..") {
      if (out.length > 0 && out[out.length - 1] !== "..") out.pop();
      else if (!rooted) out.push("..");
      continue;
    }
    out.push(part);
  }
  const joined = out.join("/");
  if (rooted) return `/${joined}`;
  return joined === "" ? "." : joined;
}

// filepath.Join: the parts that are not empty, joined and cleaned.
export function join(...parts) {
  const kept = parts.filter((part) => part !== "");
  return kept.length === 0 ? "" : clean(kept.join("/"));
}

// filepath.Dir: everything before the last "/", cleaned ("." when none).
export function dirOf(path) {
  return clean(path.slice(0, path.lastIndexOf("/") + 1));
}

// filepath.Base: the last name, trailing slashes ignored.
export function baseOf(path) {
  if (path === "") return ".";
  const trimmed = path.replace(/\/+$/u, "");
  if (trimmed === "") return "/";
  return trimmed.slice(trimmed.lastIndexOf("/") + 1);
}

export function withSlash(path) {
  return path.endsWith("/") ? path : `${path}/`;
}

// home.Short (internal/home/home.go:34-45): the home folder written "~".
export function short(path, home) {
  if (!home || !path.startsWith(home)) return path;
  if (path.length === home.length) return "~";
  if (path[home.length] !== "/") return path;
  return join("~", path.slice(home.length));
}

// expand (picker.go:67-79): "~" is the home folder and a relative path
// starts from base.
export function expand(typed, base, home) {
  if (typed === "~") return home;
  if (typed.startsWith("~/")) return withSlash(home) + typed.slice(2);
  if (typed === "" || typed.startsWith("/")) return typed;
  return withSlash(base) + typed;
}

// Where a full path looks for matches: in the folder it names when it ends
// in "/", else in its parent for names starting with its last part. null for
// an empty path.
export function matchPlace(path) {
  if (path === "") return null;
  if (path.endsWith("/")) return { parent: path, part: "" };
  return { parent: dirOf(path), part: baseOf(path) };
}

// matchFolders (picker.go:81-118) over one folder's listing ({name, is_dir}
// entries; dir_list's is_dir follows links, so a link to a folder counts):
// the folders whose names start with the last typed part, ignoring case,
// hidden ones only once that part starts with ".", by name, at most 5, as
// full paths.
export function matchEntries(path, entries) {
  const place = matchPlace(path);
  if (!place || !Array.isArray(entries)) return [];
  const want = place.part.toLowerCase();
  return entries
    .filter((entry) => entry && typeof entry.name === "string" && entry.name !== "" && entry.is_dir === true)
    .map((entry) => entry.name)
    .filter((name) => place.part.startsWith(".") || !name.startsWith("."))
    .filter((name) => name.toLowerCase().startsWith(want))
    .sort(byName)
    .slice(0, PICKER_ROWS)
    .map((name) => join(place.parent, name));
}

// The matches for a full path, listing its folder with listDir(path) →
// {entries}. An unreadable folder, or no path, gives none.
export async function matchFolders(path, listDir) {
  const place = matchPlace(path);
  if (!place) return [];
  try {
    const listing = await listDir(place.parent);
    return matchEntries(path, listing?.entries);
  } catch {
    return [];
  }
}

// newPicker (picker.go:32-45): the box opens with dir typed in, home as "~",
// ending in "/", the caret at the end and no row picked. recent is the
// recent folders (latest first; SES's recent_folders skips missing ones),
// each once, at most 5.
export function newPicker({ kind = "session", dir = "", recent = [], home = "" } = {}) {
  const p = { kind, base: dir, home, input: "", cursor: 0, sel: -1, matches: [], recent: [], err: "", version: 0 };
  setInput(p, withSlash(short(dir, home)));
  for (const folder of recent ?? []) {
    if (p.recent.length === PICKER_ROWS) break;
    if (typeof folder === "string" && folder !== "" && !p.recent.includes(folder)) p.recent.push(folder);
  }
  return p;
}

export function setInput(p, text) {
  p.input = text;
  p.cursor = text.length;
  edited(p);
}

// edited (picker.go:60-65): a change to the typed path lets go of the picked
// row and the error; the matches follow (the caller lists the folder).
export function edited(p) {
  p.sel = -1;
  p.err = "";
  p.version += 1;
}

// The full path the typed text names, for matching.
export function typedPath(p) {
  return expand(p.input, p.base, p.home);
}

// Every row that can be picked: the matches, then the recent folders.
export function rows(p) {
  return [...p.matches, ...p.recent];
}

// move (picker.go:126-133): the typed path is the stop before the first row;
// moving wraps. The error stays.
export function move(p, by) {
  const n = rows(p).length;
  if (n === 0) return;
  p.sel = ((p.sel + 1 + by + n + 1) % (n + 1)) - 1;
}

// complete (picker.go:135-145): the picked row, or else the first match,
// goes into the typed path with "/" added, to go on into it. True when the
// path changed.
export function complete(p) {
  const all = rows(p);
  if (p.sel >= 0 && p.sel < all.length) {
    setInput(p, withSlash(short(all[p.sel], p.home)));
    return true;
  }
  if (p.matches.length > 0) {
    setInput(p, withSlash(short(p.matches[0], p.home)));
    return true;
  }
  return false;
}

// choice (picker.go:147-166), up to the folder check: the picked row, or the
// typed path made full and clean; null with err set when nothing is typed.
// The caller checks that the folder exists and calls noFolder when not.
export function choice(p) {
  const all = rows(p);
  if (p.sel >= 0 && p.sel < all.length) return all[p.sel];
  const typed = p.input.trim();
  if (typed === "") {
    p.err = "Type a folder.";
    return null;
  }
  return clean(expand(typed, p.base, p.home));
}

export function noFolder(p, dir) {
  p.err = `No folder at ${short(dir, p.home)}`;
}

// Pasted or typed text loses new lines, returns and tabs (picker.go:168-182).
export function cleanText(text) {
  return String(text ?? "").replace(/[\n\r\t]/gu, "");
}

// insert (picker.go:168-182), replacing the selected text [from, to) when
// there is one, as a text box does.
export function insert(p, text, from = p.cursor, to = p.cursor) {
  const add = cleanText(text);
  if (add === "" && from === to) return false;
  p.input = p.input.slice(0, from) + add + p.input.slice(to);
  p.cursor = from + add.length;
  edited(p);
  return true;
}

// The string length of the character before or after a place (a surrogate
// pair is one character).
function charBefore(text, at) {
  const low = text.charCodeAt(at - 1);
  const high = text.charCodeAt(at - 2);
  return at >= 2 && low >= 0xdc00 && low <= 0xdfff && high >= 0xd800 && high <= 0xdbff ? 2 : 1;
}
function charAfter(text, at) {
  const high = text.charCodeAt(at);
  const low = text.charCodeAt(at + 1);
  return high >= 0xd800 && high <= 0xdbff && low >= 0xdc00 && low <= 0xdfff ? 2 : 1;
}

export function backspace(p) {
  if (p.cursor === 0) return false;
  const at = p.cursor - charBefore(p.input, p.cursor);
  p.input = p.input.slice(0, at) + p.input.slice(p.cursor);
  p.cursor = at;
  edited(p);
  return true;
}

export function deleteForward(p) {
  if (p.cursor >= p.input.length) return false;
  p.input = p.input.slice(0, p.cursor) + p.input.slice(p.cursor + charAfter(p.input, p.cursor));
  edited(p);
  return true;
}

// deleteWord (picker.go:201-216): the folder name before the caret, with
// its slash.
export function deleteWord(p) {
  let i = p.cursor;
  while (i > 0 && p.input[i - 1] === "/") i -= 1;
  while (i > 0 && p.input[i - 1] !== "/") i -= 1;
  if (i === p.cursor) return false;
  p.input = p.input.slice(0, i) + p.input.slice(p.cursor);
  p.cursor = i;
  edited(p);
  return true;
}

// The caret moves (input.go:86-93).
export function caretLeft(p) {
  p.cursor = Math.max(p.cursor - charBefore(p.input, p.cursor), 0);
}
export function caretRight(p) {
  p.cursor = Math.min(p.cursor + charAfter(p.input, p.cursor), p.input.length);
}
export function caretHome(p) {
  p.cursor = 0;
}
export function caretEnd(p) {
  p.cursor = p.input.length;
}
