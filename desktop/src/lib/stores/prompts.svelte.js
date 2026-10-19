// What waits for the user (desktop-design 1.5.3, 2.7): the permission request
// the composer slot draws (P3.7), the question batch (phase 5 draws it in the
// same slot), each chat's waiting marks, and each tool call's permission state.
// The engine sends permission and question events only to the windows viewing
// that chat or its root (internal/server/events.go:144-182), so a request here
// belongs to the shown chat; the stored one is dropped when the window shows
// another chat, and the engine sends it again when the window comes back
// (republishPendingPrompts, internal/backend/events.go:77-107).
import { api } from "../engine/api.js";
import { onEvent, onResync } from "../engine/events.js";
import { markPromptPending, readStart, rootOf, sessions, upsertSession } from "./sessions.svelte.js";
import { closeSlot } from "./ui.svelte.js";

export const prompts = $state({
  permission: null, // the shown PermissionRequest, or null
  question: null, // the shown question batch (proto.QuestionRequest), or null
  pendingByRoot: {}, // root chat id → {permission, question}: the prompt_pending marks (engine design 5.1(e))
  callStatus: {}, // tool_call_id → "permission" | "running" | "cancelled" (CHAT-026); a result overrides it
});

// The chat a prompt event belongs to. A helper's prompt names the helper's
// chat ("<message id>$$<tool call id>"); it counts for its root. A helper the
// window cannot place yet still reached this window, which the engine does
// only for the chat this window shows, so it counts for that one.
function promptRoot(sessionId) {
  const root = rootOf(sessionId);
  if (root) return root;
  return String(sessionId ?? "").includes("$$") ? sessions.currentId : "";
}

onEvent("permission_request", (_kind, request) => {
  if (!request?.tool_call_id || !promptRoot(request.session_id)) return;
  prompts.permission = request; // a newer request replaces the shown one (DLG-119)
});

// DLG-119, as internal/ui/model/agent_events.go:128-151: neither flag is the
// "requested" mark, sent just before the request (internal/permission/
// permission.go:216-219, :281), so it never closes the slot; granted runs the
// tool; denied keeps "permission" until the tool's result shows the denial.
// Only a final answer for the shown request's tool call closes the slot, which
// is how an answer given in another app closes it here. A call this window
// already marked cancelled (CHAT-026) stays cancelled.
onEvent("permission_notification", (_kind, note) => {
  const id = note?.tool_call_id;
  if (!id || !promptRoot(note.session_id)) return;
  if (prompts.callStatus[id] !== "cancelled") prompts.callStatus[id] = note.granted ? "running" : "permission";
  if (!note.granted && !note.denied) return;
  if (prompts.permission?.tool_call_id === id) {
    prompts.permission = null;
    closeSlot("permission");
  }
});

onEvent("question_batch_request", (_kind, request) => {
  if (!request || !promptRoot(request.session_id)) return;
  prompts.question = request;
});

// Only one question batch can be pending at a time, so any notification means
// the shown one was answered somewhere (agent_events.go:115-126).
onEvent("question_batch_notification", () => {
  prompts.question = null;
  closeSlot("question");
});

// The waiting marks of the chat list (LIST-012). session_id is already the
// root chat. A chat this window has not seen is dropped: its first read
// brings its pending_prompt (1.5.3).
onEvent("prompt_pending", (_kind, mark) => {
  const root = mark?.session_id;
  if (!root || !sessions.byId[root]) return;
  if (mark.kind !== "permission" && mark.kind !== "question") return;
  const marks = { ...(prompts.pendingByRoot[root] ?? { permission: false, question: false }) };
  marks[mark.kind] = mark.pending === true;
  prompts.pendingByRoot[root] = marks;
  markPromptPending(root, marks.permission ? "permission" : marks.question ? "question" : "");
});

// A chat read brings pending_prompt (a permission wins when both wait): the
// marks start from it, so a later event for the other kind keeps the right one.
export function seedMarks(root, pendingPrompt) {
  prompts.pendingByRoot[root] = { permission: pendingPrompt === "permission", question: pendingPrompt === "question" };
}

// This window answered the shown request: close it at once (DLG-119).
export function clearPermission(toolCallId) {
  if (prompts.permission && (!toolCallId || prompts.permission.tool_call_id === toolCallId)) {
    prompts.permission = null;
    closeSlot("permission");
  }
}

// The window now shows root ("" is the landing page): prompts of another chat
// can no longer be answered here, and the engine stops sending their outcome.
export function keepPromptsFor(root) {
  if (prompts.permission && promptRoot(prompts.permission.session_id) !== root) {
    prompts.permission = null;
    closeSlot("permission");
  }
  if (prompts.question && promptRoot(prompts.question.session_id) !== root) {
    prompts.question = null;
    closeSlot("question");
  }
}

// After a reconnect: what the shown chat waits for, read again (1.5.3). A
// request answered while the stream was down is dropped; one still pending is
// sent again by the engine when the window rejoins (1.2.6 step 3).
onResync(async () => {
  const sid = sessions.currentId;
  if (!sid) return;
  const since = readStart();
  const read = await api.agentSession(sid);
  if (!read?.id || sessions.currentId !== sid) return;
  upsertSession(read, { since });
  const waiting = sessions.byId[sid]?.pending_prompt ?? "";
  if (waiting !== "permission") clearPermission();
  if (waiting !== "question" && prompts.question) {
    prompts.question = null;
    closeSlot("question");
  }
});
