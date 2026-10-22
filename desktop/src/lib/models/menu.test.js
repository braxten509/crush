import { describe, expect, it } from "vitest";
import { menuGroups, RECENT, searchRows, startGroup } from "./menu.js";

const row = (group, provider, model, extra = {}) => ({ id: `${group}:${model}`, group, provider: { id: provider, name: group }, model: { id: model, name: model }, configured: false, recent: false, ...extra });
const rows = [
  row(RECENT, "claude", "opus", { recent: true }),
  row("Claude Code", "claude", "opus", { configured: true }),
  row("Claude Code", "claude", "sonnet", { configured: true }),
  row("Codex", "codex", "gpt-6"),
];

describe("model menu", () => {
  it("groups rows by provider in order, marking set-up groups", () => {
    const groups = menuGroups(rows);
    expect(groups.map((g) => [g.name, g.items.length, g.configured])).toEqual([
      [RECENT, 1, false],
      ["Claude Code", 2, true],
      ["Codex", 1, false],
    ]);
  });

  it("starts on the current model's own provider, not Recently used", () => {
    expect(startGroup(menuGroups(rows), { provider: "claude", model: "opus" })).toBe("Claude Code");
    expect(startGroup(menuGroups(rows), { provider: "none", model: "x" })).toBe("Claude Code");
  });

  it("searches provider and model names across groups without recent copies", () => {
    expect(searchRows(rows, "  ").length).toBe(0);
    expect(searchRows(rows, "code OP").map((r) => r.id)).toEqual(["Claude Code:opus"]);
    expect(searchRows(rows, "gpt").map((r) => r.id)).toEqual(["Codex:gpt-6"]);
  });

  it("shows an OpenRouter provider with nothing saved yet, in name order, and marks where + adds", () => {
    const groups = menuGroups(rows, [{ id: "openrouter", name: "OpenRouter" }]);
    expect(groups.map((g) => [g.name, g.items.length, g.addTo ?? ""])).toEqual([
      [RECENT, 1, ""],
      ["Claude Code", 2, ""],
      ["Codex", 1, ""],
      ["OpenRouter", 0, "openrouter"],
    ]);
    const saved = [...rows, row("OpenRouter", "openrouter", "moonshot/kimi-k3", { configured: true })];
    expect(menuGroups(saved, [{ id: "openrouter", name: "OpenRouter" }]).map((g) => [g.name, g.items.length, g.addTo ?? ""]).at(-1)).toEqual(["OpenRouter", 1, "openrouter"]);
    expect(menuGroups(rows, [{ id: "a-first", name: "Aardvark" }]).map((g) => g.name)).toEqual([RECENT, "Aardvark", "Claude Code", "Codex"]);
  });
});
