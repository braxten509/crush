// The saved review records chat bodies read their diffs from (details.js):
// each is asked for once, when an open tool needs it, and kept for the
// window's life. A record that lacks a call (its message grew after the read)
// is asked for once more. Expired or failed reads draw no diff, as the
// terminal draws none for these results.
import { SvelteMap } from "svelte/reactivity";
import { api } from "../engine/api.js";
import { detailResult, detailSource } from "./details.js";

const records = new SvelteMap(); // "<sid>\u0000<mid>" → {state: "loading"|"ready"|"failed", message, retried}

function keyOf(source) {
  return source.sessionId + "\u0000" + source.messageId;
}

// The call with its diff restored, the call itself when it needs nothing, or
// null while its record loads (or could not be read). Reads only: an effect
// calls requestDetail.
export function detailFor(call, sessionId) {
  const source = detailSource(call, sessionId);
  if (!source) return call;
  const record = records.get(keyOf(source));
  if (record?.state !== "ready") return null;
  const result = detailResult(call, record.message);
  return result ? { ...call, result } : null;
}

// Asks for the call's record when it has none yet (or once more when the
// record lacks the call). At most one read per record is in flight.
export function requestDetail(call, sessionId, review = api.review) {
  const source = detailSource(call, sessionId);
  if (!source) return;
  const key = keyOf(source);
  const record = records.get(key);
  if (record) {
    if (record.state !== "ready" || record.retried || detailResult(call, record.message)) return;
  }
  const retried = Boolean(record);
  records.set(key, { state: "loading", message: record?.message ?? null, retried });
  review(source.sessionId, source.messageId).then(
    (message) => records.set(key, { state: "ready", message, retried }),
    () => records.set(key, { state: "failed", message: null, retried }),
  );
}

// The state of a call's record, for the probe: "", "loading", "ready", "failed".
export function detailState(call, sessionId) {
  const source = detailSource(call, sessionId);
  return source ? (records.get(keyOf(source))?.state ?? "") : "";
}
