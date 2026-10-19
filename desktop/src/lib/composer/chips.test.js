import { describe, expect, it } from "vitest";
import { attachmentChip, deleteModeKey, fitChips, moreText, skillChip } from "./chips.js";

describe("chip views (TYPE-035, attachments.go:266-274)", () => {
  it("picks the icon from the type: picture, skill (Markdown), else text", () => {
    expect(attachmentChip({ kind: "picture", name: "paste_1.png", mime: "image/png", base64: "iVBO" })).toEqual({
      kind: "picture",
      name: "paste_1.png",
      mime: "image/png",
      viewable: true,
      src: "data:image/png;base64,iVBO",
    });
    expect(attachmentChip({ kind: "text", name: "demo", mime: "text/markdown", base64: "eA==" }).kind).toBe("skill");
    expect(attachmentChip({ kind: "file", name: "a.pdf", mime: "application/pdf", base64: "eA==" }).kind).toBe("text");
    expect(skillChip({ id: "s1", name: "demo" })).toEqual({ kind: "skill", name: "demo", mime: "", viewable: false });
  });
});

describe("fitChips (TYPE-035, attachments.go:160-232)", () => {
  const moreWidth = (n) => 60 + String(n).length * 8;
  const base = { moreWidth, gap: 8, fixed: 70, minName: 40 };

  it("shows every chip that fits", () => {
    expect(fitChips({ ...base, widths: [100, 100], available: 300 })).toEqual({ shown: 2, more: 0, firstName: null });
  });

  it("ends with N more… instead of a second line", () => {
    // 8 chips of 120 in 520: three fit with room for "5 more…" (a fourth
    // would end at 504 + 76 for its hint).
    expect(fitChips({ ...base, widths: Array(8).fill(120), available: 520 })).toEqual({ shown: 3, more: 5, firstName: null });
    expect(fitChips({ ...base, widths: Array(8).fill(120), available: 600 })).toEqual({ shown: 4, more: 4, firstName: null });
    expect(moreText(5)).toBe("5 more…");
  });

  it("trims the first name, to at least 5 characters, when even one chip does not fit", () => {
    expect(fitChips({ ...base, widths: [300, 100], available: 200 })).toEqual({ shown: 1, more: 1, firstName: 200 - 70 - 8 - 68 });
    expect(fitChips({ ...base, widths: [300, 100], available: 150 })).toEqual({ shown: 1, more: 0, firstName: 80 });
    expect(fitChips({ ...base, widths: [300], available: 150 })).toEqual({ shown: 1, more: 0, firstName: 80 });
  });
});

describe("delete mode keys (TYPE-038, attachments.go:60-90)", () => {
  it("enters with Ctrl+R only when there are chips, but always takes it", () => {
    expect(deleteModeKey("Ctrl+R", { deleting: false, count: 2 })).toEqual({ enter: true });
    expect(deleteModeKey("Ctrl+R", { deleting: false, count: 0 })).toEqual({ eat: true });
  });

  it("removes by digit, clears with r, leaves with Esc, eats the rest", () => {
    const on = { deleting: true, count: 3 };
    expect(deleteModeKey("1", on)).toEqual({ remove: 1 });
    expect(deleteModeKey("r", on)).toEqual({ clear: true });
    expect(deleteModeKey("Escape", on)).toEqual({ leave: true });
    expect(deleteModeKey("x", on)).toEqual({ eat: true });
    expect(deleteModeKey("Enter", on)).toEqual({ eat: true });
    expect(deleteModeKey("x", { deleting: false, count: 3 })).toBeNull();
  });
});
