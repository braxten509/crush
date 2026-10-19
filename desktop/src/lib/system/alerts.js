// SYS's alerts (desktop-design-5-6 2.18; NOTE-002 to NOTE-006): a finished
// turn, a permission ask, a question batch, a secure form and the memory
// pop-up. One window handles each (alert_handler: the window that shows the
// chat, else the project's lead window). A notification goes out only when
// that window is not focused or shows another chat; sounds play either way;
// style "disabled" stops both, and "bell" plays the bell instead of a
// notification. The finished alert checks twice that the chat is idle with
// nothing queued (completion_notification.go:15-60).
import { getCurrentWebviewWindow } from "@tauri-apps/api/webviewWindow";
import { api } from "../engine/api.js";
import { alertHandler, alertNotify, alertSound } from "../engine/commands.js";
import { onEvent } from "../engine/events.js";
import { projectDir } from "../stores/composer.svelte.js";
import { sessions } from "../stores/sessions.svelte.js";
import { ws } from "../stores/workspace.svelte.js";
import { windowFocus } from "./focus.js";
import { markDone, markError } from "./badge.svelte.js";

const label = (() => {
  try {
    return getCurrentWebviewWindow().label;
  } catch {
    return "";
  }
})();

// An unknown value counts as auto (notification.go:57-58).
export function style() {
  const value = String(ws.config?.options?.notifications ?? "auto").toLowerCase();
  return ["auto", "native", "osc", "bell", "disabled"].includes(value) ? value : "auto";
}

async function mine(sessionId) {
  try {
    return (await alertHandler(projectDir(), sessionId ?? "")) === label;
  } catch {
    return Boolean(sessionId) && sessionId === sessions.currentId;
  }
}

async function raise({ sessionId, sound = null, title = null, body = "" }) {
  const kind = style();
  if (kind === "disabled") return;
  if (!(await mine(sessionId))) return;
  if (sound) alertSound(sound).catch(() => {});
  if (!title) return;
  const shown = windowFocus.focused && sessionId === sessions.currentId;
  if (shown) return;
  if (kind === "bell") alertSound("bell").catch(() => {});
  else alertNotify(title, body).catch(() => {});
}

async function idle(sid) {
  try {
    const [record, queued] = await Promise.all([api.agentSession(sid), api.queuedCount(sid)]);
    const count = typeof queued === "number" ? queued : Number(queued?.count ?? 0);
    return record?.is_busy !== true && count === 0;
  } catch {
    return false;
  }
}

async function finished(event) {
  const sid = event?.session_id;
  if (!sid || !(await idle(sid))) return;
  if (!windowFocus.focused && sid === sessions.currentId) markDone();
  if (!(await idle(sid))) return;
  const title = event.session_title || sessions.byId[sid]?.title || "New session";
  raise({ sessionId: sid, sound: "complete", title: "Crush finished", body: `"${title}" is done` });
}

let started = false;

export function startAlerts() {
  if (started) return;
  started = true;
  onEvent("agent_event", (_kind, event) => {
    if (event?.type === "agent_finished") finished(event);
    else if (event?.type === "error" && event.session_id === sessions.currentId && !windowFocus.focused) markError();
  });
  onEvent("permission_request", (_kind, request) => {
    if (!request?.tool_call_id) return;
    raise({ sessionId: request.session_id, title: "Crush is waiting...", body: `Permission required to execute "${request.tool_name ?? ""}"` });
  });
  onEvent("question_batch_request", (_kind, request) => {
    const n = request?.questions?.length ?? 0;
    raise({ sessionId: request?.session_id, sound: "message-new-instant", title: "Crush is waiting...", body: `${n} questions need your input` });
  });
  // The engine sends permission asks and questions only to windows showing
  // that chat (internal/server/events.go:144-183), so a chat no window shows
  // is alerted from its prompt_pending mark, which every window gets; the
  // lead window handles it (LIST-052).
  const pendingNow = new Set();
  onEvent("prompt_pending", (_kind, mark) => {
    const key = `${mark?.session_id}|${mark?.kind}`;
    if (!mark?.pending) {
      pendingNow.delete(key);
      return;
    }
    if (pendingNow.has(key)) return;
    pendingNow.add(key);
    if (!mark.session_id || mark.session_id === sessions.currentId) return; // the prompt event itself alerts
    const chat = sessions.byId[mark.session_id]?.title || "New session";
    if (mark.kind === "permission") raise({ sessionId: mark.session_id, title: "Crush is waiting...", body: `Permission required in "${chat}"` });
    else if (mark.kind === "question") raise({ sessionId: mark.session_id, sound: "message-new-instant", title: "Crush is waiting...", body: `Questions in "${chat}" need your input` });
  });
  onEvent("secure_form", (_kind, form) => {
    raise({ sessionId: form?.session_id, sound: "message-new-instant" });
  });
  // The memory pop-up: one window wins the claim per episode; sent even
  // while focused, not with style "disabled", a beep with "bell"
  // (memory_warning.go:51-78).
  let high = false;
  const memory = async (status) => {
    const rise = status?.supported === true && status.high === true && !high;
    high = status?.high === true;
    if (!rise || style() === "disabled") return;
    try {
      const answer = await api.claimMemoryNotice(status.episode);
      if (!answer?.claimed) return;
      // Through the chosen style, as the terminal's backend: Bell beeps.
      if (style() === "bell") alertSound("bell").catch(() => {});
      else alertNotify("Crush · Memory almost full", status.message ?? "").catch(() => {});
    } catch {
      // No claim: another window shows it.
    }
  };
  onEvent("memory_status", (_kind, status) => memory(status));
  // A window that joins while memory is already high reads it once.
  onEvent("connection", (_kind, ev) => {
    if (ev?.state === "connected") api.memory().then(memory, () => {});
  });
}
