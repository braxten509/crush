// The chat list's action and keys (desktop-design 1.10; the shortcut table,
// lib/keys.js): "New Session" (chat.new) on Ctrl+N (LIST-033) and Ctrl+K to
// the Sessions sheet (PLAN.md:103; desktop-design-5-6 7.1, 2.5). Until a
// Sessions sheet is registered (SES), Ctrl+K focuses the chat-list search as
// before. The list's own ↑ ↓ Enter Esc are ChatList's, while it has the focus.
import { focusSearch } from "../../components/list/listfocus.js";
import { startNewChat } from "../../components/list/newchat.js";
import { registerAction } from "../actions.js";
import { hasDialog, openDialog } from "../dialogs.js";
import { bindKey } from "../keys.js";

registerAction({ id: "chat.new", title: "New chat", keys: ["Ctrl+N"], run: startNewChat });

bindKey("global", "Ctrl+N", () => {
  startNewChat();
  return true;
});

bindKey("global", "Ctrl+K", () => {
  openDialog("sessions");
  return hasDialog("sessions") || focusSearch();
});
