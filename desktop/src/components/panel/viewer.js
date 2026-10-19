// The picture viewer's words and sources (4.5). Pure: ImageViewer draws.
//
//   viewerSource(image)        what ui.viewer asks for: {kind: "file", path} for a
//                              project file (read with file_read), {kind: "url", src}
//                              for a data: or blob: picture, or null (anything else:
//                              the content rule would block it, CHAT-007)
//   base64Bytes(text)          the decoded size of base64 text
//   dataBytes(src)             a data: URL's decoded size, or null (blob: URLs)
//   pictureName(image)         the name in the bar: the given name, else the path's last part
//   pictureSize(w, h, bytes)   "1280 × 720 · 214 KB" (bytes null: the size alone)

export function viewerSource(image) {
  if (!image || typeof image !== "object") return null;
  if (typeof image.path === "string" && image.path !== "") return { kind: "file", path: image.path };
  const src = typeof image.src === "string" ? image.src.trim() : "";
  return /^(?:data|blob):/iu.test(src) ? { kind: "url", src } : null;
}

export function base64Bytes(text) {
  const body = String(text ?? "").replace(/[\s=]+/gu, "");
  return Math.floor((body.length * 3) / 4);
}

export function dataBytes(src) {
  const match = /^data:([^,]*),(.*)$/isu.exec(String(src ?? ""));
  if (!match) return null;
  if (/;base64$/iu.test(match[1])) return base64Bytes(match[2]);
  try {
    return new TextEncoder().encode(decodeURIComponent(match[2])).length;
  } catch {
    return match[2].length;
  }
}

export function pictureName(image) {
  const name = typeof image?.name === "string" ? image.name.trim() : "";
  if (name) return name;
  const path = typeof image?.path === "string" ? image.path : "";
  return path.split(/[\\/]/u).filter(Boolean).pop() ?? "";
}

function byteWords(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

export function pictureSize(width, height, bytes) {
  const parts = [];
  if (width > 0 && height > 0) parts.push(`${width} × ${height}`);
  if (Number.isFinite(bytes) && bytes >= 0) parts.push(byteWords(bytes));
  return parts.join(" · ");
}
