import { describe, expect, it } from "vitest";
import { snapshot } from "./probe.js";

function fakeDocument({ shell, heading, title, fonts, zoom }) {
  return {
    title,
    fonts: fonts === undefined ? undefined : { status: fonts },
    documentElement: { style: { getPropertyValue: (name) => (name === "--zoom" && zoom != null ? zoom : "") } },
    querySelector(sel) {
      if (sel === '[data-probe="shell"]') {
        return shell ? { getBoundingClientRect: () => ({ x: 0, y: 0, width: 800, height: 600 }) } : null;
      }
      if (sel === "h1") return heading == null ? null : { textContent: heading };
      return null;
    },
  };
}

describe("probe snapshot", () => {
  it("reports the shell once it is on screen", () => {
    const s = snapshot(fakeDocument({ shell: true, heading: "Crush", title: "Crush" }));
    expect(s.ready).toBe(true);
    expect(s.heading).toBe("Crush");
    expect(s.shell).toEqual({ x: 0, y: 0, w: 800, h: 600 });
  });

  it("reports whether the fonts have loaded and the zoom as a scale", () => {
    const s = snapshot(fakeDocument({ shell: true, heading: "Crush", title: "Crush", fonts: "loaded", zoom: "0.6666666666666666" }));
    expect(s.fonts).toBe("loaded");
    expect(s.zoom).toBeCloseTo(2 / 3);
  });

  it("reports fonts still loading and no zoom before the zoom is applied", () => {
    const s = snapshot(fakeDocument({ shell: true, heading: "Crush", title: "Crush", fonts: "loading", zoom: null }));
    expect(s.fonts).toBe("loading");
    expect(s.zoom).toBe(null);
  });

  it("reports not ready before the shell exists", () => {
    const s = snapshot(fakeDocument({ shell: false, heading: null, title: "" }));
    expect(s.ready).toBe(false);
    expect(s.shell).toBe(null);
  });
});
