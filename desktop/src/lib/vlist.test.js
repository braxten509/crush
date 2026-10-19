// The virtual list's arithmetic (desktop-design 1.6 items 2 and 8): what is
// mounted for a long selection (CHAT-087), the anchor that keeps the view
// still (CHAT-092, CHAT-099), the rhythm between items and the estimates;
// and the list itself in the page model: its rows outlive a size change.
// @vitest-environment jsdom
import { flushSync, mount, unmount } from "svelte";
import { describe, expect, test, vi } from "vitest";
import VirtualList from "../components/timeline/VirtualList.svelte";
// This file runs in the node project (vite.config.js is frozen); the page
// model needs Svelte's browser build to mount.
vi.mock("svelte", () => import("../../node_modules/svelte/src/index-client.js"));

import {
  anchorAt,
  atBottom,
  createHeights,
  estimate,
  gapBefore,
  inView,
  itemBottom,
  itemTop,
  layout,
  maxTop,
  mountedIndices,
  scrollToShow,
  segments,
  topForAnchor,
  visibleRange,
  windowRange,
} from "./vlist.js";

const reply = (key, text = "x".repeat(140), extra = {}) => ({ kind: "reply", key, messageId: key, version: 1, text, ...extra });
const chat = (n) => Array.from({ length: n }, (_, i) => (i % 2 === 0 ? { kind: "user", key: `u${i}`, messageId: `u${i}`, version: 1, chips: [] } : reply(`r${i}`)));
const lay = (items, pad = { top: 24, bottom: 24 }) =>
  layout(
    items,
    (i) => estimate(items[i]),
    (i) => gapBefore(items[i - 1], items[i]),
    pad,
  );

describe("a selection mounts its two ends, never the chat between (CHAT-087)", () => {
  test("item 1 to item 900 of a 1,000-item chat keeps at most the window plus 2", () => {
    const items = chat(1000);
    const l = lay(items);
    const viewport = 900;
    const top = maxTop(l, viewport); // the view at the bottom, as a chat opens
    const range = windowRange(l, top, viewport);
    const windowSize = range.last - range.first + 1;
    const mounted = mountedIndices(range, [0, 899]);
    expect(mounted.length).toBeLessThanOrEqual(windowSize + 2);
    expect(mounted).toContain(0);
    expect(mounted).toContain(899);
    // Far fewer than the chat: the items between the ends stay out.
    expect(mounted.length).toBeLessThan(40);
    for (let i = 1; i < 899; i++) if (i < range.first) expect(mounted).not.toContain(i);
  });

  test("spacers and items fill the page exactly, in order", () => {
    const items = chat(1000);
    const l = lay(items);
    const mounted = mountedIndices(windowRange(l, 20000, 900), [3, 980]);
    const segs = segments(l, mounted);
    let y = l.pad.top;
    let last = -1;
    for (const s of segs) {
      if (s.spacer) y += s.height;
      else {
        expect(s.index).toBeGreaterThan(last);
        last = s.index;
        expect(y).toBeCloseTo(l.tops[s.index]);
        y += l.gaps[s.index] + l.heights[s.index];
      }
    }
    expect(y + l.pad.bottom).toBeCloseTo(l.total);
  });

  test("the window is the view and one view above and below", () => {
    const items = chat(1000);
    const l = lay(items);
    const top = 30000;
    const range = windowRange(l, top, 900);
    expect(itemBottom(l, range.first)).toBeGreaterThan(top - 900);
    expect(range.first === 0 || itemBottom(l, range.first - 1) <= top - 900).toBe(true);
    expect(itemTop(l, range.last)).toBeLessThan(top + 1800);
    expect(range.last === items.length - 1 || itemTop(l, range.last + 1) >= top + 1800).toBe(true);
  });
});

describe("the view stays still (CHAT-092, CHAT-099)", () => {
  test("an item above the view growing moves the scroll, not the view", () => {
    const items = chat(50);
    const keys = items.map((x) => x.key);
    const heights = items.map((x) => estimate(x));
    const build = () =>
      layout(
        items,
        (i) => heights[i],
        (i) => gapBefore(items[i - 1], items[i]),
        { top: 24, bottom: 24 },
      );
    let l = build();
    const top = 2000;
    const anchor = anchorAt(l, keys, top);
    const first = visibleRange(l, top, 600).first;
    const screenY = itemTop(l, first) - top;
    heights[2] += 120; // a reply above the view finishes streaming
    l = build();
    const next = topForAnchor(l, keys, anchor);
    expect(next).toBeCloseTo(top + 120);
    expect(itemTop(l, first) - next).toBeCloseTo(screenY);
  });

  test("an anchor whose item left gives no position", () => {
    const l = lay(chat(4));
    expect(topForAnchor(l, ["a", "b"], { key: "zz", offset: 3 })).toBeNull();
  });

  test("an item taller than the view counts as at the bottom once its end shows", () => {
    const items = [reply("r1", "x".repeat(7000))];
    const l = lay(items);
    expect(l.total).toBeGreaterThan(3000);
    expect(atBottom(maxTop(l, 600), 600, l.total)).toBe(true);
    expect(atBottom(maxTop(l, 600) - 2, 600, l.total)).toBe(false);
    // Scrolling to it shows its top; End shows its end.
    expect(scrollToShow(l, 0, maxTop(l, 600), 600)).toBe(itemTop(l, 0));
  });

  test("scrolling to an item below the view puts its end at the bottom", () => {
    const items = chat(40);
    const l = lay(items);
    const top = 0;
    const i = 20;
    expect(inView(l, i, top, 600)).toBe(false);
    const next = scrollToShow(l, i, top, 600);
    expect(itemBottom(l, i)).toBeCloseTo(next + 600);
    expect(inView(l, i, next, 600)).toBe(true);
    // Already in view: no scroll.
    expect(scrollToShow(l, i, next, 600)).toBe(next);
  });
});

describe("rhythm and estimates (the picture's Timeline CSS; 1.6 item 2)", () => {
  const u = { kind: "user", messageId: "u" };
  const r = { kind: "reply", messageId: "a" };
  const g = { kind: "tools", messageId: "a" };
  test.each([
    [null, r, 0],
    [u, r, 44],
    [r, g, 37],
    [u, g, 37],
    [g, r, 39],
    [g, g, 39],
    [r, u, 40],
    [r, { kind: "banner", messageId: "a" }, 16],
    [r, { kind: "footer", messageId: "a" }, 16],
    [r, { kind: "status", messageId: "a" }, 16],
    [r, { kind: "footer", messageId: "b" }, 40],
    [u, { kind: "status", messageId: "" }, 44],
  ])("%j then %j: %i", (prev, item, gap) => {
    expect(gapBefore(prev, item)).toBe(gap);
  });

  test("estimates by kind", () => {
    expect(estimate({ kind: "user", chips: [] })).toBe(80);
    expect(estimate({ kind: "reply", text: "x".repeat(70) })).toBe(83);
    expect(estimate({ kind: "reply", text: "x".repeat(71) })).toBe(116);
    expect(estimate({ kind: "tools" })).toBe(44);
  });

  test("measured heights win over estimates and follow the key across versions", () => {
    const h = createHeights();
    const item = reply("r1");
    expect(h.get(item, 500)).toBeNull();
    expect(h.set(item, 500, 210)).toBe(true);
    expect(h.set(item, 500, 210)).toBe(false);
    expect(h.exact(item, 500)).toBe(true);
    const grown = { ...item, version: 2 };
    expect(h.exact(grown, 500)).toBe(false);
    expect(h.get(grown, 500)).toBe(210);
    h.keep(new Set());
    expect(h.size).toBe(0);
  });
});

describe("VirtualList", () => {
  test("rows keep their text through the first load, a window resize and a column drag", () => {
    const observers = [];
    globalThis.ResizeObserver = class {
      constructor(fn) {
        this.fn = fn;
        observers.push(this);
      }
      observe() {}
      unobserve() {}
      disconnect() {}
    };
    // jsdom has no layout: the view is 600 px tall and the column 700 px wide.
    const size = { height: 600, width: 700 };
    const proto = HTMLElement.prototype;
    Object.defineProperty(proto, "clientHeight", { configurable: true, get: () => size.height });
    Object.defineProperty(proto, "clientWidth", { configurable: true, get: () => size.width });
    const items = ["first message", "second message", "third message"].map((text, i) => ({ kind: "notice", key: `n${i}`, version: 1, text }));
    const target = document.createElement("div");
    document.body.append(target);
    const list = mount(VirtualList, { target, props: { items } });
    flushSync();
    const scroller = target.querySelector(".scroller");
    const canvas = target.querySelector(".canvas");
    const resize = (node) => observers.forEach((o) => o.fn([{ target: node, contentRect: { height: 0 } }]));
    const text = () => canvas.textContent;
    try {
      expect(text()).toContain("first message");
      resize(scroller);
      expect(text()).toContain("second message");
      size.height = 400;
      resize(scroller);
      expect(text()).toContain("second message");
      size.width = 520;
      resize(canvas);
      expect(text()).toContain("third message");
    } finally {
      unmount(list);
      target.remove();
      delete globalThis.ResizeObserver;
      delete proto.clientHeight; // back to jsdom's own, on Element
      delete proto.clientWidth;
    }
  });
});
