// What a generic tool result is (CHAT-059): JSON is re-indented, a unified
// diff is drawn as a diff, Markdown-looking text is shown as Markdown source,
// anything else as plain text. Ports of chat/tool_result_content.go:23-59,
// chat/unified_diff.go:18-188 and diff/detect.go.

// -- JSON: json.Unmarshal into json.RawMessage, then json.MarshalIndent --

class JSONError extends Error {}

const NUMBER = /-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?/y;

// Reads one JSON value of the text the way encoding/json's scanner does and
// writes it as MarshalIndent(raw, "", "  ") would: the value's own string and
// number text kept, key order and repeated keys kept, <, >, &, U+2028 and
// U+2029 escaped, empty objects and arrays as {} and [].
function indentValue(text, at, depth, out) {
  at = skipSpace(text, at);
  const c = text[at];
  if (c === "{" || c === "[") {
    const close = c === "{" ? "}" : "]";
    at = skipSpace(text, at + 1);
    if (text[at] === close) {
      out.push(c + close);
      return at + 1;
    }
    out.push(c);
    const pad = "\n" + "  ".repeat(depth + 1);
    for (let first = true; ; first = false) {
      out.push(first ? pad : "," + pad);
      if (c === "{") {
        at = skipSpace(text, at);
        if (text[at] !== '"') throw new JSONError();
        at = readString(text, at, out);
        at = skipSpace(text, at);
        if (text[at] !== ":") throw new JSONError();
        out.push(": ");
        at += 1;
      }
      at = indentValue(text, at, depth + 1, out);
      at = skipSpace(text, at);
      if (text[at] === ",") {
        at += 1;
        continue;
      }
      if (text[at] !== close) throw new JSONError();
      out.push("\n" + "  ".repeat(depth) + close);
      return at + 1;
    }
  }
  if (c === '"') return readString(text, at, out);
  for (const word of ["true", "false", "null"]) {
    if (text.startsWith(word, at)) {
      out.push(word);
      return at + word.length;
    }
  }
  NUMBER.lastIndex = at;
  const m = NUMBER.exec(text);
  if (!m) throw new JSONError();
  // A number's text runs to the next delimiter; anything else glued on is an error.
  const end = at + m[0].length;
  if (end < text.length && !/[\s,\]}:]/.test(text[end])) throw new JSONError();
  out.push(m[0]);
  return end;
}

function skipSpace(text, at) {
  while (at < text.length && (text[at] === " " || text[at] === "\t" || text[at] === "\n" || text[at] === "\r")) at++;
  return at;
}

function readString(text, at, out) {
  let s = '"';
  let i = at + 1;
  for (;;) {
    if (i >= text.length) throw new JSONError();
    const ch = text[i];
    const code = text.charCodeAt(i);
    if (ch === '"') break;
    if (code < 0x20) throw new JSONError();
    if (ch === "\\") {
      const e = text[i + 1];
      if (e === "u") {
        if (!/^[0-9a-fA-F]{4}$/.test(text.slice(i + 2, i + 6))) throw new JSONError();
        s += text.slice(i, i + 6);
        i += 6;
        continue;
      }
      if (!'"\\/bfnrt'.includes(e ?? "x")) throw new JSONError();
      s += ch + e;
      i += 2;
      continue;
    }
    if (ch === "<" || ch === ">" || ch === "&" || ch === "\u2028" || ch === "\u2029") {
      s += "\\u" + code.toString(16).padStart(4, "0");
    } else s += ch;
    i++;
  }
  out.push(s + '"');
  return i + 1;
}

// The text re-indented as the terminal shows a JSON result, or null when it
// is not valid JSON (any JSON value counts, as json.Unmarshal accepts it).
export function indentJSON(text) {
  const t = String(text ?? "");
  const out = [];
  try {
    const end = skipSpace(t, indentValue(t, 0, 0, out));
    if (end !== t.length) return null;
  } catch (error) {
    // Too deep to walk counts as invalid, as Go's scanner stops at depth 10000.
    if (error instanceof JSONError || error instanceof RangeError) return null;
    throw error;
  }
  return out.join("");
}

// -- Unified diffs (diff/detect.go, chat/unified_diff.go) --

// IsUnifiedDiff: a git header and a file header, or a hunk and a file header.
export function isUnifiedDiff(content) {
  let hunk = false;
  let file = false;
  let git = false;
  for (const line of String(content ?? "").split("\n")) {
    if (line.startsWith("@@")) hunk = true;
    if (line.startsWith("--- ") || line.startsWith("+++ ")) file = true;
    if (line.startsWith("diff --git ")) git = true;
  }
  return (git && file) || (hunk && file);
}

function headerPath(line, header, side) {
  let path = line.slice(header.length);
  if (path.startsWith(side)) path = path.slice(side.length);
  const tab = path.indexOf("\t");
  return tab >= 0 ? path.slice(0, tab) : path;
}

// parseUnifiedDiff: each file's before and after text rebuilt from its
// hunks (line numbers restart at 1; context outside the hunks is unknown).
export function parseUnifiedDiff(content) {
  const files = [];
  let current = -1;
  let inHunk = false;
  const start = (path) => {
    files.push({ path, before: "", after: "" });
    current = files.length - 1;
  };
  const lines = String(content ?? "").split("\n");
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const nextIsPlusHeader = i + 1 < lines.length && lines[i + 1].startsWith("+++ ");
    if (line.startsWith("diff --git ")) {
      inHunk = false;
      // strings.SplitN(line, " ", 4): the fourth part keeps the rest of the line.
      const parts = line.split(" ");
      if (parts.length >= 4) {
        const rest = parts.slice(3).join(" ");
        start(rest.startsWith("b/") ? rest.slice(2) : rest);
      }
    } else if (line.startsWith("@@")) {
      inHunk = true;
    } else if (line.startsWith("index ") || line.startsWith("new file") || line.startsWith("deleted file")) {
      inHunk = false;
    } else if (line.startsWith("--- ") && (!inHunk || nextIsPlusHeader)) {
      const fromHunk = inHunk && nextIsPlusHeader;
      inHunk = false;
      const path = headerPath(line, "--- ", "a/");
      if (current < 0 || fromHunk) start(path);
      else if (path !== "/dev/null") files[current].path = path;
    } else if (line.startsWith("+++ ") && !inHunk) {
      const path = headerPath(line, "+++ ", "b/");
      if (path === "/dev/null") continue;
      if (current < 0) start(path);
      else if (files[current].path === "" || files[current].path.startsWith("/dev/null")) files[current].path = path;
    } else if (current >= 0) {
      const f = files[current];
      if (line.startsWith("-")) {
        inHunk = true;
        f.before += line.slice(1) + "\n";
      } else if (line.startsWith("+")) {
        inHunk = true;
        f.after += line.slice(1) + "\n";
      } else if (line.startsWith(" ")) {
        inHunk = true;
        f.before += line.slice(1) + "\n";
        f.after += line.slice(1) + "\n";
      }
    }
  }
  return files.map((f) => ({
    path: f.path,
    before: f.before.endsWith("\n") ? f.before.slice(0, -1) : f.before,
    after: f.after.endsWith("\n") ? f.after.slice(0, -1) : f.after,
  }));
}

// A unified diff as the terminal draws it (toolOutputDiffContentFromUnified):
// its files as a diff, or the text as "result.diff" code when no file parses.
export function unifiedBody(content) {
  const files = parseUnifiedDiff(content);
  if (files.length === 0) return { type: "code", path: "result.diff", text: String(content ?? ""), offset: 0 };
  return { type: "diff", files };
}

// -- Markdown --

const MARKDOWN_SIGNS = ["# ", "## ", "**", "```", "- ", "1. ", "> ", "---", "***"];

export function looksLikeMarkdown(content) {
  const text = String(content ?? "");
  return MARKDOWN_SIGNS.some((sign) => text.includes(sign));
}

// renderToolResultTextContent: the body for a text result.
export function sniffBody(content) {
  const text = String(content ?? "");
  const json = indentJSON(text);
  if (json !== null) return { type: "code", path: "result.json", text: json, offset: 0 };
  if (isUnifiedDiff(text)) return unifiedBody(text);
  if (looksLikeMarkdown(text)) return { type: "code", path: "result.md", text, offset: 0 };
  return { type: "plain", text };
}

// The kind sniffBody picks, for the probe and the tests.
export function sniffKind(content) {
  const body = sniffBody(content);
  if (body.type === "code") return body.path === "result.json" ? "json" : body.path === "result.md" ? "markdown" : "diff";
  return body.type === "diff" ? "diff" : "plain";
}
