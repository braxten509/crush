import { describe, expect, it } from "vitest";
import { waitingChats } from "./waiting.js";

// The chats this window sees waiting for an answer, for the app-wide badge.
describe("waitingChats", () => {
  it("lists every open chat waiting for a question or permission, and the shown chat while its form is up", () => {
    const pending = { a: "question", b: "permission", c: "", d: "question" };
    expect(waitingChats(["a", "b", "c", "e"], (id) => pending[id], "e", true)).toEqual(["a", "b", "e"]);
    expect(waitingChats(["a"], (id) => pending[id], "a", true)).toEqual(["a"]);
    expect(waitingChats(["c"], (id) => pending[id], "c", false)).toEqual([]);
  });
});
