// CHAT-021 (the elapsed time after a live status, activity_timer.go:8-37),
// CHAT-035's duration (Go's time.Duration.String for whole seconds) and the
// byline's clock time.
import { describe, expect, test } from "vitest";
import { activityStart, clockTime, elapsedSince, elapsedText, goDuration } from "./timefmt.js";

describe("elapsed time after a status (CHAT-021)", () => {
  test.each([
    [0, ""],
    [9, ""],
    [9.99, ""],
    [10, "10s"],
    [59, "59s"],
    [61, "1m 1s"],
    [3599, "59m 59s"],
    [3600, "1h 0m"],
    [3661, "1h 1m"],
    [-5, ""],
  ])("%s s gives %j", (seconds, text) => {
    expect(elapsedText(seconds)).toBe(text);
  });

  test("measured from when the window saw the activity", () => {
    expect(elapsedSince(1_000, 13_500)).toBe("12s");
    expect(elapsedSince(0, 13_500)).toBe("");
  });

  test("a heartbeat keeps the timer; a new activity restarts it", () => {
    const timers = new Map();
    expect(activityStart(timers, "status:m1", "Thinking", 1_000)).toBe(1_000);
    // The same activity later (a provider heartbeat, a new chunk) keeps the start.
    expect(activityStart(timers, "status:m1", "Thinking", 30_000)).toBe(1_000);
    expect(elapsedSince(activityStart(timers, "status:m1", "Thinking", 61_000), 61_000)).toBe("1m 0s");
    // Another activity starts again.
    expect(activityStart(timers, "status:m1", "Responding", 62_000)).toBe(62_000);
    expect(elapsedSince(activityStart(timers, "status:m1", "Responding", 70_000), 70_000)).toBe("");
    // Each status has its own timer.
    expect(activityStart(timers, "status:m2", "Thinking", 63_000)).toBe(63_000);
  });
});

describe("footer duration (Go's Duration.String)", () => {
  test.each([
    [0, "0s"],
    [12, "12s"],
    [60, "1m0s"],
    [62, "1m2s"],
    [3600, "1h0m0s"],
    [3661, "1h1m1s"],
    [-5, "-5s"],
  ])("%s s gives %j", (seconds, text) => {
    expect(goDuration(seconds)).toBe(text);
  });
});

test("the byline's clock time is the local hour and minute", () => {
  const at = new Date(2026, 9, 18, 9, 5, 30).getTime() / 1000;
  expect(clockTime(at)).toBe("09:05");
  expect(clockTime(0)).toBe("");
});
