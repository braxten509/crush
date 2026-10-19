// CHAT-033: the cases of internal/ui/common/common_test.go:32-90, plus the
// offset map back to the stored text.
import { describe, expect, test } from "vitest";
import {
  PLAN_READY_MARKER as READY,
  PLAN_START_MARKER as START,
  planReadyPresent,
  planStartPresent,
  stripPlanMarkers,
  stripPlanMarkersMapped,
  stripPlanReadyMarker,
} from "./plan.js";

describe("plan markers (common_test.go)", () => {
  test("PlanReadyMarkerPresent", () => {
    expect(planReadyPresent("plan\n" + READY)).toBe(true);
    expect(planReadyPresent("plan\n  " + READY + "  \ntrailing note")).toBe(true);
    expect(planReadyPresent("plan\n`" + READY + "`")).toBe(true);
    expect(planReadyPresent("plan without marker")).toBe(false);
    expect(planReadyPresent("I will end with " + READY + " when done.")).toBe(false);
  });

  test("StripPlanReadyMarker", () => {
    expect(stripPlanReadyMarker("plan\n" + READY)).toBe("plan");
    expect(stripPlanReadyMarker("plan\n  " + READY + "  \nnote")).toBe("plan\nnote");
    const prose = "I will end with " + READY + " when done.";
    expect(stripPlanReadyMarker(prose)).toBe(prose);
    expect(stripPlanReadyMarker("no marker here")).toBe("no marker here");
    expect(stripPlanReadyMarker("plan\n`" + READY + "`")).toBe("plan");
    // A fence that only wraps the marker goes with it.
    expect(stripPlanReadyMarker("plan\n```\n" + READY + "\n```")).toBe("plan");
    // A real code block just above the marker keeps its fences.
    expect(stripPlanReadyMarker("plan\n```go\nfmt.Println(1)\n```\n" + READY)).toBe("plan\n```go\nfmt.Println(1)\n```");
  });

  test("PlanStartMarkerPresent", () => {
    expect(planStartPresent(START + "\nplan")).toBe(true);
    expect(planStartPresent("note\n  " + START + "  \nplan")).toBe(true);
    expect(planStartPresent("`" + START + "`\nplan")).toBe(true);
    expect(planStartPresent("plan without marker")).toBe(false);
    expect(planStartPresent("It begins with " + START + " on its own line.")).toBe(false);
    expect(planStartPresent("plan\n" + READY)).toBe(false);
  });

  test("StripPlanMarkers", () => {
    expect(stripPlanMarkers(START + "\nplan\n" + READY)).toBe("plan");
    expect(stripPlanMarkers(START + "\nhalf a plan")).toBe("half a plan");
    const prose = "The plan begins with " + START + ".";
    expect(stripPlanMarkers(prose)).toBe(prose);
  });
});

describe("the offset map", () => {
  // Every kept character maps to the same character of the input.
  function expectFaithful(input) {
    const { text, toSource } = stripPlanMarkersMapped(input);
    for (let i = 0; i < text.length; i++) {
      if (text[i] === "\n") continue; // a joining newline may stand for a removed line's
      expect(input[toSource(i)], `char ${i} of ${JSON.stringify(text)}`).toBe(text[i]);
    }
    return { text, toSource };
  }

  test("no marker: the identity", () => {
    const { text, toSource } = stripPlanMarkersMapped("just text\nmore");
    expect(text).toBe("just text\nmore");
    expect(toSource(5)).toBe(5);
  });

  test("start and ready markers", () => {
    const input = START + "\n# Plan\n- one\n" + READY;
    const { text, toSource } = expectFaithful(input);
    expect(text).toBe("# Plan\n- one");
    expect(toSource(0)).toBe(START.length + 1);
    expect(toSource(text.length)).toBe(input.indexOf("- one") + "- one".length);
  });

  test("a fenced marker and a real fence above it", () => {
    const input = "plan\n```go\nx := 1\n```\n```\n" + READY + "\n```\nafter";
    const { text, toSource } = expectFaithful(input);
    expect(text).toBe("plan\n```go\nx := 1\n```\nafter");
    expect(toSource(text.indexOf("after"))).toBe(input.indexOf("after"));
  });

  test("a stored range sliced through the map holds the shown text", () => {
    const input = "intro\n" + START + "\nstep one\nstep two\n" + READY;
    const { text, toSource } = expectFaithful(input);
    const at = text.indexOf("step two");
    expect(input.slice(toSource(at), toSource(at + "step two".length))).toBe("step two");
  });
});
