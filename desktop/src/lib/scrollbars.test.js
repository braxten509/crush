// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { startScrollbars } from "./scrollbars.js";

describe("app scrollbars", () => {
  afterEach(() => vi.useRealTimers());

  it("shows an overflowing scroller while scrolling and hides it two seconds later", () => {
    vi.useFakeTimers();
    const root = document.createElement("main");
    const scroller = document.createElement("div");
    Object.defineProperties(scroller, {
      scrollHeight: { value: 500 },
      clientHeight: { value: 100 },
    });
    root.append(scroller);
    document.body.append(root);

    const stop = startScrollbars(document);
    scroller.dispatchEvent(new Event("scroll"));
    expect(scroller.classList.contains("app-scrollbar-active")).toBe(true);

    vi.advanceTimersByTime(1500);
    scroller.dispatchEvent(new Event("scroll"));
    vi.advanceTimersByTime(1500);
    expect(scroller.classList.contains("app-scrollbar-active")).toBe(true);
    vi.advanceTimersByTime(500);
    expect(scroller.classList.contains("app-scrollbar-active")).toBe(false);

    stop();
    root.remove();
  });

  it("does not reveal a scrollbar when content fits", () => {
    const scroller = document.createElement("div");
    Object.defineProperties(scroller, {
      scrollHeight: { value: 100 },
      clientHeight: { value: 100 },
    });
    document.body.append(scroller);

    const stop = startScrollbars(document);
    scroller.dispatchEvent(new Event("scroll"));
    expect(scroller.classList.contains("app-scrollbar-active")).toBe(false);

    stop();
    scroller.remove();
  });
});
