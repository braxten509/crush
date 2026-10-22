import { describe, expect, it } from "vitest";
import { stackedChats } from "./timelinestack.js";

describe("stackedChats", () => {
  const loaded = (ids) => (id) => ids.includes(id);

  it("keeps listed loaded chats in list order, whichever one shows", () => {
    const list = ["a", "b", "c"];
    expect(stackedChats("a", list, loaded(["a", "b", "c"]))).toEqual(["a", "b", "c"]);
    expect(stackedChats("c", list, loaded(["a", "b", "c"]))).toEqual(["a", "b", "c"]);
  });

  it("leaves out chats not loaded yet, but always has the shown one", () => {
    expect(stackedChats("b", ["a", "b", "c"], loaded(["c"]))).toEqual(["b", "c"]);
    expect(stackedChats("x", ["a"], loaded(["a"]))).toEqual(["a", "x"]);
    expect(stackedChats("", ["a", "a"], loaded(["a"]))).toEqual(["a"]);
  });
});
