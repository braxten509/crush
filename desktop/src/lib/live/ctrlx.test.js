import { readFile } from "node:fs/promises";
import { describe, expect, it } from "vitest";

// Ctrl+X is cut, as everywhere else on the desktop (owner, 2026-10-27): no
// app-wide binding takes it for Background Processes, so the page's own cut
// runs. Lists that use it to delete (sessions, projects, themes) keep it.
describe("Ctrl+X", () => {
  it("is not bound app-wide and is not the Background Processes shortcut", async () => {
    const live = await readFile(new URL("../plugins/live.js", import.meta.url), "utf8");
    expect(live).not.toMatch(/bindKey\("global", "Ctrl\+X"/);
    const entries = await readFile(new URL("../palette/entries.js", import.meta.url), "utf8");
    expect(entries).not.toMatch(/"background".*shortcut: "ctrl\+x"/);
  });
});
