// CHAT-078: an Agent row's nested tools read from its helper chat.
import { describe, expect, it } from "vitest";
import { callsFromMessages, chatList } from "./agent.js";

const assistant = (id, parts) => ({ id, role: "assistant", parts });
const call = (id, name, finished) => ({ type: "tool_call", data: { id, name, input: "{}", ...(finished === undefined ? {} : { finished }) } });
const result = (id, content, isError = false) => ({ type: "tool_result", data: { tool_call_id: id, name: "bash", content, metadata: "", is_error: isError } });

describe("helper chat tools", () => {
  it("pairs each call with its result, in chat order", () => {
    const list = [
      assistant("a1", [call("c1", "view"), call("c2", "bash")]),
      { id: "t1", role: "tool", parts: [result("c1", "file"), result("c2", "boom", true)] },
      assistant("a2", [call("c3", "grep")]),
    ];
    const calls = callsFromMessages(list);
    expect(calls.map((c) => [c.id, c.name, c.messageId, c.status, c.result?.content ?? null])).toEqual([
      ["c1", "view", "a1", "running", "file"],
      ["c2", "bash", "a1", "running", "boom"],
      ["c3", "grep", "a2", "running", null],
    ]);
    expect(calls[0]).toMatchObject({ input: "{}", hooks: [], children: [] });
  });
  it("marks a streaming input pending and a stopped reply's open calls cancelled", () => {
    expect(callsFromMessages([assistant("a", [call("c", "bash", false)])])[0].status).toBe("pending");
    const stopped = assistant("a", [call("c", "bash"), { type: "finish", data: { reason: "canceled", time: 1 } }]);
    expect(callsFromMessages([stopped])[0].status).toBe("cancelled");
  });
  it("keeps one call per id when a later update repeats it", () => {
    const list = [assistant("a", [call("c", "bash", false)]), assistant("a", [call("c", "bash", true)])];
    expect(callsFromMessages(list)).toHaveLength(1);
    expect(callsFromMessages(list)[0].status).toBe("running");
  });
  it("reads a loaded chat in order and skips missing messages", () => {
    const byId = new Map([["a", assistant("a", [])]]);
    expect(chatList({ ids: ["a", "gone"], byId }).map((m) => m.id)).toEqual(["a"]);
    expect(chatList(null)).toEqual([]);
    expect(callsFromMessages(undefined)).toEqual([]);
  });
});
