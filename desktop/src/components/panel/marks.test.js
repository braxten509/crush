import { describe, expect, it } from "vitest";
import { drawLines, PEN, penWidth } from "./marks.js";

// The viewer's marks: red lines and white notes with a black shadow
// (owner, 2026-11-08).

function fakeContext(width, height) {
  const calls = [];
  let state = {};
  const stack = [];
  const context = {
    canvas: { width, height },
    save: () => stack.push({ ...state }),
    restore: () => (state = stack.pop()),
    beginPath() {},
    moveTo() {},
    lineTo() {},
    arc() {},
    stroke: () => calls.push(["stroke", state.strokeStyle]),
    fill: () => calls.push(["fill", state.fillStyle]),
    fillText: (text, x, y) => calls.push(["text", text, x, y, state.fillStyle, state.shadowColor, state.shadowBlur]),
  };
  return {
    calls,
    context: new Proxy(context, {
      get: (target, key) => (key in target ? target[key] : state[key]),
      set: (_, key, value) => ((state[key] = value), true),
    }),
  };
}

describe("drawLines", () => {
  it("draws lines in red and notes in white with a black shadow, in order", () => {
    const { calls, context } = fakeContext(1080, 2400);
    const marks = [[{ x: 1, y: 1 }, { x: 5, y: 5 }], { text: "Tap here", x: 40, y: 90 }, [{ x: 9, y: 9 }]];
    drawLines(context, marks, penWidth(1080, 2400));
    const texts = calls.filter((c) => c[0] === "text");
    expect(texts.length).toBeGreaterThan(0);
    for (const [, text, x, y, fill, shadow, blur] of texts) {
      expect([text, x, y, fill]).toEqual(["Tap here", 40, 90, "#ffffff"]);
      expect(shadow).toMatch(/^rgba\(0, 0, 0/u);
      expect(blur).toBeGreaterThan(0);
    }
    expect(calls.find((c) => c[0] === "stroke")).toEqual(["stroke", PEN]);
    // The dot after the note is still red: the note's white does not leak.
    expect(calls.at(-1)).toEqual(["fill", PEN]);
  });
});
