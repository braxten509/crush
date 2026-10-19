// SET's writes and reads (desktop-design-5-6 2.12, 2.13, section 4). Every
// config write goes through the engine (POST …/config/set at the global
// scope); the rows show the effective value from ws.config, which the
// workspace store re-reads on config_changed (M3), so a change made in the
// terminal shows here too.
import { windowBusy } from "../busy.js";
import { api, SCOPE } from "../engine/api.js";
import { focusField } from "../stores/composer.svelte.js";
import { notify } from "../stores/ui.svelte.js";
import { ws } from "../stores/workspace.svelte.js";

export const DEFAULT_AUTOCOMPACT = 400_000;
export const DEFAULT_CHECK_IN = 2;
export const NOTIFICATION_STYLES = [
  { id: "auto", label: "Auto" },
  { id: "native", label: "Native" },
  { id: "osc", label: "OSC" },
  { id: "bell", label: "Bell" },
  { id: "disabled", label: "Disabled" },
];

const errorText = (error) => error?.message ?? String(error);
export const options = () => ws.config?.options ?? {};
export const tui = () => options().tui ?? {};

export async function setOption(key, value) {
  await api.setConfig(SCOPE.global, key, value);
}

// -- The four toggles (config_write.go:71-199) --

// Toggle Sidebar: the terminal's compact mode, inverted.
export async function toggleSidebar() {
  try {
    await api.setCompact(!(tui().compact_mode === true));
  } catch (error) {
    notify("error", errorText(error));
  }
}

export async function setSidebarShown(shown) {
  try {
    await api.setCompact(!shown);
  } catch (error) {
    notify("error", errorText(error));
  }
}

export async function toggleComposerFocusOnly() {
  const next = !(tui().composer_focus_only === true);
  try {
    await setOption("options.tui.composer_focus_only", next);
    if (next) focusField();
    notify("info", next ? "Focus stays on the composer" : "Tab moves focus again");
  } catch (error) {
    notify("error", errorText(error));
  }
}

export async function toggleThoughtLine() {
  const next = tui().thought_line === false;
  try {
    await setOption("options.tui.thought_line", next);
    notify("info", next ? "Busy status shows the model's newest thought" : "Busy status hides the model's thinking");
  } catch (error) {
    notify("error", errorText(error));
  }
}

// G-16: the engine pins itself and the agents it starts.
export async function toggleAgentCpus() {
  const next = !(options().agent_cpus_enabled === true);
  try {
    await setOption("options.agent_cpus_enabled", next);
    const answer = await api.setCpuAffinity(next);
    notify("info", next ? `AI work now runs on CPUs ${answer?.cpus ?? ""}`.trim() : "AI work can use the whole CPU again");
  } catch (error) {
    notify("error", errorText(error));
  }
}

export async function setAgentCpus(list) {
  try {
    await setOption("options.agent_cpus", list.trim());
    if (options().agent_cpus_enabled === true) {
      const answer = await api.setCpuAffinity(true);
      notify("info", `AI work now runs on CPUs ${answer?.cpus ?? ""}`.trim());
    }
  } catch (error) {
    notify("error", errorText(error));
  }
}

export async function setCheckInMinutes(text) {
  const minutes = Number(String(text).trim());
  // 0 turns check-ins off (options.check_in_minutes).
  if (String(text).trim() === "" || !Number.isInteger(minutes) || minutes < 0) {
    notify("error", "Enter a whole number of minutes (0 turns check-ins off)");
    return false;
  }
  try {
    await setOption("options.check_in_minutes", minutes);
    return true;
  } catch (error) {
    notify("error", errorText(error));
    return false;
  }
}

export async function setLaunchTarget(target) {
  try {
    await setOption("options.remote_launch_target", target);
  } catch (error) {
    notify("error", errorText(error));
  }
}

// -- Fallback compaction (autocompact.go:42-64) --

// → {value} or {error}
export function parseTokens(text) {
  const digits = String(text).replaceAll(",", "").trim();
  if (!/^\d+$/.test(digits)) return { error: "Enter a whole number of tokens" };
  const value = Number(digits);
  if (value < 1000) return { error: "Enter at least 1,000 tokens" };
  return { value };
}

export async function saveAutoCompact(value) {
  if (windowBusy()) {
    notify("warn", "Agent is busy, please wait before saving fallback compaction");
    return false;
  }
  try {
    await setOption("options.auto_compact_token_limit", value);
    await api.updateAgent();
    notify("info", `Fallback compaction set to ${value} tokens`);
    return true;
  } catch (error) {
    notify("error", errorText(error));
    return false;
  }
}

// -- Memory limit (memory_limit.go:78-90) --

// → {value: GB number, 0 for none} or {error}
export function parseGigabytes(text) {
  const trimmed = String(text).trim().replace(/\s*gb$/i, "");
  if (trimmed === "") return { value: 0 };
  if (!/^\d+(\.\d+)?$/.test(trimmed)) return { error: "Enter a number of GB, or leave it empty for no limit" };
  const value = Number(trimmed);
  if (value !== 0 && value < 2) return { error: "Enter at least 2 GB, or leave it empty for no limit" };
  return { value };
}

export const gigabytes = (bytes) => (Number(bytes ?? 0) / 1024 ** 3).toFixed(1);

// -- Notification style (config_write.go:106-120) --

export async function saveNotificationStyle(id) {
  try {
    await setOption("options.notifications", id);
    notify("info", `Notifications set to: ${id}`);
    return true;
  } catch (error) {
    notify("error", errorText(error));
    return false;
  }
}

export const notificationStyle = () => {
  const value = String(options().notifications ?? "auto").toLowerCase();
  return NOTIFICATION_STYLES.some((s) => s.id === value) ? value : "auto";
};
