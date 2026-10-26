// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { INPUT_MS, SHOW_MS, startBarFade, userActed } from "./barfade.js";

// A scroll area 100 wide with a 6 wide bar (jsdom has no layout, so the
// sizes are set by hand).
function area() {
  const el = document.createElement("div");
  Object.defineProperty(el, "clientWidth", { value: 94 });
  Object.defineProperty(el, "offsetWidth", { value: 100 });
  Object.defineProperty(el, "clientHeight", { value: 200 });
  Object.defineProperty(el, "offsetHeight", { value: 200 });
  document.body.append(el);
  return el;
}

function press(el, offsetX) {
  const event = new MouseEvent("mousedown", { bubbles: true, button: 0 });
  Object.defineProperty(event, "offsetX", { value: offsetX });
  Object.defineProperty(event, "offsetY", { value: 10 });
  el.dispatchEvent(event);
}

describe("every scroll bar hides until used (barfade.js)", () => {
  let stop;
  beforeEach(() => {
    vi.useFakeTimers();
    stop = startBarFade(document);
  });
  afterEach(() => {
    stop();
    vi.useRealTimers();
    document.body.replaceChildren();
  });

  it("shows a bar while the user scrolls its area and hides it 2 s after", () => {
    const el = area();
    expect(el.classList.contains("bar-used")).toBe(false);
    // The app moving it (a reply following the bottom) does not count.
    el.dispatchEvent(new Event("scroll"));
    expect(el.classList.contains("bar-used")).toBe(false);
    el.dispatchEvent(new WheelEvent("wheel", { bubbles: true }));
    expect(userActed()).toBe(true);
    el.dispatchEvent(new Event("scroll"));
    expect(el.classList.contains("bar-used")).toBe(true);
    vi.advanceTimersByTime(SHOW_MS - 10);
    expect(userActed()).toBe(false);
    el.dispatchEvent(new KeyboardEvent("keydown", { bubbles: true, key: "PageDown" }));
    vi.advanceTimersByTime(INPUT_MS - 10);
    el.dispatchEvent(new Event("scroll"));
    vi.advanceTimersByTime(SHOW_MS - 10);
    expect(el.classList.contains("bar-used")).toBe(true);
    vi.advanceTimersByTime(20);
    expect(el.classList.contains("bar-used")).toBe(false);
  });

  it("leaves the areas that fade their own bar alone (the chat, the sheets)", () => {
    const el = area();
    el.classList.add("kit-scroll");
    el.dispatchEvent(new WheelEvent("wheel", { bubbles: true }));
    el.dispatchEvent(new Event("scroll"));
    expect(el.classList.contains("bar-used")).toBe(false);
  });

  it("keeps a pressed bar shown until the button is let go, then 2 s more", () => {
    const el = area();
    press(el, 50); // on the content, not the bar
    expect(el.classList.contains("bar-used")).toBe(false);
    press(el, 97); // on the bar's column
    vi.advanceTimersByTime(SHOW_MS * 3);
    expect(el.classList.contains("bar-used")).toBe(true);
    window.dispatchEvent(new MouseEvent("mouseup"));
    vi.advanceTimersByTime(SHOW_MS + 10);
    expect(el.classList.contains("bar-used")).toBe(false);
  });
});
