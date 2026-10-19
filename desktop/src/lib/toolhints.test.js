import { beforeEach, describe, expect, it, vi } from "vitest";

let th;

beforeEach(async () => {
  vi.resetModules();
  th = await import("./toolhints.js");
});

describe("tool hints (desktop-design-5-6 6.3, LIVE-004)", () => {
  it("gives the first answer that is not null, in registration order", () => {
    const item = { key: "g1", working: true };
    expect(th.toolHint(item)).toBe(null);
    th.registerToolHint(() => null);
    th.registerToolHint((it) => (it.working ? "Still running after 60 s" : null));
    th.registerToolHint(() => "later");
    expect(th.toolHint(item)).toBe("Still running after 60 s");
    expect(th.toolHint({ key: "g2", working: false })).toBe("later");
  });

  it("unregisters", () => {
    const off = th.registerToolHint(() => "hint");
    off();
    expect(th.toolHint({ key: "g1" })).toBe(null);
  });
});
