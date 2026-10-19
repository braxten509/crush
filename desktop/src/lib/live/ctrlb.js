// Ctrl+B (desktop-design-5-6 2.15; LIVE-003, CHAT-061): in a chat, whatever
// has focus except a sheet, POST …/sessions/{sid}/background → {moved}. When
// the engine moved the running command, its message event changes that tool
// result and P3.6's group draws it with the background status; LIVE hides the
// 60-second hint for it at once (the terminal's Backgrounded,
// internal/ui/model/keypress.go:600-608). When nothing moved, the engine
// design's info (5.4 Rules). With no chat the key does nothing.
import { api } from "../engine/api.js";
import { chatOf } from "../stores/messages.svelte.js";
import { sessions } from "../stores/sessions.svelte.js";
import { notify } from "../stores/ui.svelte.js";
import { markBackgrounded, runningCalls } from "./hint.js";

export const NOTHING_MOVED = "No running command can move to the background";

// The key's handler. It always takes the key, as the terminal's does
// (keypress.go:600-608); with no chat it does nothing else.
export function backgroundKey() {
  const sid = sessions.currentId;
  if (sid) moveToBackground(sid);
  return true;
}

export async function moveToBackground(sid) {
  // The commands that run now in this chat's groups: the one the engine moves is among them.
  const running = runningCalls((messageId) => chatOf(messageId) === sid);
  let answer;
  try {
    answer = await api.background(sid);
  } catch (error) {
    notify("error", error?.message ?? String(error));
    return false;
  }
  if (answer?.moved === true) {
    markBackgrounded(running);
    return true;
  }
  notify("info", NOTHING_MOVED);
  return false;
}
