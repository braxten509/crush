// The theme this window shows (desktop-design-5-6 3.3, 7.4); the tokens
// themselves are on :root. The desktop's choice is its own `theme` setting
// ("" or absent: scholars-edition), never the terminal's. Every window takes
// a change through `settings-changed` (other app processes through P3.2's
// watcher); a theme file changed elsewhere arrives as config_changed and is
// applied when its revision moved. A preview (the Themes sheet, the editor)
// recolors this window only until it is applied or reverted. The
// `appearance` setting ("light", "dark" or "system"; absent: light) turns a
// light theme dark (dark.js); "system" follows the desktop's own preference.
import { getCurrentWebviewWindow } from "@tauri-apps/api/webviewWindow";
import { api } from "../engine/api.js";
import { settings_get, settings_set } from "../engine/commands.js";
import { onEvent } from "../engine/events.js";
import { applyView } from "./apply.js";
import { readCache, writeCache } from "./cache.js";
import { notify } from "../stores/ui.svelte.js";
import { darkView, wantsDark } from "./dark.js";
import { DEFAULT_THEME } from "./tokens.js";

export const theme = $state({ name: DEFAULT_THEME, dark: false, revision: "", appearance: "light" });

let chosen = ""; // the setting as last read ("" = the default)
let lastView = null; // the view as the engine sent it, before dark mode
let loadSeq = 0;
let previewing = false;

export const chosenTheme = () => chosen || DEFAULT_THEME;
export const themeSetting = () => chosen;

// Applies a ThemeView to this window; `save` also records it as the first-frame cache.
export function applyTheme(view, { save = true } = {}) {
  lastView = view;
  const { tokens, dark } = applyView(wantsDark(theme.appearance, systemDark()) ? darkView(view) : view);
  theme.name = view?.name || theme.name;
  theme.dark = dark;
  theme.revision = view?.revision ?? "";
  if (save) writeCache({ name: theme.name, revision: theme.revision, appearance: modeKey(), dark, tokens });
}

// The desktop's own light/dark preference ("system" mode follows it).
const systemQuery = globalThis.matchMedia?.("(prefers-color-scheme: dark)") ?? null;
const systemDark = () => systemQuery?.matches === true;

// What the cache was made for: a mode change must not reuse it.
const modeKey = () => (wantsDark(theme.appearance, systemDark()) ? "dark" : "light");

// Light, dark or system: settings_set, which every window hears.
export async function chooseAppearance(mode) {
  try {
    await settings_set("appearance", mode);
  } catch (error) {
    notify("error", `Couldn't save the mode: ${error?.message ?? error}`);
    return;
  }
  if (!takeAppearance({ appearance: mode })) return;
  if (lastView && !previewing) applyTheme(lastView);
  else loadCurrent({ force: true });
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
  if (!force && cache?.name === view.name && cache?.revision === view.revision && (cache?.appearance ?? "light") === modeKey() && theme.name === view.name && theme.revision === view.revision) {
    lastView = view;
    return;
  }
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
  return takeAppearance(settings) || changed;
}

function takeAppearance(settings) {
  const mode = ["light", "dark", "system"].includes(settings?.appearance) ? settings.appearance : "light";
  const changed = mode !== theme.appearance;
  theme.appearance = mode;
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
    // Until the settings arrive, the mode the cached colors were made in.
    if (cache.appearance === "dark") theme.appearance = "dark";
  }
  // The desktop switched light/dark: "system" follows it.
  systemQuery?.addEventListener?.("change", () => {
    if (theme.appearance === "system" && lastView && !previewing) applyTheme(lastView);
  });
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
