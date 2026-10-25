import { beforeEach, describe, expect, it, vi } from "vitest";

const calls = vi.hoisted(() => ({ fallback: vi.fn(), refreshLimits: vi.fn(), setFallbackOrder: vi.fn() }));
vi.mock("../engine/api.js", () => ({
  api: { fallback: calls.fallback, refreshLimits: calls.refreshLimits, setFallbackOrder: calls.setFallbackOrder },
}));
vi.mock("../engine/commands.js", () => ({ sign_in_run: vi.fn() }));
vi.mock("../engine/events.js", () => ({ onEvent: vi.fn() }));

import { fallback, loadFallback, saveOrder } from "./store.svelte.js";

const account = (id, kind, signedIn = true) => ({ id, kind, name: id || kind, label: id || kind, signed_in: signedIn });

const settings = (accounts) => ({ accounts, order: {} });

describe("fallback accounts", () => {
  beforeEach(() => {
    calls.fallback.mockReset();
    calls.refreshLimits.mockReset();
  });

  it("refresh a tool's usage when an account is added or signed in", async () => {
    calls.fallback.mockResolvedValue(settings([account("", "claude-code")]));
    await loadFallback();
    expect(calls.refreshLimits).toHaveBeenCalledWith("claude-code");

    calls.refreshLimits.mockClear();
    calls.fallback.mockResolvedValue(settings([account("", "claude-code"), account("c2", "claude-code", false)]));
    await loadFallback();
    expect(calls.refreshLimits).not.toHaveBeenCalled();

    calls.fallback.mockResolvedValue(settings([account("", "claude-code"), account("c2", "claude-code", true)]));
    await loadFallback();
    expect(calls.refreshLimits).toHaveBeenCalledWith("claude-code");

    calls.refreshLimits.mockClear();
    await loadFallback();
    expect(calls.refreshLimits).not.toHaveBeenCalled();
  });
});

describe("saving the fallback order", () => {
  it("an older save's answer never replaces a newer edit", async () => {
    const replies = [];
    calls.setFallbackOrder.mockImplementation((order) => new Promise((resolve) => replies.push(() => resolve({ accounts: [], order }))));
    const one = { on: true, blocks: [{ type: "a" }], lists: [] };
    const two = { on: true, blocks: [{ type: "a" }, { type: "b" }], lists: [] };
    saveOrder(one);
    const done = saveOrder(two);
    await vi.waitFor(() => expect(replies).toHaveLength(1));
    replies[0]();
    await vi.waitFor(() => expect(replies).toHaveLength(2));
    // The first save's answer must not put the one-block order back.
    expect(fallback.order.blocks).toHaveLength(2);
    replies[1]();
    await done;
    expect(fallback.order.blocks).toHaveLength(2);
  });
});
