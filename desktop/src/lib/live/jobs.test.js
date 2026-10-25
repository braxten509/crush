import { describe, expect, it } from "vitest";


import { lastingJobs } from "./jobs.svelte.js";

describe("the Background list", () => {
  it("leaves out jobs that have run 4 seconds or less", () => {
    const now = Date.parse("2026-10-25T12:00:10Z");
    const job = (pid, started) => ({ pid, started });
    const list = [job(1, "2026-10-25T12:00:09Z"), job(2, "2026-10-25T12:00:06Z"), job(3, "2026-10-25T12:00:05.999Z"), job(4, "")];
    expect(lastingJobs(list, now).map((j) => j.pid)).toEqual([3, 4]);
  });
});
