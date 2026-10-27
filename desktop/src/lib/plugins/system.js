// SYS's registrations (desktop-design-5-6 2.18, 2.19, 2.28; loaded by
// lib/plugins.js): the dialogs `logs`, `about`, `crash` and `close` (the
// close question, lib/closeask.js); the actions `updates.clis`,
// `updates.crush` and `usage.report`; the alert service, the taskbar state,
// the window title, the update checks and the crash notice.
import About from "../../components/system/About.svelte";
import CloseCrush from "../../components/system/CloseCrush.svelte";
import CrashNotice from "../../components/system/CrashNotice.svelte";
import Logs from "../../components/system/Logs.svelte";
import { registerAction } from "../actions.js";
import { CLOSE_SHEET } from "../closeask.js";
import { openDialog, registerDialog } from "../dialogs.js";
import { crashPending, crushStats } from "../engine/commands.js";
import { notify } from "../stores/ui.svelte.js";
import { startAlerts } from "../system/alerts.js";
import { focusChanged, startBadge } from "../system/badge.svelte.js";
import { startFocus } from "../system/focus.js";
import { startTitle } from "../system/title.svelte.js";
import { checkClis, checkCrush, startUpdates } from "../system/updates.js";

registerDialog("logs", Logs);
registerDialog("about", About);
registerDialog("crash", CrashNotice);
registerDialog(CLOSE_SHEET, CloseCrush);

registerAction({ id: "updates.clis", title: "Update agent CLIs", run: () => checkClis(true) });
registerAction({ id: "updates.crush", title: "Update Crush", run: () => checkCrush(true) });

// MISC-017: crush writes the report and opens it in the browser itself.
registerAction({
  id: "usage.report",
  title: "Usage report",
  run: async () => {
    try {
      const result = await crushStats();
      const lines = result?.lines ?? [];
      const generated = lines.find((l) => l.startsWith("Stats generated:"));
      const browser = lines.find((l) => l.startsWith("Could not open browser:"));
      if (browser) notify("warn", browser);
      else if (generated) notify("info", generated);
      else if (!result?.ok) notify("error", lines[lines.length - 1] || "crush stats failed");
    } catch (error) {
      notify("error", error?.message ?? String(error));
    }
  },
});

if (typeof window !== "undefined") {
  startFocus(focusChanged);
  startAlerts();
  startBadge();
  startTitle();
  startUpdates();
  crashPending()
    .then((report) => {
      if (report?.path) openDialog("crash", report);
    })
    .catch(() => {});
}
