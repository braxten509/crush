// FAST, Ultracode and Thinking, and the effort save (desktop-design-5-6 2.3;
// CMD-024 to CMD-026; internal/ui/model/fast_mode.go and model_select.go:
// 400-475). Each writes the coder's selected model and rebuilds the agent's
// model. Support comes from G-8's capabilities, not ported rules.
import { windowBusy } from "../busy.js";
import { api, SCOPE } from "../engine/api.js";
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
  const { type, selected } = coderModel();
  if (!selected?.model) throw new Error("agent configuration not found");
  return { type, selected: { ...selected } };
}

// A model picked in a chat is saved with that chat (owner, 2026-10-23), so
// another chat keeps its own; the workspace's copy stays the default for
// chats without one and for new chats.
export async function saveModelChoice(type, selected) {
  const sid = sessions.currentId;
  if (sid && type === "large") {
    // A chat of a folder this window has not joined may refuse the write;
    // the workspace's copy is saved either way.
    try {
      await api.setSessionModel(sid, selected);
      sessions.byId[sid] = { ...sessions.byId[sid], model: { ...selected } };
    } catch (error) {
      console.error("saving the chat's model failed", error?.message ?? error);
    }
  }
  await api.setModel(SCOPE.global, type, selected);
}

async function save(type, selected, info) {
  await saveModelChoice(type, selected);
  await api.updateAgent();
  notify("info", info);
  loadAgent();
  refreshModels();
}

// fast_mode.go:12-45.
export async function toggleFast() {
  if (windowBusy()) return notify("warn", BUSY);
  try {
    if (!capabilities.supports_fast) throw new Error("selected model does not support FAST mode");
    const { type, selected } = await coder();
    const on = selected.service_tier !== "fast";
    selected.service_tier = on ? "fast" : "default";
    await save(type, selected, `FAST mode ${on ? "enabled" : "disabled"}`);
  } catch (error) {
    notify("error", errorText(error));
  }
}

// fast_mode.go:47-76.
export async function toggleUltracode() {
  if (windowBusy()) return notify("warn", BUSY);
  try {
    if (!capabilities.supports_ultracode) throw new Error("selected model does not support Ultracode");
    const { type, selected } = await coder();
    selected.ultracode = !selected.ultracode;
    await save(type, selected, `Ultracode ${onOff(selected.ultracode)}`);
  } catch (error) {
    notify("error", errorText(error));
  }
}

// model_select.go:403-434 (no busy check there).
export async function toggleThinking() {
  try {
    if (!capabilities.supports_thinking_toggle) throw new Error("selected model does not support a thinking toggle");
    const { type, selected } = await coder();
    selected.think = !selected.think;
    await save(type, selected, `Thinking mode ${selected.think ? "enabled" : "disabled"}`);
  } catch (error) {
    notify("error", errorText(error));
  }
}

// setReasoningEffort (model_select.go:440-475); ultracode is null when the
// model has no Ultracode switch. Throws a failed save, so the sheet stays.
export async function setEffort(effort, ultracode = null) {
  const { type, selected } = await coder();
  selected.reasoning_effort = effort;
  let info = `Reasoning effort set to ${effort}`;
  if (ultracode !== null) {
    selected.ultracode = ultracode;
    info += ` · Ultracode ${onOff(ultracode)}`;
  }
  await saveModelChoice(type, selected);
  api
    .updateAgent()
    .then(() => {
      notify("info", info);
      loadAgent();
    })
    .catch((error) => notify("error", errorText(error)));
  refreshModels();
}

// The account a Claude Code or Codex model runs on (config.SelectedModel's
// account; "" is the tool's own login), from the model menu's Account row.
export async function setAccount(account, name) {
  if (windowBusy()) return notify("warn", BUSY);
  const { type, selected } = await coder();
  if (account) selected.account = account;
  else delete selected.account;
  await save(type, selected, `Account set to ${name}`);
}
