import { beforeEach, describe, expect, it, vi } from "vitest";

let ls;

beforeEach(async () => {
  vi.resetModules();
  ls = await import("./listsections.js");
});

describe("left list sections (desktop-design-5-6 2.20, 6.4)", () => {
  it("lists one place's visible sections by order", () => {
    let shown = true;
    ls.registerListSection({ id: "servers", order: 20, place: "footer", component: "S" });
    ls.registerListSection({ id: "terminals", order: 20, place: "rows", component: "T" });
    ls.registerListSection({ id: "first", order: 10, place: "rows", visible: () => shown, component: "F" });
    expect(ls.listSections("rows").map((s) => s.id)).toEqual(["first", "terminals"]);
    expect(ls.listSections("footer").map((s) => s.id)).toEqual(["servers"]);
    expect(ls.listSections("header")).toEqual([]);
    shown = false;
    expect(ls.listSections("rows").map((s) => s.id)).toEqual(["terminals"]);
  });

  it("refuses an unknown place, and unregisters", () => {
    expect(() => ls.registerListSection({ id: "x", order: 1, place: "middle", component: "X" })).toThrow(/unknown place "middle"/);
    const off = ls.registerListSection({ id: "h", order: 1, place: "header", component: "H" });
    off();
    expect(ls.listSections("header")).toEqual([]);
  });
});
