// The palette's own entries (desktop-design-5-6 2.2 below the table, 2.24):
// Summarize Session, Toggle Yolo Mode (silent here, unlike Ctrl+Y's banner),
// Initialize Project, and Quit (the window closes at once, no sheet).
import { windowBusy } from "../busy.js";
import { closeWindow } from "../closing.js";
import { api, uuid } from "../engine/api.js";
import { sendPromptText } from "../stores/composer.svelte.js";
import { sessions } from "../stores/sessions.svelte.js";
import { notify } from "../stores/ui.svelte.js";

const errorText = (error) => error?.message ?? String(error);

// CMD-023 (dialog_actions.go:143-150): the engine writes the summary inside
// the request, so it has a call id and no time limit.
export async function summarize() {
  if (windowBusy()) {
    notify("warn", "Agent is busy, please wait before summarizing session...");
    return;
  }
  const sid = sessions.currentId;
  if (!sid) return;
  try {
    await api.summarize(sid, uuid());
  } catch (error) {
    notify("error", errorText(error));
  }
}

// CMD-035: GET then POST …/permissions/skip; no banner.
export async function toggleYolo() {
  try {
    const now = await api.permissionsSkip();
    await api.setPermissionsSkip(!(now?.skip === true));
  } catch (error) {
    notify("error", errorText(error));
  }
}

// CMD-037: the init prompt is sent as the chat's next message.
export async function initProject() {
  if (windowBusy()) {
    notify("warn", "Agent is busy, please wait before initializing the project...");
    return;
  }
  try {
    const answer = await api.initPrompt();
    await api.initProject();
    if (answer?.prompt) await sendPrompt(answer.prompt);
  } catch (error) {
    notify("error", errorText(error));
  }
}

// A prompt the palette sends: always a prompt (never a shell command, even
// in "!" mode), and the draft in the typing box is left alone.
export async function sendPrompt(text) {
  return sendPromptText(text);
}

// CMD-056.
export function quitNow() {
  closeWindow();
}
