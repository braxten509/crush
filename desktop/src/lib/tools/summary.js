// What a tool group's row says (CHAT-080 to CHAT-085): the count, the
// changed files or, when none changed, what the tools did, the failed and
// backgrounded extras, and the mark. Ports of chat/toolgroup_header.go:16-306,
// chat/review_inputs.go:18-101 and chat/changes.go:14-150; the verbs are the
// desktop's own (desktop-design 4.6).
import { cells } from "../diff.js";
import { baseName, decode, truncate } from "./go.js";
import { anyCounted, buildReviewChanges, DELETED, hasEdits, hiddenReviewPath, stats } from "./review.js";
import { movedToBackground, toolStatus } from "./status.js";

// "Ran 4 tools" (PLAN.md:147 wording; the terminal says "4 actions").
export function countLabel(n) {
  return n === 1 ? "Ran 1 tool" : `Ran ${n} tools`;
}

// editedFile: the file an edit, multi-edit or write touched, unless its
// review says no file changed or it deleted the file.
export function editedFile(call) {
  const review = call.result?.review;
  if (review) {
    if ((review.summary && review.summary.files === 0) || (!review.summary && (review.changes ?? []).length === 0)) return "";
  }
  if (call.name !== "edit" && call.name !== "multiedit" && call.name !== "write") return "";
  const p = decode(call.input ?? "", { file_path: "string", delete_file: "bool" }).value;
  return p.delete_file ? "" : p.file_path;
}

// tallySteps: the tools, the failed and backgrounded ones, and the base names
// of the files they edited (each once, in order).
export function tally(calls, marks = {}) {
  const out = { tools: 0, failed: 0, moved: 0, edited: [] };
  const seen = new Set();
  for (const call of calls) {
    out.tools++;
    if (toolStatus(call, marks) === "error") out.failed++;
    if (movedToBackground(call.result, call.name)) out.moved++;
    const f = editedFile(call);
    if (f !== "" && !seen.has(f) && !hiddenReviewPath(f, "")) {
      seen.add(f);
      out.edited.push(baseName(f));
    }
  }
  return out;
}

// changeKind: the verb for what happened to one file.
export function changeKind(f) {
  if (f.kind === DELETED) return "removed";
  switch (f.transfer) {
    case "checkout":
      return "checked out";
    case "copy":
      return "copied";
    case "move":
      return "moved";
    case "generated":
      return "generated";
  }
  return "edited";
}

const MAX_NAME = 24;

// shortFileName: a name over 24 cells keeps its start and its last 8 cells.
export function shortFileName(name) {
  if (cells(name) <= MAX_NAME) return name;
  const tail = 8;
  const head = MAX_NAME - tail - 1;
  return truncate(name, head, "") + "…" + lastCells(name, tail);
}

function lastCells(text, n) {
  const chars = [...text];
  let width = 0;
  let i = chars.length;
  while (i > 0 && width + cells(chars[i - 1]) <= n) width += cells(chars[--i]);
  return chars.slice(i).join("");
}

// fileChangeLabel: "edited a.go", "moved 3 files", "4 files changed".
export function fileChangeLabel(kinds, name) {
  let total = 0;
  let verb = "";
  for (const [kind, n] of Object.entries(kinds)) {
    if (n === 0) continue;
    total += n;
    verb = verb === "" ? kind : "mixed";
  }
  if (total === 0) return "";
  if (total === 1 && name !== "") return verb + " " + shortFileName(name);
  if (total === 1) return verb + " 1 file";
  if (verb !== "mixed") return `${verb} ${total} files`;
  return `${total} files changed`;
}

// computeStoredReviewSummary: the saved summaries of the group's results,
// which outlive their diffs, and whether any saved change deleted files.
export function storedSummary(calls) {
  let files = 0;
  let copied = 0;
  let checkouts = 0;
  let moved = 0;
  let generated = 0;
  let removed = 0;
  let deletions = false;
  let adds = 0;
  let dels = 0;
  let isCounted = false;
  let names = [];
  let complete = true;
  for (const call of calls) {
    const review = call.result?.review;
    if (!review) continue;
    deletions = deletions || Boolean(review.deletions);
    const summary = review.summary;
    if (!summary) continue;
    deletions = deletions || Boolean(summary.deletions);
    let paths = summary.paths ?? [];
    if (paths.length === 0) {
      const path = decode(call.input ?? "", { file_path: "string" }).value.file_path;
      if (path !== "") {
        if (hiddenReviewPath(path, "")) continue;
        paths = [path];
      }
    }
    const n = summary.files ?? 0;
    files += n;
    // The paths keep a few files at most; past that only counts are known.
    complete = complete && n <= paths.length + (summary.removed ?? 0);
    if (summary.counted && !summary.no_lines) {
      adds += summary.adds ?? 0;
      dels += summary.dels ?? 0;
      isCounted = true;
    }
    copied += summary.copied ?? 0;
    checkouts += summary.checkouts ?? 0;
    moved += summary.moved ?? 0;
    generated += summary.generated ?? 0;
    removed += summary.removed ?? 0;
    for (const path of paths) names = [...names.filter((x) => x !== path), path];
  }
  const transfers = copied + checkouts + moved + generated;
  let edited = files - transfers - removed;
  if (complete && names.length > 0) edited = Math.min(edited, names.length - transfers);
  const label = fileChangeLabel(
    { edited: Math.max(edited, 0), copied, "checked out": checkouts, moved, generated, removed },
    names.length === 1 ? baseName(names[0]) : "",
  );
  return { label, deletions, adds, dels, counted: isCounted };
}

// The review inputs of finished calls: [{call, result}].
export function reviewInputs(calls) {
  return calls.filter((c) => c.result).map((call) => ({ call, result: call.result }));
}

// The changes of the group's live (not yet summarized) reviews, kept per
// group until a call or result changes (Changes(), cached by key).
const resultIds = new WeakMap();
let nextResultId = 1;
const changesCache = new Map();

function resultId(result) {
  if (!resultIds.has(result)) resultIds.set(result, nextResultId++);
  return resultIds.get(result);
}

export function groupChanges(groupKey, calls) {
  const key = calls
    .filter((c) => c.result)
    .map((c) => `${c.id}:${resultId(c.result)}`)
    .join(";");
  const known = changesCache.get(groupKey);
  if (known && known.key === key) return known.files;
  const files = buildReviewChanges(reviewInputs(calls));
  changesCache.set(groupKey, { key, files });
  return files;
}

// liveChangeLabel: the captured changes, or the files the tools edited.
export function liveChangeLabel(changes, edited) {
  if (changes.length === 0) return fileChangeLabel({ edited: edited.length }, edited.length === 1 ? edited[0] : "");
  const kinds = {};
  const seen = new Set();
  let name = "";
  for (const change of changes) {
    if (seen.has(change.path)) continue;
    seen.add(change.path);
    const kind = changeKind(change);
    kinds[kind] = (kinds[kind] ?? 0) + 1;
    name = baseName(change.path);
  }
  return fileChangeLabel(kinds, name);
}

// changeSummary: the text after the count, {label, adds, dels} (adds and
// dels null when no counts show); null when nothing changed. While working
// (status words not empty) a live summary shows only its counts.
export function changeSummary({ status, changes, edited, stored }) {
  if (stored.label !== "") return { label: stored.label, adds: stored.counted ? stored.adds : null, dels: stored.counted ? stored.dels : null };
  if (stored.deletions && changes.length === 0) return { label: "Some files were deleted", adds: null, dels: null };
  if (changes.length === 0 && edited.length === 0) return null;
  const isCounted = anyCounted(changes) && changes.some(hasEdits);
  if (status !== "" && !isCounted) return null;
  const label = status === "" ? liveChangeLabel(changes, edited) : "";
  if (!isCounted) return { label, adds: null, dels: null };
  const { adds, dels } = stats(changes);
  return { label, adds, dels };
}

// The verbs of a run that changed no file (desktop-design 4.6), at most
// three, then "…": "read session.go, ran a command".
export function toolVerbs(calls) {
  const order = [];
  const groups = new Map();
  const note = (key, value) => {
    if (!groups.has(key)) {
      groups.set(key, []);
      order.push(key);
    }
    if (value !== undefined) groups.get(key).push(value);
  };
  for (const call of calls) {
    const name = String(call.name ?? "");
    const input = call.input ?? "";
    if (name === "view") note("read", decode(input, { file_path: "string" }).value.file_path);
    else if (name === "glob" || name === "grep" || name === "ls" || name === "sourcegraph") note("searched");
    else if (name === "bash") note("ran", call.id);
    else if (name === "fetch" || name === "download" || name === "web_fetch") note("fetched", hostOf(decode(input, { url: "string" }).value.url));
    else if (name === "web_search") note("web");
    else if (name === "todos") note("plan");
    else if (name === "agent") note("helper");
    else if (name.startsWith("lsp_")) note("code");
    else if (name.startsWith("mcp_")) {
      const rest = name.slice(4);
      const cut = rest.indexOf("_");
      if (cut >= 0) note("used:" + rest.slice(0, cut));
    }
  }
  const words = order.map((key) => {
    const values = groups.get(key);
    const distinct = [...new Set(values.filter((v) => v !== ""))];
    switch (key) {
      case "read":
        return distinct.length === 1 ? `read ${baseName(distinct[0])}` : `read ${distinct.length || values.length} files`;
      case "searched":
        return "searched";
      case "ran":
        return values.length === 1 ? "ran a command" : `ran ${values.length} commands`;
      case "fetched":
        return distinct.length === 1 ? `fetched ${distinct[0]}` : `fetched ${values.length} pages`;
      case "web":
        return "searched the web";
      case "plan":
        return "updated the plan";
      case "helper":
        return "asked a helper";
      case "code":
        return "checked code";
    }
    return "used " + key.slice("used:".length);
  });
  return words.length > 3 ? [...words.slice(0, 3), "…"].join(", ") : words.join(", ");
}

function hostOf(url) {
  try {
    return new URL(url).host;
  } catch {
    return "";
  }
}

// The extras after the summary (CHAT-082, toolgroup_header.go:42-47):
// " · N backgrounded", then " · N failed" (the failed count in --error-ink).
export function groupExtras(t) {
  const out = [];
  if (t.moved > 0) out.push({ text: `${t.moved} backgrounded`, tone: "" });
  if (t.failed > 0) out.push({ text: `${t.failed} failed`, tone: "error" });
  return out;
}

// Everything a group's row says (toolgroup_header.go header): the count, the
// mark, the change summary or (done, nothing changed) the verbs, the extras,
// and whether the Changes button shows (the terminal's clickable count:
// captured changes or a saved summary). status: the live words, "" when
// the group is not working.
export function groupRow({ groupKey, calls, marks = {}, status = "", working = false }) {
  const t = tally(calls, marks);
  const changes = groupChanges(groupKey, calls);
  const stored = storedSummary(calls);
  const summary = changeSummary({ status, changes, edited: t.edited, stored });
  return {
    count: countLabel(t.tools),
    mark: groupMark({ working: working || status !== "", calls, marks }),
    summary,
    verbs: summary || status !== "" ? "" : toolVerbs(calls),
    extras: groupExtras(t),
    hasChanges: changes.length > 0 || stored.label !== "",
  };
}

// headerIcon: the activity mark while the group works or a step still
// streams; else the last tool decides: a cross when it failed, a gear when
// it went on in the background, and no mark when done (CHAT-081).
export function groupMark({ working, calls, marks = {} }) {
  if (working) return "activity";
  for (const call of calls) if (toolStatus(call, marks) === "pending") return "activity";
  for (let i = calls.length - 1; i >= 0; i--) {
    const status = toolStatus(calls[i], marks);
    if (status === "error") return "cross";
    if (movedToBackground(calls[i].result, calls[i].name)) return "gear";
    break;
  }
  return null;
}
