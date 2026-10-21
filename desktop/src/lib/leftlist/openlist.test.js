import { describe, expect, it } from "vitest";
import { listedRows } from "./openlist.js";

const here = (dir) => dir === "/p/here";
const session = (id, extra = {}) => ({ id, title: `Chat ${id}`, updated_at: 100, created_at: 1, ...extra });

describe("listedRows", () => {
  const byId = { a: session("a"), b: session("b"), branch: session("branch", { created_at: 2 }), lone: session("lone") };
  const parentOf = (id) => ({ branch: "a", lone: "gone" })[id] ?? null;

  it("draws the open chats in the list's order, each with its folder", () => {
    const open = [
      { id: "b", dir: "/p/here" },
      { id: "x", dir: "/p/other", title: "Other chat" },
      { id: "a", dir: "/p/here" },
    ];
    const rows = listedRows(open, { byId, parentOf, here, folderOf: (d) => d.split("/").pop() });
    expect(rows.map((r) => [r.id, r.agent, r.folder?.dir ?? null])).toEqual([
      ["b", "here", null],
      ["x", "other", "/p/other"],
      ["a", "here", null],
    ]);
    expect(rows[1].title).toBe("Other chat");
  });

  it("hangs an open branch under its open chat and leaves unlisted chats out", () => {
    const open = [
      { id: "branch", dir: "/p/here" },
      { id: "a", dir: "/p/here" },
      { id: "lone", dir: "/p/here" },
    ];
    const rows = listedRows(open, { byId, parentOf, here, selectedId: "branch" });
    expect(rows.map((r) => [r.id, r.depth])).toEqual([
      ["a", 0],
      ["branch", 1],
      ["lone", 0],
    ]);
    expect(rows.some((r) => r.id === "b")).toBe(false);
  });

  it("moves a branch of a closed chat up under the nearest open one", () => {
    const tree = { s: session("s"), b: session("b", { created_at: 2 }), d: session("d", { created_at: 3 }) };
    const up = (id) => ({ b: "s", d: "b" })[id] ?? null;
    const rows = listedRows([{ id: "s", dir: "/p/here" }, { id: "d", dir: "/p/here" }], { byId: tree, parentOf: up, here, selectedId: "d" });
    expect(rows.map((r) => [r.id, r.depth])).toEqual([
      ["s", 0],
      ["d", 1],
    ]);
  });

  it("names a chat of another folder from the catalog when it has it", () => {
    const rows = listedRows([{ id: "x", dir: "/p/other", title: "New Session" }], { here, catalog: { x: { ID: "x", Title: "Named", UpdatedAt: 50 } } });
    expect([rows[0].title, rows[0].at]).toEqual(["Named", 50]);
  });
});
