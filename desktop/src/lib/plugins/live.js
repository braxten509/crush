// Helpers, background jobs and memory (desktop-design-5-6 2.15 to 2.17, 7.3;
// LIVE): the tray's helpers (order 30) and jobs (order 40) entries, both
// entered by ↓ from the typing box; the sheets "background" and "sub-agents";
// Ctrl+X and Ctrl+B; the 60-second hint under a running command; the memory
// notice; and the probe's "live" section.
import Background from "../../components/live/Background.svelte";
import SubAgents from "../../components/live/SubAgents.svelte";
import { dialogs, registerDialog } from "../dialogs.js";
import { bindKey } from "../keys.js";
import { backgroundKey } from "../live/ctrlb.js";
import { hintedCalls, hintFor } from "../live/hint.js";
import {
  helpers,
  helpersLabel,
  openSheet,
  probes,
  runningHelpers,
  SHEET,
  startHelpers,
  startWatches,
  trayEntryFocused,
  trayHint,
} from "../live/helpers.svelte.js";
import { jobs, jobsLabel, serverJobs, startJobs, trayJobs } from "../live/jobs.svelte.js";
import { memoryStatus, startMemory } from "../live/memory.js";
import { mainView } from "../mainview.js";
import { capabilities } from "../models/capabilities.svelte.js";
import { probeSection } from "../probe.js";
import { timelineRange } from "../selection.js";
import { sessions } from "../stores/sessions.svelte.js";
import { ui } from "../stores/ui.svelte.js";
import { ticker } from "../ticker.svelte.js";
import { registerToolHint } from "../toolhints.js";
import { traySections, traySelected } from "../tray.js";

registerDialog(SHEET.jobs, Background);
registerDialog(SHEET.helpers, SubAgents);

// The helpers and jobs are the right panel's BACKGROUND now (owner,
// 2026-10-19), not tray entries.

// -- Keys --

// Ctrl+X in a chat (keypress.go:592-599): the tray's LIVE entries take it while
// one of them is selected (above); anywhere else it opens Background Processes.
bindKey("global", "Ctrl+X", () => {
  if (!sessions.currentId) return false;
  openSheet(SHEET.jobs);
  return true;
});

// Ctrl+B (LIVE-003): whatever has the focus, except a sheet, which the router
// does not ask global keys for.
bindKey("global", "Ctrl+B", () => backgroundKey());

// In the helper view, Ctrl+C copies the selection (the view's Timeline takes
// it) and does nothing without one, so it never reaches Quit there
// (desktop-design-5-6 2.16 Keys). A sheet or menu over the view takes the
// input as usual (internal/ui/model/subagent_view.go:178-181).
bindKey("global", "Ctrl+C", () => {
  if (ui.focus !== "main" || mainView.current?.kind !== "helper") return false;
  if (ui.menu || ui.viewer || dialogs.length > 0) return false;
  const transcript = document.querySelector("[data-helper-view] section.timeline");
  return !(transcript && timelineRange(transcript));
});

// -- The 60-second hint (LIVE-004) --

registerToolHint((item) => hintFor(item));

// -- Reading --

startHelpers();
startJobs();
startMemory();
startWatches();

// -- Probe (desktop-design-5-6 7.4) --

function rectOf(el) {
  if (!el) return null;
  const r = el.getBoundingClientRect();
  return { x: r.x, y: r.y, w: r.width, h: r.height };
}

const ENTRY_LABEL = { helpers: () => helpersLabel(runningHelpers().length), jobs: () => jobsLabel(trayJobs().length) };

probeSection(
  "live",
  () => {
    const memory = memoryStatus();
    return {
      session_id: helpers.sid || null,
      tray: traySections
        .filter((section) => section.id in ENTRY_LABEL)
        .map((section) => ({
          id: section.id,
          label: ENTRY_LABEL[section.id](),
          hint: trayHint(section.id),
          focused: trayEntryFocused(section.id),
          // The helpers mark's quarter turn (◐◓◑◒), one step per wall-clock second.
          turn: section.id === "helpers" ? ticker.second % 4 : null,
        })),
      tray_focused: ui.focus === "tray",
      tray_selected: ui.focus === "tray" ? traySelected() : null,
      helpers: runningHelpers().map((h) => ({ id: h.id, name: h.name, status: h.status, child_session_id: h.child_session_id ?? "" })),
      helper_records: [...helpers.byId.values()].map((h) => ({ id: h.id, name: h.name, status: h.status })),
      jobs: trayJobs().map((job) => ({ job_id: job.job_id ?? "", pid: job.pid ?? 0, command: job.command ?? "" })),
      server_jobs: serverJobs().map((job) => ({ job_id: job.job_id ?? "", pid: job.pid ?? 0 })),
      jobs_read: jobs.sid || null,
      sheet: probes.sheet ? probes.sheet.light() : null,
      view: probes.view ? probes.view.light() : null,
      can_background: capabilities.can_background === true,
      hinted: hintedCalls(),
      memory: memory
        ? { supported: memory.supported === true, high: memory.high === true, percent: memory.percent ?? 0, episode: memory.episode ?? 0, message: memory.message ?? "" }
        : null,
    };
  },
  () => ({
    tray: [...document.querySelectorAll("[data-live-entry]")].map((el) => ({ id: el.dataset.liveEntry, rect: rectOf(el) })),
    sheet: probes.sheet ? probes.sheet.heavy() : null,
    view: probes.view ? probes.view.heavy() : null,
  }),
);
