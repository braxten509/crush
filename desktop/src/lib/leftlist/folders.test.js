import { describe, expect, it } from "vitest";
import { folderGroups, folderName } from "./folders.js";

const chat = (ID, Directory, UpdatedAt, extra = {}) => ({ ID, Directory, DataDirectory: `${Directory}/.crush`, Title: ID, UpdatedAt, ...extra });

describe("folderGroups", () => {
  const catalog = [
    chat("a1", "/p/here", 50),
    chat("b1", "/p/beta", 10),
    chat("b2", "/p/beta", 40),
    chat("c1", "/p/gamma", 30),
    chat("c2", "/p/gamma", 20),
    chat("b3", "/p/beta", 45, { ParentSessionID: "b2" }),
  ];
  const here = (dir) => dir === "/p/here";

  it("groups the other folders' chats under their folder, newest folder and chat first", () => {
    const groups = folderGroups(catalog, { here });
    expect(groups.map((g) => [g.label, g.rows.map((r) => r.id)])).toEqual([
      ["beta", ["b2", "b1"]],
      ["gamma", ["c1", "c2"]],
    ]);
    expect(groups[0].rows[0].folder).toEqual({ dir: "/p/beta", dataDir: "/p/beta/.crush" });
  });

  it("leaves out chats closed in their own folder's set, and a folder left with none", () => {
    const isClosedIn = (dataDir, id) => (dataDir === "/p/gamma/.crush" && id.startsWith("c")) || (dataDir === "/p/beta/.crush" && id === "b2");
    const groups = folderGroups(catalog, { here, isClosedIn });
    expect(groups.map((g) => [g.label, g.rows.map((r) => r.id)])).toEqual([["beta", ["b1"]]]);
  });
});

describe("folderGroups' branches and agents", () => {
  it("leaves out a folder's known branches and names each chat's agent", () => {
    const catalog = [chat("b1", "/p/beta", 10, { ContextBudget: { Provider: "codex-cli" } }), chat("b2", "/p/beta", 20)];
    const groups = folderGroups(catalog, { isBranch: (dataDir, id) => dataDir === "/p/beta/.crush" && id === "b2", agentOf: (p) => (p ? `agent ${p}` : "") });
    expect(groups[0].rows.map((r) => [r.id, r.agent])).toEqual([["b1", "agent codex-cli"]]);
  });
});

describe("folderName", () => {
  it("is the folder's last part", () => {
    expect(folderName("/home/me/dev/crush/")).toBe("crush");
    expect(folderName("")).toBe("");
  });
});
