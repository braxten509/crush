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

  it("uses the terminal's values", () => {
    expect([GRACE_QUIET_MS, GRACE_MAX_MS, REOPEN_MS]).toEqual([425, 1500, 500]);
  });

  it("absorbs keys until none has come for 425 ms", () => {
    const grace = createGrace("quiet");
    vi.advanceTimersByTime(424);
    expect(grace.absorb(key())).toBe(true); // 424 ms after opening: still quiet-armed, and it restarts the quiet time
    vi.advanceTimersByTime(424);
    expect(grace.absorb(key())).toBe(true);
    vi.advanceTimersByTime(425);
    expect(grace.absorb(key())).toBe(false); // 425 ms without a key: the prompt takes keys
    expect(grace.active()).toBe(false);
  });

  it("ends 425 ms after opening when no key comes at all", () => {
    const grace = createGrace("idle");
    vi.advanceTimersByTime(425);
    expect(grace.absorb(key())).toBe(false);
  });

  it("restarts the quiet time with each absorbed key, but never past 1.5 s", () => {
    const grace = createGrace("cap");
    const absorbed = [];
    for (let at = 100; at <= 1600; at += 100) {
      vi.advanceTimersByTime(100);
      absorbed.push([at, grace.absorb(key())]);
    }
    // Keys every 100 ms keep it absorbing until 1.5 s after opening, then it stops.
    expect(absorbed.filter(([, taken]) => taken).map(([at]) => at)).toEqual([100, 200, 300, 400, 500, 600, 700, 800, 900, 1000, 1100, 1200, 1300, 1400]);
    expect(absorbed.find(([at]) => at === 1500)[1]).toBe(false);
  });

  it("does not count modifier keys", () => {
    const grace = createGrace("modifiers");
    vi.advanceTimersByTime(300);
    expect(grace.absorb(key("Shift"))).toBe(false); // not absorbed, and the quiet time is not restarted
    vi.advanceTimersByTime(130);
    expect(grace.absorb(key("a"))).toBe(false); // 430 ms since opening
  });

  it("has no grace when the same prompt closed less than 500 ms ago", () => {
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

  it("has a grace again once 500 ms have passed since the close", () => {
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
