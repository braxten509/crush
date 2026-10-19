import { describe, expect, it } from "vitest";
import { matchFolders, newPicker, typedPath } from "./picker.js";

// A folder tree as dir_list answers it (is_dir follows links): {folder:
// entries}; a folder not here is unreadable.
const TREE = {
  "/home": [{ name: "u", is_dir: true }],
  "/home/u": [
    { name: ".config", is_dir: true },
    { name: "Code", is_dir: true },
    { name: "code-notes.txt", is_dir: false },
    { name: "linked", is_dir: true, is_symlink: true },
    { name: "broken", is_dir: false, is_symlink: true },
    { name: "Music", is_dir: true },
  ],
  "/home/u/many": ["a6", "a1", "a5", "a3", "A2", "a4", "b1"].map((name) => ({ name, is_dir: true })),
};

function listDir(path) {
  const entries = TREE[path.replace(/(.)\/+$/u, "$1")];
  if (!entries) return Promise.reject({ code: "not_a_folder" });
  return Promise.resolve({ path, entries });
}

const match = (typed, base = "/home/u/many") => matchFolders(typedPath(newPickerWith(typed, base)), listDir);

function newPickerWith(typed, base) {
  const p = newPicker({ dir: base, home: "/home/u" });
  p.input = typed;
  return p;
}

describe("picker_match (u:lst.picker_match; desktop-design-5-6 2.21, LIST-039; picker.go:67-118)", () => {
  it("lists the folders whose names start with the last part, ignoring case, by name, at most 5", async () => {
    expect(await match("/home/u/many/a")).toEqual(["/home/u/many/A2", "/home/u/many/a1", "/home/u/many/a3", "/home/u/many/a4", "/home/u/many/a5"]);
    expect(await match("/home/u/c")).toEqual(["/home/u/Code"]);
    expect(await match("/home/u/CO")).toEqual(["/home/u/Code"]);
  });

  it("lists a folder's inside when the path ends in a slash; hidden folders only once a dot is typed", async () => {
    expect(await match("/home/u/")).toEqual(["/home/u/Code", "/home/u/Music", "/home/u/linked"]);
    expect(await match("/home/u/.")).toEqual(["/home/u/.config"]);
  });

  it("counts links to folders and never files", async () => {
    expect(await match("/home/u/l")).toEqual(["/home/u/linked"]);
    expect(await match("/home/u/b")).toEqual([]);
    expect(await match("/home/u/code-")).toEqual([]);
  });

  it("reads ~ as home and a relative path from the starting folder", async () => {
    expect(await match("~/M")).toEqual(["/home/u/Music"]);
    expect(await match("~")).toEqual(["/home/u"]);
    expect(await match("b")).toEqual(["/home/u/many/b1"]);
  });

  it("gives no matches for an empty path or an unreadable folder", async () => {
    expect(await match("")).toEqual([]);
    expect(await match("/nowhere/x")).toEqual([]);
  });

  it("opens on the starting folder with home as ~, a slash at the end, and each recent folder once, at most 5", () => {
    const p = newPicker({ dir: "/home/u/proj", home: "/home/u", recent: ["/a", "/b", "/a", "/c", "/d", "/e", "/f"] });
    expect([p.input, p.cursor, p.sel, p.err]).toEqual(["~/proj/", 7, -1, ""]);
    expect(p.recent).toEqual(["/a", "/b", "/c", "/d", "/e"]);
  });
});
