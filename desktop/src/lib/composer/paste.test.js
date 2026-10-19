import { describe, expect, it } from "vitest";
import {
  A_FOLDER,
  FILE_TOO_BIG,
  MAX_ATTACHMENT_BYTES,
  NO_PICTURES,
  PASTE_TOO_BIG,
  PICTURE_TOO_BIG,
  attachPath,
  dropText,
  fileUriPaths,
  isImagePath,
  isPictureFile,
  normalize,
  overThreshold,
  pasteIndex,
  pasteRoute,
  pastedPictures,
  pictureAttachment,
  supportsImages,
  textAttachment,
  unixPaths,
} from "./paste.js";
import { detectContentType } from "./sniff.js";

const PNG = Uint8Array.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 13, 0x49, 0x48, 0x44, 0x52]);
const lines = (n) => Array.from({ length: n }, (_, i) => `line ${i + 1}`).join("\n");
const decode = (base64) => new TextDecoder().decode(Uint8Array.from(atob(base64), (c) => c.charCodeAt(0)));

describe("the size threshold (TYPE-011, paste.go:122-136)", () => {
  it("lets 10 lines and 1,000 columns through, not 11 or 1,001", () => {
    expect(overThreshold(lines(10))).toBe(false);
    expect(overThreshold(lines(11))).toBe(true);
    expect(overThreshold("x".repeat(1000))).toBe(false);
    expect(overThreshold("x".repeat(1001))).toBe(true);
  });

  it("counts columns in UTF-8 bytes, as Go's len does", () => {
    expect(overThreshold("é".repeat(500))).toBe(false);
    expect(overThreshold("é".repeat(501))).toBe(true);
    expect(overThreshold(`short\n${"€".repeat(334)}`)).toBe(true);
  });

  it("normalizes \\r\\n first", () => {
    expect(normalize("a\r\nb\r\n")).toBe("a\nb\n");
  });
});

describe("a long paste as an attachment (TYPE-011)", () => {
  it("is named paste_N.txt with the next number", () => {
    const first = textAttachment(lines(11), []);
    expect(first.attachment).toMatchObject({ name: "paste_1.txt", path: "paste_1.txt", kind: "text", mime: "text/plain; charset=utf-8" });
    expect(decode(first.attachment.base64)).toBe(lines(11));
    expect(pasteIndex([{ name: "paste_1.txt" }, { name: "notes.md" }, { name: "paste_3.txt" }])).toBe(4);
    expect(textAttachment("x", [{ name: "paste_2.txt" }]).attachment.name).toBe("paste_3.txt");
  });

  it("counts pasted pictures too, so names stay unique", () => {
    expect(pasteIndex([{ name: "paste_2.png" }])).toBe(3);
  });

  it("refuses over 5 MB", () => {
    expect(textAttachment("x".repeat(MAX_ATTACHMENT_BYTES), []).attachment).toBeTruthy();
    expect(textAttachment("x".repeat(MAX_ATTACHMENT_BYTES + 1), [])).toEqual({ warn: PASTE_TOO_BIG });
    expect(PASTE_TOO_BIG).toBe("Paste is too big (>5mb)");
  });

  it("names the type as Go's sniffer does", () => {
    expect(textAttachment("<html><body>hi</body></html>\n".repeat(12), []).attachment.mime).toBe("text/html; charset=utf-8");
  });
});

describe("pasted paths (TYPE-012, fsext/paste.go)", () => {
  it("reads shell-escaped paths", () => {
    expect(unixPaths("/tmp/a\\ b.png /tmp/c.png")).toEqual(["/tmp/a b.png", "/tmp/c.png"]);
    expect(unixPaths("ends\\")).toEqual(["ends\\"]);
    expect(unixPaths("   ")).toEqual([]);
  });

  it("routes at once: long → attach, picture paths → look up, else text", () => {
    expect(pasteRoute(lines(11))).toBe("attach");
    expect(pasteRoute("/tmp/a.png /tmp/b.JPG")).toBe("paths");
    expect(pasteRoute("/tmp/a.png\n/tmp/b.jpeg")).toBe("paths");
    expect(pasteRoute("/tmp/a.png /tmp/notes.txt")).toBe("text");
    expect(pasteRoute("hello world")).toBe("text");
    expect(isImagePath("x.PNG")).toBe(true);
    expect(isImagePath("x.gif")).toBe(false);
  });

  const files = {
    "/p/a.png": { exists: true, is_dir: false, size: 10 },
    "/p/b.png": { exists: true, is_dir: false, size: 10 },
    "/p/a b.png": { exists: true, is_dir: false, size: 10 },
    "/p/dir.png": { exists: true, is_dir: true, size: 0 },
    "/p/big.png": { exists: true, is_dir: false, size: MAX_ATTACHMENT_BYTES + 1 },
  };
  const stat = async (path) => files[path] ?? { exists: false, is_dir: false, size: 0 };

  it("attaches only when every path exists and is a picture", async () => {
    expect(await pastedPictures("/p/a.png /p/b.png", stat)).toEqual(["/p/a.png", "/p/b.png"]);
    expect(await pastedPictures("/p/a.png\n/p/b.png", stat)).toEqual(["/p/a.png", "/p/b.png"]);
    expect(await pastedPictures("/p/a b.png", stat)).toEqual(["/p/a b.png"]);
    expect(await pastedPictures("/p/a.png /p/missing.png", stat)).toBeNull();
  });

  it("treats a failed look as present, and reports it when attaching", async () => {
    const failing = async (path) => {
      if (path === "/p/locked.png") throw { code: "bad_path", message: "stat /p/locked.png: permission denied" };
      return stat(path);
    };
    expect(await pastedPictures("/p/locked.png", failing)).toEqual(["/p/locked.png"]);
    expect(await attachPath("/p/locked.png", { stat: failing })).toEqual({ error: "stat /p/locked.png: permission denied" });
  });

  it("gives each path its notice or its attachment", async () => {
    const read = async () => ({ mime: "image/png", base64: "iVBORw0KGgo=" });
    expect(await attachPath("/p/dir.png", { stat, read })).toEqual({ warn: A_FOLDER });
    expect(await attachPath("/p/big.png", { stat, read })).toEqual({ warn: FILE_TOO_BIG });
    expect(await attachPath("/p/gone.png", { stat, read })).toEqual({ error: "stat /p/gone.png: no such file or directory" });
    expect(await attachPath("/p/a.png", { stat, read })).toEqual({
      attachment: { kind: "picture", name: "a.png", mime: "image/png", path: "/p/a.png", base64: "iVBORw0KGgo=" },
    });
    expect(A_FOLDER).toBe("Cannot attach a directory");
    expect(FILE_TOO_BIG).toBe("File is too big (>5mb)");
  });

  it("dropped files become the text a terminal would paste for them", () => {
    const text = dropText(["/p/a b.png", "/p/c.png"]);
    expect(text).toBe("/p/a\\ b.png /p/c.png");
    expect(unixPaths(text)).toEqual(["/p/a b.png", "/p/c.png"]);
    expect(fileUriPaths("file:///p/a%20b.png\r\n# comment\nhttps://example.com/x.png\nfile://localhost/p/c.png")).toEqual([
      "/p/a b.png",
      "/p/c.png",
    ]);
  });
});

describe("a pasted picture (TYPE-014, TYPE-040)", () => {
  it("attaches paste_N.png when the model takes pictures", () => {
    const result = pictureAttachment(PNG, [{ name: "paste_1.txt" }], true);
    expect(result.attachment).toMatchObject({ name: "paste_2.png", kind: "picture", mime: "image/png" });
  });

  it("warns for a model without pictures and refuses over 5 MB", () => {
    expect(pictureAttachment(PNG, [], false)).toEqual({ warn: NO_PICTURES });
    expect(pictureAttachment(new Uint8Array(MAX_ATTACHMENT_BYTES + 1), [], true)).toEqual({ error: PICTURE_TOO_BIG });
    expect(NO_PICTURES).toBe("The current model does not support image attachments");
    expect(PICTURE_TOO_BIG).toBe("File too large, max 5MB");
  });

  it("knows PNG and JPEG files from the rest", () => {
    expect(isPictureFile({ type: "image/png" })).toBe(true);
    expect(isPictureFile({ type: "", name: "shot.jpeg" })).toBe(true);
    expect(isPictureFile({ type: "image/gif", name: "a.gif" })).toBe(false);
  });

  it("asks the agent's model", () => {
    expect(supportsImages({ model: { supports_attachments: true } })).toBe(true);
    expect(supportsImages({ model: { supports_attachments: false } })).toBe(false);
    expect(supportsImages(null)).toBe(false);
  });
});

describe("detectContentType (Go's sniffer)", () => {
  const bytes = (s) => new TextEncoder().encode(s);
  it("names what files.rs names", () => {
    expect(detectContentType(PNG)).toBe("image/png");
    expect(detectContentType(Uint8Array.from([0xff, 0xd8, 0xff, 0xe0]))).toBe("image/jpeg");
    expect(detectContentType(bytes("  <!DOCTYPE html>"))).toBe("text/html; charset=utf-8");
    expect(detectContentType(bytes("<pre>x</pre>"))).toBe("text/plain; charset=utf-8");
    expect(detectContentType(bytes("<?xml version"))).toBe("text/xml; charset=utf-8");
    expect(detectContentType(bytes("%PDF-1.7"))).toBe("application/pdf");
    expect(detectContentType(bytes("package main\n"))).toBe("text/plain; charset=utf-8");
    expect(detectContentType(bytes("colored \x1b[31mred\x1b[0m"))).toBe("text/plain; charset=utf-8");
    expect(detectContentType(bytes("text then \x1a, a control byte"))).toBe("application/octet-stream");
    expect(detectContentType(Uint8Array.from([0xef, 0xbb, 0xbf, 0x68]))).toBe("text/plain; charset=utf-8");
    expect(detectContentType(new Uint8Array(0))).toBe("text/plain; charset=utf-8");
  });
});
