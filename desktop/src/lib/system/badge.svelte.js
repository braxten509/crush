// The window's alert state for the taskbar (desktop-design-5-6 2.18,
// NOTE-007 to NOTE-009): blocked while a permission ask, a question form, a
// secure form or the AWS sign-in waits; working while its chat is busy
// (unless options.progress is false); done or error after a turn ended there
// while unfocused, cleared when the window gains focus; idle otherwise. Each
// change goes to alert_window_state, which keeps the app-wide table. With
// it go the chats this window sees waiting for an answer, which the badge
// counts across the app (owner, 2026-10-25).
import { windowBusy } from "../busy.js";
import { dialogs } from "../dialogs.js";
import { alertWindowState } from "../engine/commands.js";
import { projectDir } from "../stores/composer.svelte.js";
import { folders } from "../stores/folders.svelte.js";
import { openChats } from "../stores/openchats.svelte.js";
import { prompts } from "../stores/prompts.svelte.js";
import { rootOf, sessions } from "../stores/sessions.svelte.js";
import { ws } from "../stores/workspace.svelte.js";
import { onFocusGain, windowFocus } from "./focus.js";
import { waitingChats } from "./waiting.js";

const marks = $state({ done: false, error: false, focused: windowFocus.focused });

export const markDone = () => (marks.done = true);
export const markError = () => (marks.error = true);
export const focusChanged = () => (marks.focused = windowFocus.focused);

function stateNow() {
  const blocked = Boolean(prompts.permission || prompts.question || dialogs.some((d) => d.id === "aws_sso"));
  if (blocked) return "blocked";
  if (windowBusy() && ws.config?.options?.progress !== false) return "working";
  if (marks.error) return "error";
  if (marks.done) return "done";
  return "idle";
}

let started = false;
let last = "";

export function startBadge() {
  if (started) return;
  started = true;
  onFocusGain(() => {
    marks.done = false;
    marks.error = false;
  });
  $effect.root(() => {
    $effect(() => {
      const state = stateNow();
      const focused = marks.focused;
      const sid = sessions.currentId ?? "";
      const dir = projectDir() ?? "";
      const pendingOf = (id) => sessions.byId[id]?.pending_prompt || folders.states[id]?.pending_prompt || "";
      const asked = Boolean(prompts.permission || prompts.question);
      const waiting = waitingChats(openChats.list.map((c) => c?.id).filter(Boolean), pendingOf, rootOf(sid) || sid, asked);
      const key = `${state}|${focused}|${sid}|${dir}|${waiting.join(",")}`;
      if (key === last) return;
      last = key;
      alertWindowState(state, dir, sid, focused, waiting).catch(() => {});
    });
  });
}
