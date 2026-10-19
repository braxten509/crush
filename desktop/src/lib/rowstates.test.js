import { beforeEach, describe, expect, it, vi } from "vitest";

let rs;

beforeEach(async () => {
  vi.resetModules();
  rs = await import("./rowstates.js");
});

describe("row states (desktop-design-5-6 2.20, 6.4)", () => {
  it("gives the first non-null answer, in registration order", () => {
    expect(rs.rowState("c1")).toBe(null);
    rs.registerRowState((id) => (id === "c2" ? { icon: "dot", text: "Unread" } : null));
    rs.registerRowState(() => ({ icon: "check", text: "Ready" }));
    expect(rs.rowState("c2")).toEqual({ icon: "dot", text: "Unread" });
    expect(rs.rowState("c1")).toEqual({ icon: "check", text: "Ready" });
  });

  it("unregisters", () => {
    const off = rs.registerRowState(() => ({ icon: "check", text: "Ready" }));
    off();
    expect(rs.rowState("c1")).toBe(null);
  });
});
