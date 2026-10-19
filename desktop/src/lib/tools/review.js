// The file changes of a run of tool calls, one entry per file, for the
// group's summary (CHAT-083) and the Changes sheet (DLG-294 to DLG-311).
// Ports of diffreview/diffreview.go, diffreview/transfers.go,
// chat/changes.go:34-118, filechange/visibility.go and the copy rule of
// filechange/transfers.go:64-125.
import { lineEdits, toUnified, INSERT, DELETE } from "../diff.js";
import { decode } from "./go.js";

// Kind is what happened to a file.
export const MODIFIED = 0;
export const ADDED = 1;
export const DELETED = 2;
export const COPIED = 3;
export const MOVED = 4;
export const GENERATED = 5;

export function kindName(kind) {
  switch (kind) {
    case ADDED:
      return "Added";
    case DELETED:
      return "Deleted";
    case COPIED:
      return "Copied";
    case MOVED:
      return "Moved";
    case GENERATED:
      return "Generated";
  }
  return "Changed";
}

// LineKind is the role of a diff line.
export const CONTEXT = 0;
export const ADD = 1;
export const DEL = 2;
export const HUNK = 3;

const newFile = (path) => ({ path, kind: MODIFIED, lines: [], adds: 0, dels: 0, transfer: "", omitted: false });

// Counted: the file's line counts mean something (compared and still there).
export const counted = (f) => !f.omitted && f.kind !== DELETED;
export const anyCounted = (files) => files.some(counted);
export const removedCount = (files) => files.filter((f) => f.kind === DELETED).length;

// HasEdits: an authored content change, not only an import or a deletion.
export function hasEdits(f) {
  if (f.kind === DELETED) return false;
  return f.transfer === "" || f.adds !== 0 || f.dels !== 0 || f.lines.length > 1;
}

export function stats(files) {
  let adds = 0;
  let dels = 0;
  for (const f of files) {
    if (!counted(f)) continue;
    adds += f.adds;
    dels += f.dels;
  }
  return { adds, dels };
}

const TRANSFER_LABELS = { checkout: "copied checkout:", copy: "copied", move: "moved", generated: "generated", removed: "removed" };

// TransferSummary: imported and deleted files, without calling them edits.
export function transferSummary(files) {
  const counts = { removed: removedCount(files) };
  for (const f of files) counts[f.transfer] = (counts[f.transfer] ?? 0) + 1;
  const parts = [];
  for (const kind of ["checkout", "copy", "move", "generated", "removed"]) {
    const n = counts[kind] ?? 0;
    if (n > 0) parts.push(`${TRANSFER_LABELS[kind]} ${n} ${n === 1 ? "file" : "files"}`);
  }
  return parts.join(" · ");
}

// Summary: "2 files changed", or the transfers and the edited count.
export function reviewSummary(files) {
  const transfer = transferSummary(files);
  if (transfer === "") return `${files.length} ${files.length === 1 ? "file" : "files"} changed`;
  const edited = files.filter(hasEdits).length;
  return edited > 0 ? `${transfer} · ${edited} ${edited === 1 ? "file" : "files"} edited` : transfer;
}

// fs.FileMode(m).String(): type letters (or "-"), then rwxrwxrwx.
export function fileModeString(mode) {
  const types = "dalTLDpSugct?";
  let out = "";
  for (let i = 0; i < types.length; i++) if ((mode >>> (31 - i)) & 1) out += types[i];
  if (out === "") out = "-";
  const rwx = "rwxrwxrwx";
  for (let i = 0; i < 9; i++) out += (mode >>> (8 - i)) & 1 ? rwx[i] : "-";
  return out;
}

const STATE_FIELDS = [
  ["restore_digest_only", false],
  ["restore_data", ""],
  ["restore_omitted", ""],
  ["content", ""],
  ["digest", ""],
  ["size", 0],
  ["mode", 0],
  ["omitted", ""],
];
const sameState = (a, b) => STATE_FIELDS.every(([k, zero]) => (a[k] ?? zero) === (b[k] ?? zero));
const present = (state) => state !== null && state !== undefined;

// ImportedState: the baseline for edits made after a transfer.
export const importedState = (change) => change.transfer?.baseline ?? change.after ?? null;

// The hunk heading of a numbered diff ("@@ -1,3 +1,4 @@"; one line is "1",
// none is the line before).
function span(start, n) {
  if (n === 0) start--;
  if (n === 1) return String(start);
  return `${start},${n}`;
}

function hunkHeader(h) {
  let from = 0;
  let to = 0;
  for (const l of h.lines) {
    if (l.kind === INSERT) to++;
    else if (l.kind === DELETE) from++;
    else {
      from++;
      to++;
    }
  }
  return `@@ -${span(h.fromLine, from)} +${span(h.toLine, to)} @@`;
}

// fromContent: a diff of two texts with 3 lines of context; without numbers
// (only the replaced text is known) the hunks are joined by "…" rows.
export function fromContent(path, before, after, numbered) {
  if (before === after) return null;
  const f = newFile(path);
  if (numbered) {
    if (before === "") f.kind = ADDED;
    else if (after === "") f.kind = DELETED;
  }
  const ud = toUnified(path, path, before, lineEdits(before, after), 3);
  for (const h of ud.hunks) {
    let oldNo = h.fromLine;
    let newNo = h.toLine;
    if (numbered) f.lines.push({ kind: HUNK, old: 0, new: 0, text: hunkHeader(h) });
    else if (f.lines.length > 0) f.lines.push({ kind: HUNK, old: 0, new: 0, text: "…" });
    for (const l of h.lines) {
      const text = l.content.endsWith("\n") ? l.content.slice(0, -1) : l.content;
      const line = { kind: CONTEXT, old: 0, new: 0, text };
      if (l.kind === INSERT) {
        line.kind = ADD;
        line.new = newNo++;
        f.adds++;
      } else if (l.kind === DELETE) {
        line.kind = DEL;
        line.old = oldNo++;
        f.dels++;
      } else {
        line.old = oldNo++;
        line.new = newNo++;
      }
      if (!numbered) {
        line.old = 0;
        line.new = 0;
      }
      f.lines.push(line);
    }
  }
  return f.lines.length > 0 ? f : null;
}

const HUNK_RE = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/;

// fromUnified: the hunks of a one-file unified diff (Crush's own write).
export function fromUnified(path, diff) {
  const f = newFile(path);
  let oldNo = 0;
  let newNo = 0;
  let inHunk = false;
  for (const line of String(diff).split("\n")) {
    const m = HUNK_RE.exec(line);
    if (m) {
      oldNo = Number(m[1]);
      newNo = Number(m[2]);
      inHunk = true;
      f.lines.push({ kind: HUNK, old: 0, new: 0, text: line });
      continue;
    }
    if (!inHunk) {
      if (line === "--- /dev/null") f.kind = ADDED;
      else if (line === "+++ /dev/null") f.kind = DELETED;
      continue;
    }
    if (line === "") continue;
    if (line[0] === "+") {
      f.lines.push({ kind: ADD, old: 0, new: newNo++, text: line.slice(1) });
      f.adds++;
    } else if (line[0] === "-") {
      f.lines.push({ kind: DEL, old: oldNo++, new: 0, text: line.slice(1) });
      f.dels++;
    } else if (line[0] === " ") {
      f.lines.push({ kind: CONTEXT, old: oldNo++, new: newNo++, text: line.slice(1) });
    }
  }
  return f.adds + f.dels > 0 ? f : null;
}

// fromSnapshot: a captured change (presence, mode and text before and after).
export function fromSnapshot(change) {
  const transfer = change.transfer;
  if (transfer && !present(change.before) && present(change.after)) {
    // Imported lines are the baseline; only later edits get counts.
    const f = fromSnapshot({ path: change.path, before: importedState(change), after: change.after }) ?? newFile("");
    f.path = change.path;
    f.kind = COPIED;
    f.transfer = transfer.kind;
    let label = "Copied file";
    if (transfer.kind === "checkout") label = "Copied from repository checkout";
    if (transfer.kind === "move") {
      f.kind = MOVED;
      label = "Moved from " + (transfer.source ?? "");
    }
    if (transfer.kind === "generated") {
      f.kind = GENERATED;
      label = "Generated build artifact";
    }
    f.lines = [{ kind: CONTEXT, old: 0, new: 0, text: label }, ...f.lines];
    return f;
  }
  const before = present(change.before) ? change.before : null;
  const after = present(change.after) ? change.after : null;
  if ((!before && !after) || (before && after && sameState(before, after))) return null;
  // Terminals, pipes and sockets only ever change their timestamps.
  const special = (s) => s && String(s.omitted ?? "").startsWith("Special file");
  if (special(before) || special(after)) return null;
  const f = newFile(change.path);
  if (!before) f.kind = ADDED;
  else if (!after) f.kind = DELETED;
  const note = (text) => f.lines.push({ kind: CONTEXT, old: 0, new: 0, text });
  if (before && after && (before.mode ?? 0) !== (after.mode ?? 0)) {
    note(`Mode: ${fileModeString(before.mode ?? 0)} → ${fileModeString(after.mode ?? 0)}`);
  }
  let oldText = "";
  let newText = "";
  let oldSize = 0;
  let newSize = 0;
  let omitted = "";
  if (before) {
    oldText = before.content ?? "";
    oldSize = before.size ?? 0;
    omitted = before.omitted ?? "";
  }
  if (after) {
    newText = after.content ?? "";
    newSize = after.size ?? 0;
    if (after.omitted) omitted = after.omitted;
  }
  if (omitted !== "") {
    f.omitted = true;
    note(`${omitted}; ${oldSize} → ${newSize} bytes`);
  } else {
    const content = fromContent(change.path, oldText, newText, true);
    if (content) {
      f.lines.push(...content.lines);
      f.adds = content.adds;
      f.dels = content.dels;
    } else if (f.lines.length === 0) note(kindName(f.kind) + " empty file");
  }
  return f;
}

// Build: one diff per file, in the order the files were first changed.
// Edits: {path, before, after, full, unified, snapshot}.
export function buildReview(edits) {
  const order = [];
  const byPath = new Map();
  for (const e of edits) {
    if (!byPath.has(e.path)) {
      order.push(e.path);
      byPath.set(e.path, []);
    }
    byPath.get(e.path).push(e);
  }
  const files = [];
  const movedSources = new Set();
  for (const es of byPath.values()) {
    if (!es.every((e) => e.snapshot)) continue;
    // A stable sort by capture order, kept for the loop below as in Go.
    const sorted = es
      .map((e, i) => [e, i])
      .sort((a, b) => (a[0].snapshot.order ?? 0) - (b[0].snapshot.order ?? 0) || a[1] - b[1])
      .map(([e]) => e);
    es.splice(0, es.length, ...sorted);
    const first = es[0].snapshot;
    const last = es[es.length - 1].snapshot;
    if (first.transfer && first.transfer.kind === "move" && present(last.after)) movedSources.add(first.transfer.source ?? "");
  }
  for (const path of order) {
    const es = byPath.get(path);
    if (es.every((e) => e.snapshot)) {
      const change = { ...es[0].snapshot };
      if (change.transfer) change.transfer = { ...change.transfer, baseline: importedState(change) };
      change.after = es[es.length - 1].snapshot.after ?? null;
      if (movedSources.has(path) && !present(change.after)) continue;
      const f = fromSnapshot(change);
      if (f) files.push(f);
      continue;
    }
    if (es.every((e) => e.full && !e.snapshot)) {
      const f = fromContent(path, es[0].before ?? "", es[es.length - 1].after ?? "", true);
      if (f) files.push(f);
      continue;
    }
    for (const e of es) {
      let f;
      if (e.snapshot) f = fromSnapshot(e.snapshot);
      else if (e.unified) f = fromUnified(path, e.unified);
      else f = fromContent(path, e.before ?? "", e.after ?? "", Boolean(e.full));
      if (f) files.push(f);
    }
  }
  return files;
}

// -- Which paths a review hides (filechange/visibility.go) --

// filepath.Clean for slash paths.
export function cleanPath(p) {
  const path = String(p ?? "");
  if (path === "") return ".";
  const rooted = path.startsWith("/");
  const out = [];
  for (const part of path.split("/")) {
    if (part === "" || part === ".") continue;
    if (part === "..") {
      if (out.length && out[out.length - 1] !== "..") out.pop();
      else if (!rooted) out.push("..");
      continue;
    }
    out.push(part);
  }
  const s = (rooted ? "/" : "") + out.join("/");
  return s === "" ? "." : s;
}

const dirOf = (p) => {
  const i = p.lastIndexOf("/");
  if (i < 0) return ".";
  return cleanPath(p.slice(0, i + 1) || "/");
};
const baseOf = (p) => p.slice(p.lastIndexOf("/") + 1);

const INSTRUCTION_FILES = new Set(["AGENTS.md", "CLAUDE.md", "CLAUDE.local.md", "CRUSH.md", "GEMINI.md"]);
const SCRATCH = new Set(["scratch", "tmp", "temp"]);
const EXCLUDED = ["/tmp", "/var/tmp", "/dev", "/proc", "/sys"];

// HiddenReviewPath: noise a review never shows. The window does not know
// the home folder, so the terminal's "~/.local/bin stays visible" rule is
// kept for any .local/bin folder, and the home-based cache, config and data
// folders are covered by the dot-folder rule (they all sit in ~/.cache,
// ~/.config and ~/.local).
export function hiddenReviewPath(path, root = "") {
  let p = String(path ?? "");
  if (root !== "" && !p.startsWith("/")) p = root.replace(/\/+$/, "") + "/" + p;
  p = cleanPath(p);
  const base = baseOf(p);
  if (!INSTRUCTION_FILES.has(base)) {
    const folders = dirOf(p).split("/");
    const tools = folders.findIndex((f, i) => f === ".local" && folders[i + 1] === "bin");
    const userTools = tools >= 0 || p.endsWith("/.local/bin");
    if (!userTools && folders.some((f) => f.startsWith(".") && f !== "." && f !== "..")) return true;
  }
  if (dirOf(p).split("/").some((f) => SCRATCH.has(f.toLowerCase()))) return true;
  if (!INSTRUCTION_FILES.has(base)) {
    for (const component of p.split("/")) {
      const name = component.toLowerCase();
      if (name === "skills-lock.json" || name.startsWith("agentdb.") || name.startsWith("claude-flow") || name.startsWith("ruflo")) return true;
    }
  }
  return EXCLUDED.some((dir) => p === dir || p.startsWith(dir + "/"));
}

// -- A single literal copy or clone in an older saved bash result --

// CommandTransferKind: "copy" for one plain cp command, "checkout" for one
// git clone, else "". Only a single simple command of literal and quoted
// words counts (no expansions, redirects, pipes, lists or assignments).
export function commandTransferKind(command) {
  const args = simpleCommandArgs(String(command ?? ""));
  if (!args || args.length === 0) return "";
  const program = args[0].slice(args[0].lastIndexOf("/") + 1);
  if (program === "cp") return "copy";
  if (program === "git") {
    for (let i = 1; i < args.length; i++) {
      const arg = args[i];
      if (arg === "-C" || arg === "-c" || arg === "--git-dir" || arg === "--work-tree") i++;
      else if (arg.startsWith("--git-dir=") || arg.startsWith("--work-tree=") || arg === "--no-pager" || arg === "--no-optional-locks") continue;
      else if (arg === "clone") return "checkout";
      else return "";
    }
  }
  return "";
}

// The words of a single simple shell command, or null when the text is
// anything else.
function simpleCommandArgs(text) {
  const args = [];
  let i = 0;
  let ended = false;
  const blank = (c) => c === " " || c === "\t";
  while (i < text.length) {
    const c = text[i];
    if (blank(c)) {
      i++;
      continue;
    }
    if (c === "\n" || c === ";") {
      // One statement may end with a terminator; a second one is a list.
      ended = true;
      i++;
      continue;
    }
    if (c === "#") {
      const nl = text.indexOf("\n", i);
      i = nl < 0 ? text.length : nl;
      continue;
    }
    if (ended) return null;
    if ("|&<>()$`".includes(c)) return null;
    let word = "";
    while (i < text.length && !blank(text[i]) && text[i] !== "\n" && text[i] !== ";") {
      const ch = text[i];
      if ("|&<>()$`".includes(ch)) return null;
      if (ch === "'") {
        const close = text.indexOf("'", i + 1);
        if (close < 0) return null;
        word += text.slice(i + 1, close);
        i = close + 1;
      } else if (ch === '"') {
        let j = i + 1;
        let inner = "";
        while (j < text.length && text[j] !== '"') {
          if (text[j] === "$" || text[j] === "`") return null;
          if (text[j] === "\\" && j + 1 < text.length) {
            inner += text[j] + text[j + 1];
            j += 2;
            continue;
          }
          inner += text[j];
          j++;
        }
        if (j >= text.length) return null;
        word += inner;
        i = j + 1;
      } else if (ch === "\\" && i + 1 < text.length) {
        word += ch + text[i + 1];
        i += 2;
      } else {
        word += ch;
        i++;
      }
    }
    if (args.length === 0 && /^[A-Za-z_][A-Za-z0-9_]*=/.test(word)) return null;
    args.push(word);
  }
  return args;
}

// -- The review inputs of a group (chat/changes.go, review_inputs.go) --

const EDIT_INPUT = { file_path: "string", old_string: "string", new_string: "string", replace_all: "bool" };
const EDIT_RESULT = { additions: "int", removals: "int", old_content: "string", new_content: "string", diff: "string" };

// toolEdit: the change a file tool made, from its result's metadata.
export function toolEdit(call, result) {
  if (call.name !== "edit" && call.name !== "multiedit" && call.name !== "write") return null;
  const input = decode(call.input ?? "", EDIT_INPUT).value;
  if (input.file_path === "" || !result.metadata) return null;
  const { value: meta, ok } = decode(result.metadata, EDIT_RESULT);
  if (!ok) return null;
  if (meta.old_content !== "" || meta.new_content !== "") {
    return {
      path: input.file_path,
      before: meta.old_content,
      after: meta.new_content,
      // Older CLI edits stored only the replaced text, with no counts.
      full:
        call.name !== "edit" || meta.additions + meta.removals > 0 || input.old_string !== meta.old_content || input.new_string !== meta.new_content,
    };
  }
  if (meta.diff !== "") return { path: input.file_path, unified: meta.diff };
  return null;
}

// BuildReviewChanges: inputs are [{call: {name, input}, result}] with full
// reviews (a saved summary is skipped here; the sheet loads it first).
export function buildReviewChanges(inputs) {
  const edits = [];
  const unavailable = [];
  for (const { call, result } of inputs) {
    const review = result.review;
    if (review) {
      if (review.summary) continue;
      const command = call.name === "bash" ? decode(call.input ?? "", { command: "string" }).value.command : "";
      const kind = commandTransferKind(command);
      for (const original of review.changes ?? []) {
        if (hiddenReviewPath(original.path, review.root ?? "")) continue;
        let change = original;
        if (!change.transfer && kind !== "" && !present(change.before) && present(change.after)) change = { ...change, transfer: { kind } };
        edits.push({ path: change.path, snapshot: change });
      }
      continue;
    }
    if (result.is_error) continue;
    const omitted = decode(result.metadata ?? "", { review_omitted: "string" }).value.review_omitted;
    if (omitted !== "") {
      const path = decode(call.input ?? "", EDIT_INPUT).value.file_path;
      if (path !== "" && !hiddenReviewPath(path, "")) unavailable.push({ ...newFile(path), lines: [{ kind: CONTEXT, old: 0, new: 0, text: omitted }] });
      continue;
    }
    const edit = toolEdit(call, result);
    if (edit && !hiddenReviewPath(edit.path, "")) edits.push(edit);
  }
  return [...buildReview(edits), ...unavailable];
}
