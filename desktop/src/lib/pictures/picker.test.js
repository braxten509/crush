import { describe, expect, it } from "vitest";
import { choosable, parentOf, shownEntries } from "./picker.js";

// The Add Image picker's rules (desktop-design-5-6 2.10, DLG-256).
describe("picker rules", () => {
  it("lets only jpg, jpeg and png files be chosen, in any case", () => {
    expect(choosable({ name: "A.JPG", is_dir: false })).toBe(true);
    expect(choosable({ name: "b.Png", is_dir: false })).toBe(true);
    expect(choosable({ name: "c.gif", is_dir: false })).toBe(false);
    expect(choosable({ name: "pics.png", is_dir: true })).toBe(false);
  });

  it("lists folders first, then files, each by name, without hidden ones", () => {
    const rows = shownEntries([
      { name: "b.png", is_dir: false },
      { name: ".secret", is_dir: true },
      { name: "z", is_dir: true },
      { name: "a.png", is_dir: false },
      { name: "m", is_dir: true },
    ]);
    expect(rows.map((r) => r.name)).toEqual(["m", "z", "a.png", "b.png"]);
  });

  it("goes up one folder, never past the root", () => {
    expect(parentOf("/home/me/pics/")).toBe("/home/me");
    expect(parentOf("/home")).toBe("/");
    expect(parentOf("/")).toBe("/");
  });
});
