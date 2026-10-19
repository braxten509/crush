// The external editor (desktop-design-5-6 2.26; CMD-030, TYPE-015): the
// typing box's text in $VISUAL or $EDITOR (else nano), in a terminal tab
// titled "Editor", and the result back in the typing box, as the terminal's
// Ctrl+O does (internal/ui/model/keypress.go:366-377, editor.go:144-193,
// update_ui.go:161-166).

import { windowBusy } from "../busy.js";
import { editorFinish, editorPrepare } from "../engine/commands.js";
import { backspaceLeavesBang, composer, draftOf, fieldElement, projectDir, setDraftOf, setText, typed } from "../stores/composer.svelte.js";
import { sessions } from "../stores/sessions.svelte.js";
import { notify } from "../stores/ui.svelte.js";
import { openTab } from "./tabs.svelte.js";

const BUSY = "Agent is working, please wait..."; // keypress.go:370

// The caret's line and column, both from 1, as the terminal passes the
// textarea's Line()+1 and Column()+1 (editor.go:157-160); the column counts
// characters, not UTF-16 units.
export function caretPosition(text, caret) {
  const before = text.slice(0, Math.max(0, Math.min(caret, text.length)));
  const lineStart = before.lastIndexOf("\n") + 1;
  return { line: before.split("\n").length, column: [...before.slice(lineStart)].length + 1 };
}

// Ctrl+O and the palette's "Open External Editor".
export async function openExternalEditor() {
  if (windowBusy()) {
    notify("warn", BUSY);
    return;
  }
  // The chat the text came from gets the result back, even if another chat
  // is shown by then (2.26 steps 2 and 5).
  const sid = sessions.currentId ?? "";
  const draft = draftOf();
  // "!" in front while the box is in shell mode (keypress.go:372-375); the
  // caret is the box's, without the "!", as the terminal's is.
  const text = (composer.mode === "bang" ? "!" : "") + draft.text;
  const field = fieldElement();
  const caret = field ? (field.selectionDirection === "backward" ? field.selectionStart : field.selectionEnd) : draft.text.length;
  const { line, column } = caretPosition(draft.text, caret ?? draft.text.length);
  let prepared;
  try {
    prepared = await editorPrepare(text, line, column);
  } catch (error) {
    notify("error", error?.message ?? String(error));
    return;
  }
  // The editor runs in a tab of its own, shown at once (2.26 step 4); when
  // it ends or its tab is closed, the file comes back (step 5).
  openTab({ cwd: projectDir(), argv: prepared.argv, title: "Editor", backToChat: true, after: (exited) => finish(exited, prepared.path, sid) });
}

async function finish(exited, path, sid) {
  const end = await exited;
  let result;
  try {
    result = await editorFinish(path);
  } catch (error) {
    notify("error", error?.message ?? String(error));
    return;
  }
  // A failed start shows its error in the tab; an editor that failed gives
  // nothing back (editor.go:165-174), unless the user closed its tab.
  if (end.error) return;
  if (!end.closed && (end.signal || end.code !== 0)) {
    notify("error", end.signal ? `signal: ${String(end.signal).toLowerCase()}` : `exit status ${end.code}`);
    return;
  }
  if ((sessions.currentId ?? "") === sid) applyEditorText(result.text);
  else setDraftOf(sid, result.text);
}

// openEditorMsg (update_ui.go:161-166): the text replaces the typing box's,
// the caret at its end; then syncBangModeFromTextarea (history.go:225-240):
// a leading "!" turns shell mode on (the "!" goes and the caret moves to the
// start); text without one turns it off.
export function applyEditorText(text) {
  const draft = draftOf();
  if (text.startsWith("!")) {
    if (draft.bang) setText(text.slice(1));
    // Typed in over other text, so shell mode turns on as when "!" is typed
    // at the start, and not as an emptied shell-mode box (bangWasEmpty false).
    else typed({ before: "x", value: text, caret: 0 });
    caretTo(0);
    return;
  }
  if (draft.bang) {
    // Leave shell mode the one way the store offers: an emptied box, then
    // Backspace (keypress.go:382-388).
    typed({ before: "x", value: "", caret: 0 });
    backspaceLeavesBang();
  }
  setText(text);
  caretTo(text.length);
}

// After the box has drawn the new text.
function caretTo(at) {
  setTimeout(() => {
    const field = fieldElement();
    field?.setSelectionRange(at, at);
  }, 0);
}
