import { describe, expect, it } from "vitest";
import { folderGroups, listedRows, placeHelperRows } from "./openlist.js";

const here = (dir) => dir === "/p/here";
const session = (id, extra = {}) => ({ id, title: `Chat ${id}`, updated_at: 100, created_at: 1, ...extra });

describe("listedRows", () => {
  const byId = { a: session("a"), b: session("b"), branch: session("branch", { created_at: 2 }), lone: session("lone") };
  const parentOf = (id) => ({ branch: "a", lone: "gone" })[id] ?? null;

  it("keeps a cached chat's folder, status and time when it is selected from another folder", () => {
    const open = [{ id: "branch", dir: "/p/other" }, { id: "a", dir: "/p/other" }];
    const cached = { a: session("a", { has_unfinished_work: true }), branch: session("branch") };
    const rows = listedRows(open, { byId: cached, here, selectedId: "a", parentOf });
    expect(rows.map((row) => row.id)).toEqual(["a", "branch"]);
    expect(rows[1].folder).toEqual({ dir: "/p/other" });
    const row = rows[0];
    expect(row.folder).toEqual({ dir: "/p/other" });
    expect(row.selected).toBe(true);
    expect(row.state).toBe("Background");
    expect(row.time).not.toBe("");
  });

  it("shows what another folder's unloaded chat is doing, from its engine's check", () => {
    const open = [{ id: "x", dir: "/p/other" }, { id: "y", dir: "/p/other" }];
    const states = { x: { is_busy: true }, y: { is_busy: false, has_unfinished_work: false } };
    const rows = listedRows(open, { here, states });
    expect(rows.map((r) => [r.id, r.state, r.stateKind])).toEqual([["x", "Working…", "working"], ["y", "", ""]]);
  });

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

describe("folderGroups", () => {
  it("puts each chat, branches too, under its folder; kept folders first, even empty", () => {
    const byId = { a: session("a"), branch: session("branch", { created_at: 2 }) };
    const open = [
      { id: "x", dir: "/p/other/" },
      { id: "a", dir: "/p/here" },
      { id: "branch", dir: "/p/here" },
      { id: "y", dir: "/p/new" },
    ];
    const rows = listedRows(open, { byId, parentOf: (id) => (id === "branch" ? "a" : null), here, selectedId: "branch" });
    const key = (dir) => dir.replace(/\/+$/u, "");
    const groups = folderGroups(rows, ["/p/empty", "/p/here", "/p/other"], key);
    expect(groups.map((g) => [g.dir, g.rows.map((r) => r.id)])).toEqual([
      ["/p/empty", []],
      ["/p/here", ["a", "branch"]],
      ["/p/other", ["x"]],
      ["/p/new", ["y"]],
    ]);
  });
});

describe("placeHelperRows", () => {
  it("puts a sub-agent's chat after its parent and its earlier sub-agents, or last without a listed parent", () => {
    const rows = [{ id: "a" }, { id: "b" }];
    const helpers = [
      { id: "h1", parent: "a", helper: true },
      { id: "h2", parent: "a", helper: true },
      { id: "h3", parent: "gone", helper: true },
    ];
    expect(placeHelperRows(rows, helpers).map((r) => r.id)).toEqual(["a", "h1", "h2", "b", "h3"]);
    expect(rows.map((r) => r.id)).toEqual(["a", "b"]);
  });
});
