// What the window shows, for the phone (desktop-design-5-6 2.27 REM-011):
// POST remote/presence {session_id, focused, plan} when the shown chat, the
// window's focus or plan mode changes, and only then.
import { api } from "../engine/api.js";

let last = "";

export function sendPresence({ sessionId, focused, plan }) {
  const key = `${sessionId}|${focused}|${plan}`;
  if (key === last) return;
  last = key;
  api.remotePresence({ session_id: sessionId ?? "", focused: focused === true, plan: plan === true }).catch(() => {
    last = ""; // try again on the next change
  });
}

export function resetPresence() {
  last = "";
}
