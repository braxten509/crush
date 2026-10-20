// TERM's registrations (desktop-design-5-6 2.25, 2.26, 7.3): the action
// editor.open, the close box (the sheet terminal_close), Ctrl+O in the typing
// box, the keys of a focused terminal tab (the `main` scope), the close step
// `terminals` and the probe's `terminal` section (8.2).
//
// While a terminal tab has the focus no scope after `main` runs and every key
// no binding takes goes to the shell (lib/keys.js), Ctrl+C included. These
// are the keys the session host keeps for itself in a terminal tab
// (internal/ui/sessionhost/input.go:13-71, terminal.go:140-157) and the
// desktop's copy and paste keys (2.25).
import CloseBox from "../../components/terminal/CloseBox.svelte";
import { registerAction, runAction } from "../actions.js";
import { registerCloseStep } from "../closing.js";
import { registerDialog } from "../dialogs.js";
import { clipboard_read } from "../engine/commands.js";
import { bindKey } from "../keys.js";
import { probeSection } from "../probe.js";
import { fieldElement } from "../stores/composer.svelte.js";
import { openExternalEditor } from "../terminal/editor.js";
import {
  askCloseTerminal,
  closeAllTerminals,
  copySelection,
  pasteInto,
  probeHeavy,
  probeLight,
  shiftEnter,
  shiftPage,
  shownTab,
} from "../terminal/tabs.svelte.js";

// "Open External Editor" (commands.go:570-577; CMD-030). The terminal lists
// it only while $EDITOR is set; the page cannot see the login-shell
// environment, so the action is always on and editor_prepare reports a
// missing editor.
registerAction({ id: "editor.open", title: "Open external editor", keys: ["Ctrl+O"], run: openExternalEditor });

registerDialog("terminal_close", CloseBox);

// Closing the window stops its tabs (2.23 step 2).
registerCloseStep({ id: "terminals", order: 20, run: closeAllTerminals });

// TYPE-015: Ctrl+O in the typing box (3-4 design §1.8 keeps it for this).
bindKey("composer", "Ctrl+O", (event) => {
  if (event.target !== fieldElement()) return false;
  runAction("editor.open");
  return true;
});

// A key of the shown terminal tab.
function tabKey(combo, fn) {
  bindKey("main", combo, () => {
    const tab = shownTab();
    if (!tab) return false;
    fn(tab);
    return true;
  });
}

const pasteClipboard = (tab) =>
  clipboard_read()
    .then((text) => pasteInto(tab, text ?? ""))
    .catch(() => {});

tabKey("Shift+PageUp", (tab) => shiftPage(tab, false));
tabKey("Shift+PageDown", (tab) => shiftPage(tab, true));
tabKey("Shift+Enter", shiftEnter);
tabKey("Ctrl+Shift+C", copySelection);
tabKey("Ctrl+Shift+V", pasteClipboard);
tabKey("Shift+Insert", pasteClipboard);
// Alt+W asks to close the shown tab (input.go:44-46). LST's Alt+W, in the
// global scope, does not run while a terminal tab has the focus.
tabKey("Alt+W", (tab) => askCloseTerminal(tab.id));

probeSection("terminal", probeLight, probeHeavy);
