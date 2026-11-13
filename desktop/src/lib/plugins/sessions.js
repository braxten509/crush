// SES's registrations (desktop-design-5-6 2.5, 7.3; loaded by lib/plugins.js):
// the sheets `sessions` and `projects`, the action `project.save` (Save
// Project, CMD-038), and window-request's sessionId: window_open focused this
// window to show one of its chats (the Sessions sheet of another folder's
// window, DLG-088), or a toast's click (window_show_chat) sends a chat of any
// folder with its projectDir: this window switches there in place.
import { getCurrentWebviewWindow } from "@tauri-apps/api/webviewWindow";
import ChatCloseBox from "../../components/ChatCloseBox.svelte";
import LockBox from "../../components/LockBox.svelte";
import Projects from "../../components/sessions/Projects.svelte";
import Sessions from "../../components/sessions/Sessions.svelte";
import { registerAction } from "../actions.js";
import { registerDialog } from "../dialogs.js";
import { projectSave } from "../engine/commands.js";
import { short } from "../leftlist/picker.js";
import { leftList } from "../leftlist/store.svelte.js";
import { isClosed, reopen } from "../stores/closed.svelte.js";
import { conn } from "../stores/connection.svelte.js";
import { openInFolder } from "../stores/openchats.svelte.js";
import { openChat } from "../stores/sessions.svelte.js";
import { notify } from "../stores/ui.svelte.js";

registerDialog("sessions", Sessions);
registerDialog("projects", Projects);
// The lock button's "Lock this chat?" box (owner, 2026-11-03).
registerDialog("lock_chat", LockBox);
// "Close this chat?" while it still runs something (owner, 2026-11-13).
registerDialog("chat_close", ChatCloseBox);

// dialog_actions.go:400-407.
registerAction({
  id: "project.save",
  title: "Save project",
  async run() {
    const path = conn.project.path || conn.project.cwd;
    if (!path) return;
    try {
      await projectSave(path);
      notify("info", `Saved project ${short(path, leftList.home)}`);
    } catch (error) {
      notify("error", error?.message ?? String(error));
    }
  },
});

try {
  getCurrentWebviewWindow()
    .listen("window-request", async (event) => {
      const id = event?.payload?.sessionId;
      if (!id) return;
      if (isClosed(id)) await reopen(id);
      const dir = event?.payload?.projectDir;
      if (dir) openInFolder(dir, id);
      else openChat(id);
    })
    .catch(() => {});
} catch {
  // Not inside the app window (unit tests).
}
