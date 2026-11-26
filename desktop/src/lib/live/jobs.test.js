import { describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  handlers: new Map(),
  folderHandlers: new Map(),
  resyncs: new Set(),
  jobsCalls: [],
  serversCalls: [],
  bySession: {},
}));

vi.mock("../engine/api.js", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    api: {
      ...actual.api,
      jobs: vi.fn((sid) => new Promise((resolve) => state.jobsCalls.push({ sid, resolve }))),
      servers: vi.fn(() => new Promise((resolve) => state.serversCalls.push({ resolve }))),
      killJob: vi.fn(async () => ({})),
    },
  };
});

vi.mock("../engine/events.js", () => ({
  onEvent: (type, fn) => state.handlers.set(type, fn),
  onFolderEvent: (type, fn) => state.folderHandlers.set(type, fn),
  onResync: (fn) => state.resyncs.add(fn),
  withHeld: async (_types, fn) => fn(),
}));

vi.mock("../stores/messages.svelte.js", () => ({
  messages: {
    get bySession() {
      return state.bySession;
    },
  },
}));

vi.mock("../stores/ui.svelte.js", () => ({
  notify: vi.fn(),
}));

import { sessions } from "../stores/sessions.svelte.js";
import { jobs, lastingJobs, startJobs, trayJobs } from "./jobs.svelte.js";

describe("the Background list", () => {
  it("shows brand-new background jobs right away", () => {
    const job = (pid, started) => ({ pid, started });
    const list = [job(1, "2026-10-25T12:00:09Z"), job(2, "2026-10-25T12:00:06Z"), job(3, "")];
    expect(lastingJobs(list).map((j) => j.pid)).toEqual([1, 2, 3]);
  });

  it("applies live job events right away without waiting for polling", () => {
    state.bySession = { chatA: { list: [] } };
    sessions.opening = null;
    sessions.currentId = "chatA";
    jobs.sid = "chatA";
    jobs.list = [];
    jobs.servers = [];
    startJobs();

    const onJob = state.handlers.get("job");
    const onFolderJob = state.folderHandlers.get("job");
    expect(onJob).toBeTypeOf("function");
    expect(onFolderJob).toBeTypeOf("function");

    onJob("created", { job_id: "00C", command: "record_video", session_id: "chatA", started: "2026-11-25T20:09:42Z" });
    expect(trayJobs().map((j) => j.job_id)).toEqual(["00C"]);

    onFolderJob("created", { job_id: "00D", command: "build", session_id: "chatA", started: "2026-11-25T20:10:00Z" });
    expect(trayJobs().map((j) => j.job_id)).toEqual(["00C", "00D"]);

    onJob("deleted", { job_id: "00C", session_id: "chatA" });
    expect(trayJobs().map((j) => j.job_id)).toEqual(["00D"]);
  });
});
