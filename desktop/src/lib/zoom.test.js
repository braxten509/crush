import { describe, expect, it } from "vitest";
import { ZOOM_DEFAULT, ZOOM_MAX, ZOOM_MIN, clampZoom, initialZoom, parseZoom, zoomForKey } from "./zoom.js";

const storage = (value) => ({ getItem: () => value });
const key = (k, extra = {}) => ({ key: k, ctrlKey: true, metaKey: false, altKey: false, ...extra });

describe("zoom", () => {
  it("reads a scale or twelfths", () => {
    expect(parseZoom("1")).toBe(12);
    expect(parseZoom("0.75")).toBe(9);
    expect(parseZoom("8/12")).toBe(8);
    expect(parseZoom("")).toBeNull();
    expect(parseZoom("big")).toBeNull();
    expect(parseZoom("-1")).toBeNull();
  });

  it("keeps the zoom inside its range", () => {
    expect(clampZoom(1)).toBe(ZOOM_MIN);
    expect(clampZoom(40)).toBe(ZOOM_MAX);
  });

  it("starts from the address, then the remembered choice, then Compact", () => {
    expect(initialZoom("?zoom=1", storage("9/12"))).toEqual({ twelfths: 12, remember: false });
    expect(initialZoom("", storage("9/12"))).toEqual({ twelfths: 9, remember: true });
    expect(initialZoom("", storage(null))).toEqual({ twelfths: ZOOM_DEFAULT, remember: true });
    const broken = { getItem: () => { throw new Error("no storage"); } };
    expect(initialZoom("", broken)).toEqual({ twelfths: ZOOM_DEFAULT, remember: true });
  });

  it("steps by one twelfth on Ctrl + and Ctrl -, and resets on Ctrl 0", () => {
    expect(zoomForKey(key("="), 8)).toBe(9);
    expect(zoomForKey(key("+"), 8)).toBe(9);
    expect(zoomForKey(key("-"), 8)).toBe(7);
    expect(zoomForKey(key("-"), ZOOM_MIN)).toBe(ZOOM_MIN);
    expect(zoomForKey(key("0"), 11)).toBe(ZOOM_DEFAULT);
    expect(zoomForKey(key("a"), 8)).toBeNull();
    expect(zoomForKey(key("=", { ctrlKey: false }), 8)).toBeNull();
    expect(zoomForKey(key("=", { altKey: true }), 8)).toBeNull();
  });
});
