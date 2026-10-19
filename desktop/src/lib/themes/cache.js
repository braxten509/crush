// The theme cache public/theme-boot.js reads before the page draws
// (desktop-design-5-6 3.3, LIST-054): local storage "crush-desktop.theme",
// {name, revision, dark, tokens}.
const KEY = "crush-desktop.theme";

export function readCache() {
  try {
    const raw = globalThis.localStorage?.getItem(KEY);
    return raw ? JSON.parse(raw) : null;
  } catch {
    return null;
  }
}

export function writeCache(entry) {
  try {
    globalThis.localStorage?.setItem(KEY, JSON.stringify(entry));
  } catch {
    // A full or missing storage only costs the first frame.
  }
}
