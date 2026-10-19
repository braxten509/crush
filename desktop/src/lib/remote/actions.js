// Phone actions the window runs (desktop-design-5-6 2.27 REM-010; the
// terminal's internal/ui/model/remote.go:355-440): new_chat_send, yolo and
// mode. Each is answered with POST remote/actions/{id}/done within 25 s.
import { runAction } from "../actions.js";
import { windowBusy } from "../busy.js";
import { api } from "../engine/api.js";
import { ensureSession, sessions } from "../stores/sessions.svelte.js";

const BUSY = "Agent is busy, please wait...";
const SWITCHED = "the Crush window switched to another chat";

async function done(id, answer) {
  try {
    await api.remoteActionDone(id, answer);
  } catch (error) {
    console.error("remote action answer failed", error?.message ?? error);
  }
}

export async function runRemoteAction(action) {
  const id = action?.action_id ?? action?.id;
  if (!id) return;
  if (!["new_chat_send", "yolo", "mode"].includes(action.kind)) return; // the engine runs every other kind
  // The phone acts on the chat it saw; no chat is the window's new chat
  // (remote.go:369-377).
  if ((action.session_id ?? "") !== (sessions.currentId ?? "")) return done(id, { error: SWITCHED });
  try {
    switch (action.kind) {
      case "new_chat_send": {
        const sid = await ensureSession();
        // The window may have opened another chat while the new one was made.
        if (sessions.currentId && sessions.currentId !== sid) return done(id, { error: SWITCHED });
        return done(id, { session_id: sid });
      }
      case "yolo":
        runAction("yolo.toggle");
        return done(id, {});
      case "mode":
        if (windowBusy()) return done(id, { error: BUSY });
        runAction("mode.cycle");
        return done(id, {});
    }
  } catch (error) {
    await done(id, { error: error?.message ?? String(error) });
  }
}
