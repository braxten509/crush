import { describe, expect, it, vi } from "vitest";

vi.mock("../engine/api.js", () => ({ api: { fallback: vi.fn(), refreshLimits: vi.fn() } }));
vi.mock("../engine/events.js", () => ({ onEvent: vi.fn(), onResync: vi.fn() }));
vi.mock("../engine/commands.js", () => ({ sign_in_run: vi.fn() }));

import { accountGroups } from "./accountmeters.svelte.js";

// The engine keeps a reading "ready" after a failed ask and only adds the
// error (owner, 2026-11-12: a working sign-in showed 61% left when 45% was
// true): its numbers are old and must not look current.
describe("a failed ask after a reading", () => {
  it("marks the numbers old", () => {
    const accounts = [{ id: "c2", kind: "claude-code", name: "Work", signed_in: true }];
    const reading = (extra) => ({
      kind: "claude-code", account: "c2", state: "ready", read_at: "2026-10-21T11:59:00Z",
      windows: [{ name: "5h", used: 39 }], ...extra,
    });
    const state = (extra) => accountGroups({ accounts, byKind: { "claude-code#c2": reading(extra) }, now: Date.parse("2026-10-21T12:00:00Z") })[0].rows[0].state;
    expect(state({ error: "429 Too Many Requests" })).toBe("idle");
    expect(state({})).toBe("ready");
  });
});
