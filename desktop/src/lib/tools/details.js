// The diffs of saved results. The engine keeps transcripts small: when it
// saves a tool result that changed files, it moves the diff preview
// (metadata old_content, new_content and diff) into a separate record and
// leaves a review summary {id, session_id, summary} on the result
// (internal/message/reviews.go separateReviews). GET sessions/{sid}/messages/
// {id}/review restores that record (LoadReview); the latest five prompts keep
// theirs. A chat body that draws a diff (Edit, Multi-Edit, Write's denied
// change, Replace Symbol: CHAT-064 to CHAT-066, CHAT-076, CHAT-113) reads it
// from there.
import { parseJSON } from "./go.js";

const PREVIEW_KEYS = ["old_content", "new_content", "diff"];
const DIFF_TOOLS = new Set(["edit", "multiedit", "write", "lsp_replace_symbol"]);

// The saved review a call's result points to, when its diff preview was
// moved out: {sessionId, messageId}; null when the result still has it or
// has no diff.
export function detailSource(call, sessionId = "") {
  const result = call?.result;
  const review = result?.review;
  if (!result || !DIFF_TOOLS.has(call.name) || !review?.summary || !review.id) return null;
  const meta = parseJSON(result.metadata ?? "");
  if (meta && typeof meta === "object" && PREVIEW_KEYS.some((k) => typeof meta[k] === "string" && meta[k] !== "")) return null;
  return { sessionId: review.session_id || sessionId, messageId: review.id };
}

// The call's result restored from a loaded review message: its own detail
// result, with the preview put back from the review's file snapshots when the
// detail kept only those (separateReviews deletes the preview when it has
// snapshots). null when the message has no result for the call.
export function detailResult(call, message) {
  let found = null;
  for (const part of message?.parts ?? []) {
    if (part?.type === "tool_result" && part.data?.tool_call_id === call.id) {
      found = part.data;
      break;
    }
  }
  if (!found) return null;
  const meta = parseJSON(found.metadata ?? "");
  const object = meta && typeof meta === "object" && !Array.isArray(meta) ? meta : {};
  if (PREVIEW_KEYS.some((k) => typeof object[k] === "string" && object[k] !== "")) return { ...call.result, ...found, review: call.result.review };
  const path = filePathOf(call);
  const change = (found.review?.changes ?? []).find((c) => sameFile(c.path, path)) ?? (found.review?.changes?.length === 1 ? found.review.changes[0] : null);
  if (!change) return { ...call.result, ...found, review: call.result.review };
  const before = change.before?.content ?? "";
  const after = change.after?.content ?? "";
  const metadata = JSON.stringify({ ...object, old_content: before, new_content: after });
  return { ...call.result, ...found, metadata, review: call.result.review };
}

function filePathOf(call) {
  const input = parseJSON(call.input ?? "");
  return input && typeof input === "object" ? String(input.file_path ?? "") : "";
}

function sameFile(a, b) {
  if (!a || !b) return false;
  return a === b || a.endsWith("/" + b) || b.endsWith("/" + a);
}
