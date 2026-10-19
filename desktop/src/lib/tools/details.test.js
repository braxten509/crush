// Saved results keep only a review summary; chat diffs come from the saved
// review record (internal/message/reviews.go separateReviews, LoadReview).
import { describe, expect, it } from "vitest";
import { detailResult, detailSource } from "./details.js";
import { detailFor, detailState, requestDetail } from "./details.svelte.js";

const saved = (fields = {}) => ({
  id: "c1",
  name: "edit",
  input: JSON.stringify({ file_path: "/w/a.go", old_string: "a", new_string: "b" }),
  messageId: "m1",
  result: { tool_call_id: "c1", content: "ok", metadata: JSON.stringify({ additions: 1, removals: 1 }), is_error: false, review: { id: "t1", session_id: "s1", summary: { files: 1 } } },
  ...fields,
});

const restored = (detail) => ({ id: "t1", parts: [{ type: "tool_result", data: { tool_call_id: "c1", content: "ok", is_error: false, ...detail } }] });

describe("which results need their record", () => {
  it("an edit-like tool whose saved result lost its preview", () => {
    expect(detailSource(saved(), "x")).toEqual({ sessionId: "s1", messageId: "t1" });
    const noSession = saved();
    noSession.result.review = { id: "t1", summary: { files: 1 } };
    expect(detailSource(noSession, "x")).toEqual({ sessionId: "x", messageId: "t1" });
  });
  it("not a result that still has its preview, has no summary, or is another tool", () => {
    const live = saved();
    live.result.metadata = JSON.stringify({ old_content: "a\n", new_content: "b\n" });
    expect(detailSource(live, "s")).toBeNull();
    const full = saved();
    full.result.review = { changes: [] };
    expect(detailSource(full, "s")).toBeNull();
    expect(detailSource(saved({ name: "bash" }), "s")).toBeNull();
    expect(detailSource(saved({ result: null }), "s")).toBeNull();
  });
});

describe("restoring a result", () => {
  it("takes the detail's own preview when it kept one", () => {
    const message = restored({ metadata: JSON.stringify({ diff: "--- a\n+++ b\n" }) });
    expect(JSON.parse(detailResult(saved({ name: "write" }), message).metadata)).toEqual({ diff: "--- a\n+++ b\n" });
  });
  it("rebuilds the preview from the file's snapshots", () => {
    const message = restored({
      metadata: JSON.stringify({ additions: 1, removals: 1, edits_applied: 2 }),
      review: { changes: [{ path: "/w/other.go", after: { content: "x" } }, { path: "/w/a.go", before: { content: "a\n" }, after: { content: "b\n" } }] },
    });
    const result = detailResult(saved(), message);
    expect(JSON.parse(result.metadata)).toEqual({ additions: 1, removals: 1, edits_applied: 2, old_content: "a\n", new_content: "b\n" });
    expect(result.review).toEqual({ id: "t1", session_id: "s1", summary: { files: 1 } });
  });
  it("finds nothing for a call the message does not hold", () => {
    expect(detailResult(saved({ id: "other" }), restored({}))).toBeNull();
  });
});

describe("the record cache", () => {
  it("reads each record once, then restores from it", async () => {
    const reads = [];
    let finish;
    const review = (sid, mid) => {
      reads.push([sid, mid]);
      return new Promise((resolve) => (finish = resolve));
    };
    const call = saved({ id: "c1" });
    call.result.review = { id: "cache-1", session_id: "s1", summary: { files: 1 } };
    expect(detailFor(call, "s1")).toBeNull();
    requestDetail(call, "s1", review);
    requestDetail(call, "s1", review);
    expect(reads).toEqual([["s1", "cache-1"]]);
    expect(detailState(call, "s1")).toBe("loading");
    finish({ id: "cache-1", parts: [{ type: "tool_result", data: { tool_call_id: "c1", content: "ok", metadata: JSON.stringify({ old_content: "a\n", new_content: "b\n" }) } }] });
    await Promise.resolve();
    await Promise.resolve();
    expect(detailState(call, "s1")).toBe("ready");
    expect(JSON.parse(detailFor(call, "s1").result.metadata).new_content).toBe("b\n");
    requestDetail(call, "s1", review);
    expect(reads).toHaveLength(1);
  });
  it("marks a failed read and does not ask again", async () => {
    const reads = [];
    const call = saved();
    call.result.review = { id: "cache-2", session_id: "s1", summary: { files: 1 } };
    const review = async (sid, mid) => {
      reads.push(mid);
      throw new Error("diff details are no longer saved");
    };
    requestDetail(call, "s1", review);
    await Promise.resolve();
    await Promise.resolve();
    expect(detailState(call, "s1")).toBe("failed");
    requestDetail(call, "s1", review);
    expect(reads).toEqual(["cache-2"]);
    expect(detailFor(call, "s1")).toBeNull();
  });
  it("leaves a call that needs nothing as it is", () => {
    const plain = saved({ name: "bash" });
    expect(detailFor(plain, "s1")).toBe(plain);
  });
});
