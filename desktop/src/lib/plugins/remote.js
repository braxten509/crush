// SHARE's registrations (desktop-design-5-6 2.27; loaded by lib/plugins.js):
// the action `remote.open`; the dialogs `remote`, `remote_setup` and
// `launcher_password`; remote_status and remote_action; the close step
// `remote` (a closing window stops its share); and the quiet start from the
// window's launch ID (app_info at start, window-request later), used once.
import { getCurrentWebviewWindow } from "@tauri-apps/api/webviewWindow";
import LauncherPassword from "../../components/remote/LauncherPassword.svelte";
import RemoteSetup from "../../components/remote/RemoteSetup.svelte";
import RemoteStatus from "../../components/remote/RemoteStatus.svelte";
import { registerAction } from "../actions.js";
import { registerCloseStep } from "../closing.js";
import { registerDialog } from "../dialogs.js";
import { takeLaunchId } from "../engine/commands.js";
import { onEvent } from "../engine/events.js";
import { runRemoteAction } from "../remote/actions.js";
import { isOn, onStatus, openRemote, startShare, stopShare } from "../remote/share.js";
import { watchPresence } from "../remote/store.svelte.js";

registerDialog("remote", RemoteStatus);
registerDialog("remote_setup", RemoteSetup);
registerDialog("launcher_password", LauncherPassword);

registerAction({ id: "remote.open", title: "Remote control", run: openRemote });

onEvent("remote_status", (_kind, status) => onStatus(status));
onEvent("remote_action", (_kind, action) => runRemoteAction(action));

registerCloseStep({
  id: "remote",
  order: 20,
  run: async () => {
    if (isOn()) await stopShare({ silent: true });
  },
});

watchPresence();

// REM-009: the launch ID this window was given starts sharing quietly once
// the engine is joined.
let launchChecked = false;
onEvent("connection", async (_kind, ev) => {
  if (ev?.state !== "connected" || launchChecked) return;
  launchChecked = true;
  const launchId = await takeLaunchId();
  if (launchId) startShare({ launchId, quiet: true });
});

// REM-017: a later launch for this window always starts again with its ID.
try {
  getCurrentWebviewWindow()
    .listen("window-request", (event) => {
      const launchId = event?.payload?.launchId;
      if (launchId) startShare({ launchId, quiet: true });
    })
    .catch(() => {});
} catch {
  // Not inside the app window.
}
