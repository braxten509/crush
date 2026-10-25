// The folders the left list keeps (owner, 2026-10-21): a folder shows from
// its first open chat on and stays after its last one closes, with a "+"
// for a new chat there, until its × takes it off. Kept in localStorage, which
// every window of the app shares; a change in one window reaches the others
// through the storage event.
import { clean } from "../leftlist/picker.js";

const KEY = "crush.listFolders";

export const listFolders = $state({ dirs: read() });

function read() {
  try {
    const dirs = JSON.parse(globalThis.localStorage?.getItem(KEY) ?? "[]");
    return Array.isArray(dirs) ? dirs.filter((d) => typeof d === "string" && d !== "") : [];
  } catch {
    return [];
  }
}

function write(dirs) {
  listFolders.dirs = dirs;
  try {
    globalThis.localStorage?.setItem(KEY, JSON.stringify(dirs));
  } catch (error) {
    console.warn("Could not save the list's folders", error?.message ?? error);
  }
}

// One folder's paths written one way.
export function folderKey(dir) {
  return dir ? clean(dir) : "";
}

// Keeps each folder not kept yet, after the others.
export function keepFolders(dirs) {
  const add = [];
  for (const dir of dirs) {
    const k = folderKey(dir);
    if (k && !listFolders.dirs.includes(k) && !add.includes(k)) add.push(k);
  }
  if (add.length > 0) write([...listFolders.dirs, ...add]);
}

const dropped = new Set(); // fn(dir)

// fn(dir) runs for each folder taken off the list, here or in another window.
export function onFolderDropped(fn) {
  dropped.add(fn);
  return () => dropped.delete(fn);
}

function tellDropped(before) {
  for (const dir of before) {
    if (listFolders.dirs.includes(dir)) continue;
    for (const fn of dropped) fn(dir);
  }
}

// The folder heading's ×.
export function dropFolder(dir) {
  const k = folderKey(dir);
  if (!listFolders.dirs.includes(k)) return;
  const before = listFolders.dirs;
  write(listFolders.dirs.filter((d) => d !== k));
  tellDropped(before);
}

globalThis.addEventListener?.("storage", (event) => {
  if (event.key !== KEY) return;
  const before = listFolders.dirs;
  listFolders.dirs = read();
  tellDropped(before);
});
