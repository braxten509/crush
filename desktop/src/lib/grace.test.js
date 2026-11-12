import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { GRACE_MAX_MS, GRACE_QUIET_MS, REOPEN_MS, createGrace, noteClosed } from "./grace.js";

const key = (name = "a") => ({ type: "keydown", key: name });

describe("input grace (DLG-119, the terminal's OpenDialogWithGrace)", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-10-18T12:00:00Z"));
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("absorbs keys until none has come for the quiet time", () => {
    const grace = createGrace("quiet");
    vi.advanceTimersByTime(GRACE_QUIET_MS - 1);
    expect(grace.absorb(key())).toBe(true); // just inside the quiet time: still armed, and it restarts the quiet time
    vi.advanceTimersByTime(GRACE_QUIET_MS - 1);
    expect(grace.absorb(key())).toBe(true);
    vi.advanceTimersByTime(GRACE_QUIET_MS);
    expect(grace.absorb(key())).toBe(false); // a whole quiet time without a key: the prompt takes keys
    expect(grace.active()).toBe(false);
  });

  it("ends the quiet time after opening when no key comes at all", () => {
    const grace = createGrace("idle");
    vi.advanceTimersByTime(GRACE_QUIET_MS);
    expect(grace.absorb(key())).toBe(false);
  });

  it("restarts the quiet time with each absorbed key, but never past the longest grace", () => {
    const grace = createGrace("cap");
    const step = 100; // shorter than the quiet time, so only the cap ends it
    const absorbed = [];
    for (let at = step; at <= GRACE_MAX_MS + step; at += step) {
      vi.advanceTimersByTime(step);
      absorbed.push([at, grace.absorb(key())]);
    }
    // Keys every step keep it absorbing until the longest grace, then it stops.
    expect(absorbed.filter(([, taken]) => taken).map(([at]) => at)).toEqual(absorbed.map(([at]) => at).filter((at) => at < GRACE_MAX_MS));
    expect(absorbed.find(([at]) => at >= GRACE_MAX_MS)[1]).toBe(false);
  });

  it("does not count modifier keys", () => {
    const grace = createGrace("modifiers");
    const first = Math.floor(GRACE_QUIET_MS / 2);
    vi.advanceTimersByTime(first);
    expect(grace.absorb(key("Shift"))).toBe(false); // not absorbed, and the quiet time is not restarted
    vi.advanceTimersByTime(GRACE_QUIET_MS - first + 5);
    expect(grace.absorb(key("a"))).toBe(false); // just past the quiet time since opening
  });

  it("has no grace when the same prompt closed a moment ago", () => {
    const first = createGrace("permission");
    expect(first.absorb(key())).toBe(true);
    first.close();
    vi.advanceTimersByTime(REOPEN_MS - 1);
    const again = createGrace("permission");
    expect(again.absorb(key())).toBe(false);
    expect(again.active()).toBe(false);
    // Another prompt id still gets its grace.
    expect(createGrace("other").absorb(key())).toBe(true);
  });

  it("has a grace again once the reopen time has passed since the close", () => {
    noteClosed("later");
    vi.advanceTimersByTime(REOPEN_MS);
    expect(createGrace("later").absorb(key())).toBe(true);
  });

  it("ends when the prompt closes", () => {
    const grace = createGrace("closing");
    grace.close();
    expect(grace.absorb(key())).toBe(false);
  });
});
