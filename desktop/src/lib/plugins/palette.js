// PAL's registrations (desktop-design-5-6 2.2, 2.24, 7.3; loaded by
// lib/plugins.js): the action `palette.open` (Ctrl+P; "/" in an empty typing
// box opens the composer's `/` menu instead); the sheets `commands`, `arguments` and `quit`; the global keys
// of 2.2; and Ctrl+C's plain binding, which opens the quit sheet over any
// sheet or form unless text is selected (then the key goes on and copies).
import { getCurrentWindow } from "@tauri-apps/api/window";
import Arguments from "../../components/palette/Arguments.svelte";
import Palette from "../../components/palette/Palette.svelte";
import Quit from "../../components/palette/Quit.svelte";
import { registerAction } from "../actions.js";
import { dialogRegistered, openDialog, registerDialog } from "../dialogs.js";
import { app_info } from "../engine/commands.js";
import { bindKey } from "../keys.js";
import { agentNow } from "../live/helpers.svelte.js";
import { platform } from "../palette/entries.js";
import { sessions } from "../stores/sessions.svelte.js";

registerDialog("commands", Palette);
registerDialog("arguments", Arguments);
registerDialog("quit", Quit);

registerAction({ id: "palette.open", title: "Commands", keys: ["Ctrl+P"], run: () => openDialog("commands") });

app_info()
  .then((info) => (platform.linux = info?.platform !== "macos"))
  .catch(() => {});

// A sheet that is not built yet leaves its key to whoever else binds it.
const sheetKey = (id, when = () => true) => () => {
  if (!dialogRegistered(id) || !when()) return false;
  openDialog(id);
  return true;
};

bindKey("global", "Ctrl+P", sheetKey("commands"));
bindKey("global", "Ctrl+L", sheetKey("models"));
bindKey("global", "Ctrl+M", sheetKey("models"));
bindKey("global", "Ctrl+S", sheetKey("sessions"));
bindKey("global", "Ctrl+F", sheetKey("filepicker", () => Boolean(sessions.currentId) && agentNow()?.model?.supports_attachments === true));
bindKey("global", "Ctrl+,", sheetKey("settings"));

// CMD-001's "/" in an empty typing box now opens the composer's own `/` menu
// (owner, 2026-10-20; lib/stores/composer.svelte.js openSlash); Ctrl+P keeps
// the palette.

// CMD-064: Ctrl+Z minimizes the window unless an editable field has the focus.
bindKey("global", "Ctrl+Z", (event) => {
  const target = event.target;
  if (target?.isContentEditable || target?.tagName === "INPUT" || target?.tagName === "TEXTAREA") return false;
  try {
    getCurrentWindow().minimize();
  } catch {
    return false;
  }
  return true;
});

// Any selected text: in a field, or on the page.
function textSelected(event) {
  const target = event.target;
  if (target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA")) {
    if (typeof target.selectionStart === "number" && target.selectionStart !== target.selectionEnd) return true;
  }
  const selection = globalThis.getSelection?.();
  return Boolean(selection && !selection.isCollapsed && selection.toString() !== "");
}

// 2.24 (u:pal.quit_key): the router asks "global" first for Ctrl+C unless the
// quit sheet is open, so this works over every sheet and slot form.
bindKey("global", "Ctrl+C", (event) => {
  if (textSelected(event)) return false;
  openDialog("quit");
  return true;
});
