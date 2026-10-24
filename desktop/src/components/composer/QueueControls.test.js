import { readFile } from "node:fs/promises";
import { describe, expect, it } from "vitest";

for (const file of ["QueueLine.svelte", "QueueSection.svelte"]) {
  describe(file, () => {
    it("labels the queue action Interrupt and preserves queued prompts", async () => {
      const source = await readFile(new URL(`./${file}`, import.meta.url), "utf8");

      expect(source).toMatch(/import \{\s*interruptAndSend\s*\} from .*composer\.svelte\.js/);
      expect(source).toMatch(/onclick=\{\(\) => interruptAndSend\(\)\}/);
      expect(source).toMatch(/>Interrupt<\/button>/);
      expect(source).not.toMatch(/clear\s*\(/);
    });
  });
}
