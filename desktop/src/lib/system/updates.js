// Updates (desktop-design-5-6 2.19; UPD-001 to UPD-008): the upstream notice,
// the agent CLI offer and the Crush self-update offers, each a local yes/no
// form in the typing box's place (ASK's askYesNo, slot kind "update"). When
// the slot is taken, or a question or secure form takes it from an open
// offer, the offer becomes a 10 s update notice instead (UPD-008). The
// automatic checks run once per app start in the first window.
import { askYesNo } from "../ask/localform.js";
import { api } from "../engine/api.js";
import { app_info, selfupdateInstall, selfupdateRun } from "../engine/commands.js";
import { onEvent } from "../engine/events.js";
import { notify } from "../stores/ui.svelte.js";

const NOTICE_MS = 10_000;

// "a", "a and b", "a, b and c" (cliupdate.go:211-217).
export function joinNames(names) {
  if (names.length <= 1) return names[0] ?? "";
  return `${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}`;
}

function offer({ label, question, description, fallback, onYes, onNo }) {
  askYesNo({
    label,
    question,
    description,
    onAnswer: (answer) => {
      if (answer === true) onYes();
      else if (answer === false) onNo();
      else notify("update", fallback, NOTICE_MS);
    },
  });
}

// ── Agent CLIs (UPD-002, UPD-003, CMD-043) ──

function offerClis(updates) {
  const names = updates.map((u) => u.name);
  const bins = updates.map((u) => u.bin);
  const joined = joinNames(names);
  offer({
    label: "CLI updates",
    question: `Install updates for ${joined}?`,
    description: `${updates.map((u) => `${u.name} ${u.current} → ${u.latest}`).join(" · ")}. Each CLI runs its own updater in the background. No skips these versions until newer ones come out.`,
    fallback:
      updates.length === 1
        ? `${updates[0].name} ${updates[0].latest} is available. Run Update Agent CLIs from the command palette.`
        : `Updates for ${joined} are available. Run Update Agent CLIs from the command palette.`,
    onYes: async () => {
      notify("update", `Updating ${joined}…`, NOTICE_MS);
      try {
        await api.installCliUpdates(bins);
      } catch (error) {
        notify("error", error?.message ?? String(error));
      }
    },
    onNo: async () => {
      try {
        await api.declineCliUpdates(bins);
        notify("info", "Skipped these CLI updates");
      } catch (error) {
        notify("error", error?.message ?? String(error));
      }
    },
  });
}

export async function checkClis(manual) {
  if (manual) notify("info", "Checking agent CLIs for updates…");
  let check;
  try {
    check = await api.cliUpdates(manual);
  } catch (error) {
    if (manual) notify("warn", `Couldn't check every CLI: ${error?.message ?? error}`);
    return;
  }
  const updates = check?.updates ?? [];
  if (updates.length > 0) {
    offerClis(updates);
    return;
  }
  if (!manual || check?.skipped) return;
  const errors = check?.errors ?? [];
  if (errors.length > 0) notify("warn", `Couldn't check every CLI: ${errors.join("; ")}`);
  else notify("success", "All agent CLIs are up to date");
}

function installed(done) {
  const results = done?.results ?? [];
  const passed = results.filter((r) => !r.error).map((r) => `${r.name} ${r.latest}`);
  const failed = results.filter((r) => r.error).map((r) => `${r.name}: ${r.error}`);
  let text = passed.length > 0 ? `Updated ${passed.join(", ")}` : "No CLI was updated";
  if (failed.length > 0) text += `; failed: ${failed.join("; ")}`;
  notify(failed.length > 0 ? "error" : "success", text, NOTICE_MS);
}

// ── Crush (UPD-004 to UPD-007, CMD-044) ──

let updating = false;

const firstLine = (text) => String(text ?? "").split("\n")[0].trim();

function offerInstall(tag, fixed) {
  const how = fixed?.length > 0 ? `An AI agent fixed the clashes in ${fixed.join(", ")}` : "It merged cleanly";
  offer({
    label: "Crush update",
    question: `Install Crush ${tag} now?`,
    description: `${how} and every test passed. The old version is backed up, and the merge is uploaded to your fork on GitHub. Restart Crush afterwards to use it. Saying No keeps the tested build for later (Update Crush in the command palette).`,
    fallback: `Crush ${tag} is built and tested. Run Update Crush from the command palette to install it.`,
    onYes: async () => {
      try {
        const result = await selfupdateInstall();
        const version = result?.version || tag;
        if (!result?.ok) notify("error", `Couldn't install the Crush update: ${firstLine(result?.firstLine)}`);
        else if (result.firstLine) notify("warn", `Installed Crush ${version}, but ${firstLine(result.firstLine)}. Restart Crush to use it.`);
        else notify("success", `Installed Crush ${version} and uploaded it to GitHub. Restart Crush to use it.`);
      } catch (error) {
        notify("error", `Couldn't install the Crush update: ${firstLine(error?.message ?? error)}`);
      }
    },
    onNo: () => notify("info", `The tested Crush ${tag} waits in Update Crush`),
  });
}

async function runUpdate(tag) {
  updating = true;
  notify("update", `Updating Crush to ${tag} in the background: saving changes, merging, building and testing…`, 3_600_000);
  try {
    const result = await selfupdateRun(tag);
    if (result?.ok) offerInstall(tag, result.fixed ?? []);
    else notify("error", `Crush ${tag} update stopped: ${firstLine(result?.firstLine)}. Details: ${result?.logPath ?? ""}`);
  } catch (error) {
    notify("error", `Crush ${tag} update stopped: ${firstLine(error?.message ?? error)}.`);
  } finally {
    updating = false;
  }
}

function offerUpdate(release) {
  const { tag, current, dir } = release;
  offer({
    label: "Crush update",
    question: `Update Crush to ${tag}?`,
    description: `Upstream released ${tag} (you have ${current}). Crush first saves any uncommitted changes in a commit, then brings the update into ${dir}, builds it and runs every test in the background. If files clash or tests fail, an AI agent fixes them. You're asked again before anything is installed. Saying No skips this version.`,
    fallback: `Crush ${tag} is out. Run Update Crush from the command palette.`,
    onYes: () => runUpdate(tag),
    onNo: async () => {
      try {
        await api.declineCrushUpdate(tag);
        notify("info", `Skipped Crush ${tag}`);
      } catch (error) {
        notify("error", error?.message ?? String(error));
      }
    },
  });
}

export async function checkCrush(manual) {
  let check;
  if (manual && updating) {
    notify("info", "Crush is already updating");
    return;
  }
  try {
    check = await api.crushUpdate(manual);
  } catch (error) {
    if (manual) notify("warn", `Couldn't check for a new Crush: ${error?.message ?? error}`);
    return;
  }
  if (!check?.has_checkout) {
    if (manual) notify("warn", "This Crush has no fork checkout to update");
    return;
  }
  if (manual) notify("info", "Checking for a new Crush release…");
  if (check.error) {
    if (manual) notify("warn", `Couldn't check for a new Crush: ${check.error}`);
    return;
  }
  if (check.staged) {
    offerInstall(check.staged, []);
    return;
  }
  if (!check.release) {
    if (manual) notify("success", "Crush is on the latest stable release");
    return;
  }
  if (!manual && check.declined) return;
  offerUpdate(check.release);
}

// ── Upstream notice (UPD-001) ──

async function upstream(event) {
  try {
    const check = await api.crushUpdate(false);
    if (check?.has_checkout) return;
  } catch {
    // No answer: show the notice as the terminal does.
  }
  const text = event?.is_development
    ? `This is a development version of Crush. The latest version is v${event.latest_version}.`
    : `Crush update available: v${event?.current_version} → v${event?.latest_version}.`;
  notify("update", text, NOTICE_MS);
}

let started = false;
let checked = false;

export function startUpdates() {
  if (started) return;
  started = true;
  onEvent("update_available", (_kind, event) => upstream(event));
  onEvent("cli_updates_installed", (_kind, done) => installed(done));
  const first = app_info().then(
    (info) => info?.first_window === true,
    () => false,
  );
  onEvent("connection", async (_kind, ev) => {
    if (ev?.state !== "connected" || checked || !(await first) || checked) return;
    checked = true;
    checkClis(false).finally(() => checkCrush(false));
  });
}
