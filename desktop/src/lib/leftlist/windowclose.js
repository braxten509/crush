// Closing this window (desktop-design-5-6 2.23; LIST-026, MISC-024). S5's
// closeWindow() (lib/closing.js) runs the close steps by order, then
// window_close, whose client leave lets the engine apply Q2 at once; the last
// window ends the app (LIST-017). The system close button, Ctrl+Shift+W and
// Quit all come here, with no confirmation (CHECKLIST LIST-026).
//
// LST's step "draft" (order 10): the shown chat is discarded when it is an
// empty draft and the window is not busy. The engine decides "empty": it keeps
// a chat with a user message, an unfinished reply or children (DiscardEmpty,
// internal/cmd/root.go:158-164; internal/client/sessions.go:74-79). A busy
// draft is kept.
import { windowBusy } from "../busy.js";
import { closeWindow } from "../closing.js";
import { api } from "../engine/api.js";
import { sessions } from "../stores/sessions.svelte.js";

export const DRAFT_STEP = { id: "draft", order: 10 };

export async function discardDraft() {
  const sid = sessions.currentId;
  if (!sid || windowBusy()) return false;
  await api.changeSavedSession({ DiscardEmpty: true, Session: { ID: sid } });
  return true;
}

// Ctrl+Shift+W (inferred key: the terminal's window is closed by its terminal).
export function closeThisWindow() {
  closeWindow();
  return true;
}
