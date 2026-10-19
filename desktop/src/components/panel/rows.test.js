import { describe, expect, it } from "vitest";
import { goCompare, lspRows, mcpCountWords, mcpRows, mcpTitle, skillRows } from "./rows.js";

// SIDE-009 to SIDE-011: ports of internal/ui/model/lsp.go, mcp.go and skills.go.

describe("goCompare (strings.Compare)", () => {
  it("orders by code point, as Go orders UTF-8 bytes", () => {
    expect(["b", "B", "a", "_x", "é", "z"].sort(goCompare)).toEqual(["B", "_x", "a", "b", "z", "é"]);
    // An astral character sorts after U+FFFF in UTF-8, though not in UTF-16.
    expect(goCompare("\u{1F600}", "�")).toBe(1);
    expect(goCompare("ab", "abc")).toBe(-1);
    expect(goCompare("abc", "abc")).toBe(0);
  });
});

describe("lspRows", () => {
  const lsps = {
    tsserver: { name: "tsserver", state: 2, diagnostics: { error: 1, warning: 2, hint: 3, information: 4 } },
    gopls: { name: "gopls", state: 2, diagnostics: { error: 0, warning: 0, hint: 0, information: 0 } },
    rust: { name: "rust", state: 1 },
    pyright: { name: "pyright", state: 3, error: "exec: not found" },
    clangd: { name: "clangd", state: 0 },
    zls: { name: "zls", state: 4 },
    nil: { name: "nil", state: 5 },
    odd: { name: "odd", state: 9 },
    bare: { name: "bare", state: 3 },
  };

  it("sorted by name; each state's icon and words; other states left out", () => {
    expect(lspRows(lsps).rows).toEqual([
      { name: "bare", status: "error", text: "error" },
      { name: "clangd", status: "offline", text: "unstarted" },
      { name: "gopls", status: "online", text: "", counts: { error: 0, warning: 0, hint: 0, information: 0 } },
      { name: "nil", status: "disabled", text: "disabled" },
      { name: "pyright", status: "error", text: "error: exec: not found" },
      { name: "rust", status: "busy", text: "starting..." },
      { name: "tsserver", status: "online", text: "", counts: { error: 1, warning: 2, hint: 3, information: 4 } },
      { name: "zls", status: "offline", text: "stopped" },
    ]);
    expect(lspRows({}).rows).toEqual([]);
  });

  it("cut at max: max-1 rows, then the terminal's count (len - max)", () => {
    expect(lspRows(lsps, { max: 3 })).toMatchObject({ more: 5 });
    expect(lspRows(lsps, { max: 3 }).rows.map((r) => r.name)).toEqual(["bare", "clangd"]);
    expect(lspRows(lsps, { max: 8 })).toMatchObject({ more: 0 });
    expect(lspRows(lsps, { max: 8 }).rows).toHaveLength(8);
    expect(lspRows(lsps, { max: 0 })).toEqual({ rows: [], more: 0 });
  });
});

describe("mcpRows", () => {
  const mcps = {
    docker: { name: "docker", state: "connected", tool_count: 3, prompt_count: 0, resource_count: 2 },
    fs: { name: "fs", state: "connected", tool_count: 0, channel: true },
    broken: { name: "broken", state: "error", error: "dial: refused" },
    gh: { name: "gh", state: "needs auth" },
    off: { name: "off", state: "disabled" },
    boot: { name: "boot", state: "starting" },
    weird: { name: "weird", state: "unknown" },
    stray: { name: "stray", state: "connected" },
  };
  const configured = { fs: {}, docker: {}, broken: {}, gh: {}, off: { disabled: true }, boot: {}, weird: {}, missing: {} };

  it("only configured servers with a state, sorted by name; Docker MCP named so", () => {
    expect(mcpRows(mcps, configured).rows).toEqual([
      { name: "boot", status: "busy", text: "starting..." },
      { name: "broken", status: "error", text: "error: dial: refused" },
      { name: "Docker MCP", status: "online", text: "", counts: { tools: 3, prompts: 0, resources: 2, channel: false } },
      { name: "fs", status: "online", text: "", counts: { tools: 0, prompts: 0, resources: 0, channel: true } },
      { name: "gh", status: "auth", text: "needs authentication" },
      { name: "off", status: "disabled", text: "disabled" },
      { name: "weird", status: "offline", text: "" },
    ]);
    expect(mcpTitle("docker")).toBe("Docker MCP");
  });

  it("the counts' words skip zeros; channel last", () => {
    expect(mcpCountWords({ tools: 3, prompts: 0, resources: 2, channel: false })).toBe("3 tools 2 resources");
    expect(mcpCountWords({ tools: 0, prompts: 1, resources: 0, channel: true })).toBe("1 prompts channel");
    expect(mcpCountWords({ tools: 0, prompts: 0, resources: 0, channel: false })).toBe("");
  });

  it("the same cut as the LSP list", () => {
    expect(mcpRows(mcps, configured, { max: 4 })).toMatchObject({ more: 3 });
    expect(mcpRows(mcps, configured, { max: 4 }).rows).toHaveLength(3);
  });
});

describe("skillRows", () => {
  const skills = [
    { id: "/home/u/.agents/skills/zeta/SKILL.md", name: "zeta" },
    { id: "/home/u/.agents/skills/alpha/SKILL.md", name: "alpha" },
    { id: "/repo/.crush/skills/alpha/SKILL.md", name: "alpha" },
    { id: "/home/u/.agents/skills/noname/SKILL.md", name: "" },
    { id: "builtin/crush-config/SKILL.md", name: "crush-config" },
    { id: "/home/u/.agents/skills/beta/SKILL.md", name: "beta" },
  ];

  it("sorted by name, one row per name, minus the disabled ones", () => {
    expect(skillRows(skills, ["beta"]).rows.map((r) => r.name)).toEqual(["alpha", "crush-config", "noname", "zeta"]);
    expect(skillRows([], []).rows).toEqual([]);
  });

  it("cut: max-1 rows, then the rest counted (len - (max-1))", () => {
    const thirty = Array.from({ length: 30 }, (_, i) => ({ id: `/s/skill-${String(i).padStart(2, "0")}/SKILL.md`, name: `skill-${String(i).padStart(2, "0")}` }));
    const cut = skillRows(thirty, [], { max: 20 });
    expect(cut.rows).toHaveLength(19);
    expect(cut.more).toBe(11);
    expect(cut.rows.at(-1).name).toBe("skill-18");
    expect(skillRows(thirty, []).rows).toHaveLength(30);
  });
});
