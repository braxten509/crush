// The paste rules (TYPE-010 to TYPE-014, TYPE-040): what a paste, a dropped
// file or a pasted picture becomes. A port of internal/ui/model/paste.go and
// internal/fsext/paste.go. Pure apart from the stat and read functions the
// caller passes in (file_info and file_read, desktop-design 1.3).
import { detectContentType } from "./sniff.js";

// common.MaxAttachmentSize (internal/ui/common/common.go:21).
export const MAX_ATTACHMENT_BYTES = 5 * 1024 * 1024;
export const PASTE_LINES = 10;
export const PASTE_COLUMNS = 1000;

export const PASTE_TOO_BIG = "Paste is too big (>5mb)";
export const NO_PICTURES = "The current model does not support image attachments";
export const A_FOLDER = "Cannot attach a directory";
export const FILE_TOO_BIG = "File is too big (>5mb)";
export const EMPTY_CLIPBOARD = "Clipboard is empty or does not contain text";
export const PICTURE_TOO_BIG = "File too large, max 5MB";
export const NOT_A_PICTURE = "File type is not a supported image format";
export const unreadable = (why) => `Unable to read file: ${why}`;

const encoder = new TextEncoder();

// handlePasteMsg first turns \r\n into \n (paste.go:56-57).
export function normalize(text) {
  return String(text ?? "").replaceAll("\r\n", "\n");
}

// UTF-8 length, as Go's len(line).
function byteLength(text) {
  let n = 0;
  for (const ch of text) {
    const code = ch.codePointAt(0);
    n += code < 0x80 ? 1 : code < 0x800 ? 2 : code < 0x10000 ? 3 : 4;
  }
  return n;
}

// hasPasteExceededThreshold (paste.go:122-136): more than 10 lines, or a line
// of more than 1,000 bytes.
export function overThreshold(text) {
  let lines = 0;
  for (const line of text.split("\n")) {
    lines += 1;
    if (lines > PASTE_LINES) return true;
    if (line.length > PASTE_COLUMNS || (line.length * 3 > PASTE_COLUMNS && byteLength(line) > PASTE_COLUMNS)) return true;
  }
  return false;
}

// pasteIdx (paste.go:261-276): one more than the highest paste number among
// the field's attachments. The terminal counts only paste_N.txt, so two pasted
// pictures both become paste_1.png; the desktop counts the pictures too, so
// each name is new (recorded in the piece's report).
const PASTE_NAME = /paste_(\d+)\.(?:txt|png)/;
export function pasteIndex(attachments) {
  let most = 0;
  for (const attachment of attachments ?? []) {
    const found = PASTE_NAME.exec(attachment?.name ?? "");
    if (found) most = Math.max(most, Number(found[1]));
  }
  return most + 1;
}

// common.IsImagePath (internal/ui/common/common.go:120-130).
export function isImagePath(path) {
  const lower = String(path).toLowerCase();
  return lower.endsWith(".jpg") || lower.endsWith(".jpeg") || lower.endsWith(".png");
}

// The Attachment kind (lib/types.js) for a type.
export function kindOf(mime) {
  if (mime.startsWith("image/")) return "picture";
  if (mime.startsWith("text/")) return "text";
  return "file";
}

export function baseName(path) {
  const parts = String(path).split("/").filter(Boolean);
  return parts.length ? parts[parts.length - 1] : String(path);
}

export function toBase64(data) {
  let text = "";
  for (let at = 0; at < data.length; at += 0x8000) text += String.fromCharCode(...data.subarray(at, at + 0x8000));
  return btoa(text);
}

// unixParsePastedFiles (internal/fsext/paste.go:80-129): spaces separate the
// paths, a backslash keeps the next character, a final backslash stays.
export function unixPaths(text) {
  if (text.trim() === "") return [];
  const paths = [];
  let current = "";
  let escaped = false;
  for (let i = 0; i < text.length; i += 1) {
    const ch = text[i];
    if (escaped) {
      current += ch;
      escaped = false;
    } else if (ch === "\\") {
      if (i === text.length - 1) current += ch;
      else escaped = true;
    } else if (ch === " ") {
      if (current !== "") paths.push(current);
      current = "";
    } else {
      current += ch;
    }
  }
  if (escaped) current += "\\";
  if (current !== "") paths.push(current);
  return paths;
}

// ParsePastedFiles (internal/fsext/paste.go:8-31) reads a paste one of two
// ways: a path per line when every line names a file, else unixPaths. Both
// readings, after its trimming.
export function pathReadings(text) {
  const trimmed = text.trim().replaceAll("\0", "");
  return { lines: trimmed.split("\n"), words: unixPaths(trimmed) };
}

// What a text paste becomes, decided at once (a paste event cannot wait for a
// file lookup before letting the field take the text):
//   "attach"  over the threshold: a paste_N.txt attachment (TYPE-011)
//   "paths"   every path of one reading names a picture: look the files up
//             with pastedPictures before deciding (TYPE-012)
//   "text"    the field takes it (then bang mode may start, TYPE-013)
export function pasteRoute(text) {
  if (overThreshold(text)) return "attach";
  const { lines, words } = pathReadings(text);
  if (lines.every(isImagePath) || (words.length > 0 && words.every(isImagePath))) return "paths";
  return "text";
}

async function look(stat, path) {
  try {
    return await stat(path);
  } catch (error) {
    return { exists: true, is_dir: false, failed: error };
  }
}

// allExistsAndValid (paste.go:87-105): the paths when every one exists and
// names a picture, else null (the paste is text). stat answers file_info's
// {exists, is_dir}. A look that fails counts as a file for this test, as Go's
// os.IsNotExist does, and attachPath reports it; for choosing the per-line
// reading it does not (attemptStat).
export async function pastedPictures(text, stat) {
  const { lines, words } = pathReadings(text);
  const byLine = await Promise.all(lines.map((path) => look(stat, path)));
  const paths = byLine.every((info) => info.exists && !info.failed && !info.is_dir) ? lines : words;
  if (paths.length === 0 || !paths.every(isImagePath)) return null;
  const found = await Promise.all(paths.map((path) => look(stat, path)));
  return found.every((info) => info.exists) ? paths : null;
}

const errorText = (error) => error?.message ?? String(error);

// handleFilePathPaste (paste.go:138-167): one pasted or dropped path as an
// attachment, or the notice shown instead. read answers file_read's
// {mime, base64}.
export async function attachPath(path, { stat, read }) {
  let info;
  try {
    info = await stat(path);
  } catch (error) {
    return { error: errorText(error) };
  }
  if (!info.exists) return { error: `stat ${path}: no such file or directory` };
  if (info.is_dir) return { warn: A_FOLDER };
  if (info.size > MAX_ATTACHMENT_BYTES) return { warn: FILE_TOO_BIG };
  let file;
  try {
    file = await read(path, MAX_ATTACHMENT_BYTES);
  } catch (error) {
    if (error?.code === "too_big") return { warn: FILE_TOO_BIG };
    return { error: errorText(error) };
  }
  return { attachment: { kind: kindOf(file.mime), name: baseName(path), mime: file.mime, path, base64: file.base64 } };
}

// A long paste as paste_N.txt (paste.go:67-85).
export function textAttachment(text, attachments) {
  const data = encoder.encode(text);
  if (data.length > MAX_ATTACHMENT_BYTES) return { warn: PASTE_TOO_BIG };
  const name = `paste_${pasteIndex(attachments)}.txt`;
  const mime = detectContentType(data);
  return { attachment: { kind: kindOf(mime), name, mime, path: name, base64: toBase64(data) } };
}

// Picture data from the clipboard as paste_N.png (pasteImageFromClipboard,
// paste.go:193-214).
export function pictureAttachment(data, attachments, supportsImages) {
  if (!supportsImages) return { warn: NO_PICTURES };
  if (data.length > MAX_ATTACHMENT_BYTES) return { error: PICTURE_TOO_BIG };
  const name = `paste_${pasteIndex(attachments)}.png`;
  const mime = detectContentType(data);
  return { attachment: { kind: kindOf(mime), name, mime, path: name, base64: toBase64(data) } };
}

// A clipboard file that is not PNG or JPEG data (paste.go:227-229).
export function isPictureFile({ type = "", name = "" }) {
  return type === "image/png" || type === "image/jpeg" || isImagePath(name);
}

// file:// lines of a text/uri-list as paths; other lines and comments are
// left out.
export function fileUriPaths(list) {
  const paths = [];
  for (const line of String(list ?? "").split(/\r?\n/)) {
    const entry = line.trim();
    if (!entry.startsWith("file://")) continue;
    try {
      const url = new URL(entry);
      if (url.host === "" || url.host === "localhost") paths.push(decodeURIComponent(url.pathname));
    } catch {
      // not a URL
    }
  }
  return paths;
}

// Dropped files as the text a terminal pastes for them: the paths with their
// spaces and backslashes escaped, one space apart, which unixPaths reads back.
export function dropText(paths) {
  return paths.map((path) => path.replace(/([\\ ])/g, "\\$1")).join(" ");
}

// The model takes pictures (TYPE-040; currentModelSupportsImages,
// internal/ui/model/model_select.go:387-398).
export function supportsImages(agent) {
  return Boolean(agent?.model?.supports_attachments);
}
