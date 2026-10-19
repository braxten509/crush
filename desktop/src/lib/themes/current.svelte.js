// The theme this window shows (desktop-design-5-6 3.3, 7.4); the tokens
// themselves are on :root. The desktop's choice is its own `theme` setting
// ("" or absent: scholars-edition), never the terminal's. Every window takes
// a change through `settings-changed` (other app processes through P3.2's
// watcher); a theme file changed elsewhere arrives as config_changed and is
// applied when its revision moved. A preview (the Themes sheet, the editor)
// recolors this window only until it is applied or reverted.
import { getCurrentWebviewWindow } from "@tauri-apps/api/webviewWindow";
import { api } from "../engine/api.js";
import { settings_get, settings_set } from "../engine/commands.js";
import { onEvent } from "../engine/events.js";
import { applyView } from "./apply.js";
import { readCache, writeCache } from "./cache.js";
import { DEFAULT_THEME } from "./tokens.js";

export const theme = $state({ name: DEFAULT_THEME, dark: false, revision: "" });

let chosen = ""; // the setting as last read ("" = the default)
let loadSeq = 0;
let previewing = false;

export const chosenTheme = () => chosen || DEFAULT_THEME;
export const themeSetting = () => chosen;

// Applies a ThemeView to this window; `save` also records it as the first-frame cache.
export function applyTheme(view, { save = true } = {}) {
  const { tokens, dark } = applyView(view);
  theme.name = view?.name || theme.name;
  theme.dark = dark;
  theme.revision = view?.revision ?? "";
  if (save) writeCache({ name: theme.name, revision: theme.revision, dark, tokens });
}

// Reads the chosen theme from the engine and applies it when it differs
// from what shows (or always with `force`). A theme that will not load
// falls back to the default (3.1 "Missing colors" 5).
export async function loadCurrent({ force = false } = {}) {
  const mine = ++loadSeq;
  const name = chosenTheme();
  let view;
  try {
    view = await api.theme(name);
  } catch (error) {
    console.error(`theme ${name} could not be loaded:`, error?.message ?? error);
    if (name === DEFAULT_THEME) return;
    try {
      view = await api.theme(DEFAULT_THEME);
    } catch {
      return;
    }
  }
  if (mine !== loadSeq || previewing) return;
  const cache = readCache();
  if (!force && cache?.name === view.name && cache?.revision === view.revision && theme.name === view.name && theme.revision === view.revision) return;
  applyTheme(view);
}

// -- Previews (Themes sheet and editor) --

export function previewView(view) {
  previewing = true;
  applyTheme(view, { save: false });
}

export function endPreview() {
  if (!previewing) return;
  previewing = false;
  loadCurrent({ force: true });
}

// The desktop's choice: settings_set, which every window hears.
export async function chooseTheme(name) {
  const value = name === DEFAULT_THEME ? "" : name;
  await settings_set("theme", value);
  chosen = value;
  previewing = false;
  await loadCurrent({ force: true });
}

function takeSettings(settings) {
  const next = typeof settings?.theme === "string" ? settings.theme : "";
  const changed = next !== chosen;
  chosen = next;
  return changed;
}

let started = false;

export function startThemes() {
  if (started) return;
  started = true;
  const cache = readCache();
  if (cache?.name) {
    theme.name = cache.name;
    theme.dark = cache.dark === true;
    theme.revision = cache.revision ?? "";
  }
  try {
    getCurrentWebviewWindow()
      .listen("settings-changed", (event) => {
        if (takeSettings(event?.payload) && !previewing) loadCurrent({ force: true });
      })
      .catch(() => {});
  } catch {
    // Not inside the app window.
  }
  onEvent("connection", (_kind, ev) => {
    if (ev?.state !== "connected") return;
    settings_get()
      .then((settings) => {
        takeSettings(settings);
        loadCurrent();
      })
      .catch(() => loadCurrent());
  });
  // A theme file changed (this app's save, the terminal's, G-1): re-read.
  onEvent("config_changed", () => {
    if (!previewing) loadCurrent();
  });
}
