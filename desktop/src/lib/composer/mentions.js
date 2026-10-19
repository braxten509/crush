// The `@` mention as pure rules (TYPE-026 to TYPE-030, TYPE-040): when the
// popup opens and closes, how its rows rank, what a pick puts in the field and
// what it attaches. A port of internal/ui/model/mention.go, editor.go:277-428
// and internal/ui/completions/completions.go:20-310.
import { fuzzyFind } from "./fuzzy.js";
import { baseName, isImagePath, kindOf, MAX_ATTACHMENT_BYTES, NO_PICTURES, toBase64 } from "./paste.js";

export const MAX_ROWS = 10;
export const MIN_WIDTH = 10;
export const MAX_WIDTH = 100;

const space = (ch) => ch !== undefined && /\s/u.test(ch);

// triggers (mention.go:25-33): an "@" typed at the start of the text or after
// white space. The terminal looks at the end of the text, where its caret
// always is when typing; the field looks at the caret. at is where the "@" is.
export function opensAt(value, at) {
  return value[at] === "@" && (at === 0 || space(value[at - 1]));
}

// The run of non-space characters around the caret (the textarea's Word()).
export function wordAt(value, caret) {
  let start = caret;
  while (start > 0 && !space(value[start - 1])) start -= 1;
  let end = caret;
  while (end < value.length && !space(value[end])) end += 1;
  return { start, end, text: value.slice(start, end) };
}

// follow (mention.go:41-62): the query while the caret's word is still the
// mention, else null (the popup closes): the caret moved before the "@", a
// space ended the word, or the word is no longer the one that starts at the
// "@". The terminal only asks that the word start with "@"; a click can put
// the field's caret in another word, so the field also asks that it be the
// same one.
export function mentionQuery(value, caret, start) {
  if (caret <= start || value[start] !== "@") return null;
  const word = wordAt(value, caret);
  if (word.start !== start || !word.text.startsWith("@")) return null;
  return word.text.slice(1);
}

// The span the pick replaces: the "@" and the word after it.
export function mentionSpan(value, start) {
  let end = start + 1;
  while (end < value.length && !space(value[end])) end += 1;
  return { start, end };
}

// insertCompletionText (editor.go:277-292): the text in place of the span,
// then a space, the caret after it. The terminal moves its caret to the end of
// the whole text and types the space there; the field keeps it where the text
// went, as TYPE-029 says, and uses a space that already follows instead of
// adding a second one.
export function insertPick(value, span, text) {
  const rest = value.slice(span.end);
  const inserted = /^[ \t]/.test(rest) ? text : `${text} `;
  return { value: value.slice(0, span.start) + inserted + rest, caret: span.start + text.length + 1 };
}

// Ctrl+N and Ctrl+P show the row's text in the span without a space and keep
// the popup open; the span grows or shrinks with it.
export function previewPick(value, span, text) {
  return { value: value.slice(0, span.start) + text + value.slice(span.end), span: { start: span.start, end: span.start + text.length } };
}

// The popup's rows from files_list (sorted as the terminal sorts its list):
// a folder's text ends in "/". MCP resources would follow as
// "server/title or uri" (SetItems, completions.go:159-202), but no engine
// route lists them (recorded in the piece's report), so none are added.
export function fileRows(entries) {
  return (entries ?? []).map((entry) => ({
    kind: "file",
    path: entry.path,
    dir: Boolean(entry.is_dir),
    text: entry.is_dir ? `${entry.path}/` : entry.path,
  }));
}

export function resourceRows(server, resources) {
  return (resources ?? []).map((resource) => ({
    kind: "resource",
    server,
    uri: resource.uri,
    title: resource.name ?? "",
    mime: resource.mimeType ?? resource.mime_type ?? "",
    text: `${server}/${resource.name || resource.uri}`,
  }));
}

const TIER_EXACT = 0;
const TIER_PREFIX = 1;
const TIER_SEGMENT = 2;
const TIER_FALLBACK = 3;

function stemOf(base) {
  const dot = base.lastIndexOf(".");
  return dot > -1 ? base.slice(0, dot) : base;
}

// namePriorityTier (completions.go:252-272).
export function tierOf(path, queryLower) {
  if (queryLower === "") return TIER_FALLBACK;
  const pathLower = path.toLowerCase();
  const parts = path.replaceAll("\\", "/").split("/").filter(Boolean);
  const baseLower = (parts.length ? parts[parts.length - 1] : path.endsWith("/") ? "/" : ".").toLowerCase();
  if (baseLower === queryLower || stemOf(baseLower) === queryLower) return TIER_EXACT;
  if (baseLower.startsWith(queryLower)) return TIER_PREFIX;
  if (pathLower.split(/[/\\]/).filter(Boolean).includes(queryLower)) return TIER_SEGMENT;
  return TIER_FALLBACK;
}

// applyNamePriorityFilter (completions.go:225-250): the fuzzy matches, best
// score first, then stably by tier: exact name, name prefix, path segment,
// the rest. An empty query keeps every row in its order. Each result is
// {row, chars}: chars are the matched characters' positions in row.text.
export function rankRows(rows, query) {
  if (query === "") return rows.map((row) => ({ row, chars: [] }));
  const found = fuzzyFind(query, rows.map((row) => row.text));
  const queryLower = query.trim().toLowerCase();
  return found
    .map((match) => ({ row: rows[match.index], chars: match.chars, tier: tierOf(match.text, queryLower) }))
    .sort((a, b) => a.tier - b.tier)
    .map(({ row, chars }) => ({ row, chars }));
}

// The popup's width in characters (updateSize, completions.go:274-291): the
// widest shown row plus 2, between 10 and 100.
export function popupColumns(rows) {
  const widest = rows.reduce((most, { row }) => Math.max(most, [...row.text].length), 0);
  return Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, widest + 2));
}

// The row the keys move to (selection.go:38-122 with the list drawn reversed,
// completions.go:102): the best row is drawn lowest and is index 0; ↑ and
// Ctrl+P go up the screen (a higher index), ↓ and Ctrl+N down, both wrapping.
export function moveFocus(focused, count, direction) {
  if (count === 0) return 0;
  if (direction === "up") return focused + 1 < count ? focused + 1 : 0;
  return focused > 0 ? focused - 1 : count - 1;
}

// filepath.Abs for a path from files_list, against the project folder.
export function absolutePath(cwd, path) {
  if (path.startsWith("/")) return path;
  return `${String(cwd ?? "").replace(/\/+$/, "")}/${path.replace(/^(\.\/)+/, "")}`;
}

const lastReadMs = (answer) => {
  const ms = typeof answer === "number" ? answer : Date.parse(String(answer ?? ""));
  return Number.isFinite(ms) && ms > 0 ? ms : 0;
};

// insertFileCompletion and applyFileCompletion (editor.go:294-368): what
// picking a file attaches. In a chat, a file the chat has read and that has
// not changed since is skipped (lastRead answers the read time, the zero time
// when never); before a chat exists the picks are only de-duplicated (reads:
// the absolute paths picked so far, which this adds to). A file that cannot be
// read attaches nothing: the AI reads it itself.
//   → {attachment} | {warn} | {skip: true}
export async function pickFile(path, { sessionId, absolute, supportsImages, reads, stat, read, lastRead }) {
  if (!supportsImages && isImagePath(path)) return { warn: NO_PICTURES };
  if (sessionId) {
    let readAt = 0;
    try {
      readAt = lastReadMs(await lastRead(sessionId, absolute));
    } catch {
      readAt = 0;
    }
    if (readAt > 0) {
      try {
        const info = await stat(path);
        if (info.exists && info.modified <= readAt) return { skip: true };
      } catch {
        // stat failed: read it below, as Go does
      }
    }
  }
  if (!sessionId && reads.includes(absolute)) return { skip: true };
  reads.push(absolute);
  let file;
  try {
    file = await read(path, MAX_ATTACHMENT_BYTES);
  } catch {
    return { skip: true };
  }
  return { attachment: { kind: kindOf(file.mime), name: baseName(path), mime: file.mime, path, base64: file.base64 } };
}

// insertMCPResourceCompletion (editor.go:370-428): the first content's text or
// data; nothing when empty or unreadable; the type falls back to text/plain.
// Not reachable from the popup (no route lists resources); kept tested for
// when one does.
export async function pickResource(row, { supportsImages, readResource }) {
  const display = row.title || row.uri;
  let contents;
  try {
    contents = await readResource(row.server, row.uri);
  } catch {
    return { skip: true };
  }
  const first = Array.isArray(contents) ? contents[0] : null;
  if (!first) return { skip: true };
  let base64 = "";
  if (first.text) base64 = toBase64(new TextEncoder().encode(first.text));
  else if (first.blob) base64 = first.blob;
  if (!base64) return { skip: true };
  const mime = row.mime || first.mimeType || first.mime_type || "text/plain";
  if (!supportsImages && mime.startsWith("image/")) return { warn: NO_PICTURES };
  return { attachment: { kind: kindOf(mime), name: display, mime, path: row.uri, base64 } };
}
