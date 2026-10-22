// What the window shows, for the phone (desktop-design-5-6 2.27 REM-011):
// POST remote/presence {session_id, focused, plan} when the shown chat, the
// window's focus or plan mode changes, and only then. One report at a time,
// newest first in line: two reports in flight together could reach the
// engine out of order and leave it with the older one.
import { api } from "../engine/api.js";

let last = ""; // the report sent or being sent
let sending = false;
let next = null; // the newest report, waiting for the one in flight

export function sendPresence({ sessionId, focused, plan }) {
  const key = `${sessionId}|${focused}|${plan}`;
  if (key === last) return;
  last = key;
  next = { session_id: sessionId ?? "", focused: focused === true, plan: plan === true };
  if (!sending) flush();
}

function flush() {
  const body = next;
  next = null;
  sending = true;
  api
    .remotePresence(body)
    .catch(() => {
      if (!next) last = ""; // try again on the next change
    })
    .finally(() => {
      sending = false;
      if (next) flush();
    });
}

export function resetPresence() {
  last = "";
  next = null;
}
