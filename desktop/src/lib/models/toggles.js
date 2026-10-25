// FAST, Ultracode and Thinking, and the effort save (desktop-design-5-6 2.3;
// CMD-024 to CMD-026; internal/ui/model/fast_mode.go and model_select.go:
// 400-475). Each writes the shown chat's own model; its next turn builds it.
// Support comes from G-8's capabilities, not ported rules.
import { windowBusy } from "../busy.js";
import { api } from "../engine/api.js";
import { sessions } from "../stores/sessions.svelte.js";
import { loadAgent } from "../stores/workspace.svelte.js";
import { notify } from "../stores/ui.svelte.js";
import { capabilities } from "./capabilities.svelte.js";
import { coderModel, modelData, refreshModels } from "./store.svelte.js";

const BUSY = "Agent is busy, please wait...";
export const onOff = (on) => (on ? "on" : "off");
const errorText = (error) => error?.message ?? String(error);

async function coder() {
  if (!modelData.cfg) await refreshModels();
  if (!modelData.cfg) throw new Error("configuration not found");
  const { selected } = coderModel();
  if (!selected?.model) throw new Error("no model is selected");
  return { selected: { ...selected } };
}

// A model picked in a chat is saved with that chat (owner, 2026-10-24:
// every chat owns its model); with no chat open it is the last pick a new
// chat starts on. A failed save reaches the picker.
export async function saveModelChoice(selected) {
  const sid = sessions.currentId;
  if (!sid) {
    await api.setLastPick(selected);
    modelData.lastPick = { ...selected };
    return;
  }
  await api.setSessionModel(sid, selected);
  // The chat may have been deleted while its save was on its way.
  if (sessions.byId[sid]) sessions.byId[sid] = { ...sessions.byId[sid], model: { ...selected } };
}

async function save(selected, info) {
  await saveModelChoice(selected);
  notify("info", info);
  loadAgent();
  refreshModels();
}

// fast_mode.go:12-45.
export async function toggleFast() {
  if (windowBusy()) return notify("warn", BUSY);
  try {
    if (!capabilities.supports_fast) throw new Error("selected model does not support FAST mode");
    const { selected } = await coder();
    const on = selected.service_tier !== "fast";
    selected.service_tier = on ? "fast" : "default";
    await save(selected, `FAST mode ${on ? "enabled" : "disabled"}`);
  } catch (error) {
    notify("error", errorText(error));
  }
}

// fast_mode.go:47-76.
export async function toggleUltracode() {
  if (windowBusy()) return notify("warn", BUSY);
  try {
    if (!capabilities.supports_ultracode) throw new Error("selected model does not support Ultracode");
    const { selected } = await coder();
    selected.ultracode = !selected.ultracode;
    await save(selected, `Ultracode ${onOff(selected.ultracode)}`);
  } catch (error) {
    notify("error", errorText(error));
  }
}

// model_select.go:403-434 (no busy check there).
export async function toggleThinking() {
  try {
    if (!capabilities.supports_thinking_toggle) throw new Error("selected model does not support a thinking toggle");
    const { selected } = await coder();
    selected.think = !selected.think;
    await save(selected, `Thinking mode ${selected.think ? "enabled" : "disabled"}`);
  } catch (error) {
    notify("error", errorText(error));
  }
}

// setReasoningEffort (model_select.go:440-475); ultracode is null when the
// model has no Ultracode switch. Throws a failed save, so the sheet stays.
export async function setEffort(effort, ultracode = null) {
  const { selected } = await coder();
  selected.reasoning_effort = effort;
  let info = `Reasoning effort set to ${effort}`;
  if (ultracode !== null) {
    selected.ultracode = ultracode;
    info += ` · Ultracode ${onOff(ultracode)}`;
  }
  await saveModelChoice(selected);
  notify("info", info);
  loadAgent();
  refreshModels();
}

// The account a Claude Code or Codex model runs on (config.SelectedModel's
// account; "" is the tool's own login), from the model menu's Account row.
export async function setAccount(account, name) {
  if (windowBusy()) return notify("warn", BUSY);
  const { selected } = await coder();
  if (account) selected.account = account;
  else delete selected.account;
  await save(selected, `Account set to ${name}`);
}
