// The Settings page's state (owner, 2026-10-19: Settings is a full page).
// openSettings shows it in the middle column (lib/mainview.js, kind
// "settings") with SettingsNav in the left column; the right panel steps
// aside while it shows (App.svelte). lib/plugins/settings.js gives the page
// component, so openDialog("settings") anywhere (Ctrl+, , the palette, the
// chat list's Settings button) opens the page.
import { closeMain, mainView, showMain } from "../mainview.js";
import { platform } from "../palette/entries.js";

export const settingsPage = $state({ section: "appearance" });

const SECTIONS = [
  { id: "appearance", label: "Appearance" },
  { id: "typing", label: "Typing" },
  { id: "work", label: "Work" },
  { id: "system", label: "System", linux: true },
  { id: "notifications", label: "Notifications" },
  { id: "accounts", label: "Accounts" },
  { id: "fallback", label: "Fallback order" },
  { id: "phone", label: "Phone" },
  { id: "providers", label: "Providers" },
];

export function settingsSections() {
  return SECTIONS.filter((s) => !s.linux || platform.linux);
}

let page = null;
export function setSettingsPage(component) {
  page = component;
}

export const settingsOpen = () => mainView.current?.kind === "settings";

export function openSettings(section = null) {
  if (section && settingsSections().some((s) => s.id === section)) settingsPage.section = section;
  if (!page || settingsOpen()) return Boolean(page);
  showMain({ kind: "settings", component: page });
  return true;
}

export function closeSettings() {
  if (settingsOpen()) closeMain();
}
