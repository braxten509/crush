import { readFile } from "node:fs/promises";
import { describe, expect, it } from "vitest";

// A sub-agent's row only hides (the sub-agent keeps working), so its button
// is a dash, not the ×, and says Hide (owner, 2026-10-24).
describe("ChatRow.svelte", () => {
  it("draws a dash labelled Hide on a sub-agent's row and the × elsewhere", async () => {
    const source = await readFile(new URL("./ChatRow.svelte", import.meta.url), "utf8");
    expect(source).toMatch(/aria-label=\{row\.helper \? `Hide \$\{row\.title\}` : `Close \$\{row\.title\}`\}/);
    expect(source).toMatch(/<Icon name=\{row\.helper \? "minimize" : "close"\}/);
  });
});
