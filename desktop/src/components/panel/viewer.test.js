import { describe, expect, it } from "vitest";
import { base64Bytes, dataBytes, pictureName, pictureSize, viewerSource } from "./viewer.js";

// 4.5: the picture viewer's bar words and the pictures it may load.

describe("viewerSource", () => {
  it("reads a project file by path, shows data: and blob: pictures, and refuses the rest", () => {
    expect(viewerSource({ path: "shots/a.png", name: "a.png" })).toEqual({ kind: "file", path: "shots/a.png" });
    expect(viewerSource({ src: "data:image/png;base64,AA" })).toEqual({ kind: "url", src: "data:image/png;base64,AA" });
    expect(viewerSource({ src: "blob:tauri://localhost/1" })).toEqual({ kind: "url", src: "blob:tauri://localhost/1" });
    expect(viewerSource({ src: "https://example.com/a.png" })).toBeNull();
    expect(viewerSource({ src: "javascript:alert(1)" })).toBeNull();
    expect(viewerSource({})).toBeNull();
    expect(viewerSource(null)).toBeNull();
  });
});

describe("sizes", () => {
  it("counts decoded bytes", () => {
    expect(base64Bytes("AAAA")).toBe(3);
    expect(base64Bytes("AAA=")).toBe(2);
    expect(base64Bytes("AA==")).toBe(1);
    expect(base64Bytes("")).toBe(0);
    expect(dataBytes("data:image/png;base64,AAAAAAAA")).toBe(6);
    expect(dataBytes("data:image/svg+xml,%3Csvg%3E")).toBe(5);
    expect(dataBytes("blob:x")).toBeNull();
  });

  it("writes the bar's size words", () => {
    expect(pictureSize(1280, 720, 214 * 1024)).toBe("1280 × 720 · 214 KB");
    expect(pictureSize(16, 16, 900)).toBe("16 × 16 · 900 B");
    expect(pictureSize(4000, 3000, 3.4 * 1024 * 1024)).toBe("4000 × 3000 · 3.4 MB");
    expect(pictureSize(1280, 720, null)).toBe("1280 × 720");
    expect(pictureSize(0, 0, 10)).toBe("10 B");
  });

  it("names the picture by its name, else the path's last part", () => {
    expect(pictureName({ path: "a/b/shot.png" })).toBe("shot.png");
    expect(pictureName({ path: "a/b/shot.png", name: "Screen" })).toBe("Screen");
    expect(pictureName({ src: "data:," })).toBe("");
  });
});
