// What each tool's row shows (CHAT-046 to CHAT-079): the name, the header's
// parameters and the body parts, decided as the terminal's tool items decide
// them (chat/tool_registry.go and one file per tool: bash.go, file.go,
// search.go, fetch.go, diagnostics.go, references.go, definition.go,
// rename.go, replace_symbol.go, call_hierarchy.go, symbols.go,
// lsp_restart.go, todos.go, question.go, agent.go, mcp.go, docker_mcp.go,
// generic.go). Components draw the result; nothing here touches the page.
//
// toolView(call, {status, workingDir, compact}) → {
//   name      {text} | {server, tool} | {docker, action, tone}
//   pending   the input still streams: the name and the activity mark only
//   invalid   "" or the error the whole row shows ("Invalid parameters",
//             "Invalid tool name") in place of the header
//   main      the main parameter; mainLang "bash" when it is a command
//   pairs     [[key, value]] shown as "(key=value, …)" when the room allows
//   note      text after the main parameter in slate (To-Do)
//   job       {action, pid, description} for background job rows
//   wrap      the main parameter wraps in full (a standalone Bash command)
//   wide      the body takes the timeline's full width (Edit, Multi-Edit,
//             Replace Symbol diffs)
//   body      [part]: what shows under the header (none while compact)
//   agent     {tag, prompt} for Agent and Agentic Fetch
// }
// Body parts: {type: "early"|"error"|"plain"|"code"|"image"|"skill"|"diff"|
// "note"|"markdown"|"todos"|"started"|"questions"|"servers"|"empty"}.
import { base64Size, collapseSpace, decode, decodeMap, formatSize, goMarshal, humanize, prettyPath, zeroOf } from "./go.js";
import { earlyState, errorLine, movedToBackground } from "./status.js";
import { sniffBody, unifiedBody } from "./sniff.js";
import { TODOS_PARAMS, todosView } from "./todos.js";
import { QUESTION_PARAMS, questionBody, questionSummary } from "./question.js";
import { DOCKER_PREFIX, dockerAction, dockerCompactName, dockerParams, isDockerTool, serversBody } from "./docker.js";

const EDIT_OP = { old_string: "string", new_string: "string", replace_all: "bool" };

// The parameter shapes of the built-in tools (agent/tools/*.go Params).
export const SHAPES = {
  bash: { description: "string", command: "string", working_dir: "string", run_in_background: "bool", auto_background_after: "int" },
  job_output: { shell_id: "string", wait: "bool" },
  job_kill: { shell_id: "string" },
  view: { file_path: "string", offset: "int", limit: "int" },
  write: { file_path: "string", content: "string" },
  edit: { file_path: "string", old_string: "string", new_string: "string", replace_all: "bool" },
  multiedit: { file_path: "string", edits: ["array", EDIT_OP] },
  glob: { pattern: "string", path: "string" },
  grep: { pattern: "string", path: "string", include: "string", literal_text: "bool" },
  ls: { path: "string", ignore: ["array", "string"], depth: "int" },
  download: { url: "string", file_path: "string", timeout: "int" },
  fetch: { url: "string", format: "string", timeout: "int" },
  web_fetch: { url: "string" },
  web_search: { query: "string", max_results: "int" },
  sourcegraph: { query: "string", count: "int", context_window: "int", timeout: "int" },
  lsp_diagnostics: { file_path: "string" },
  lsp_references: { symbol: "string", path: "string" },
  lsp_definition: { symbol: "string", path: "string" },
  lsp_rename: { symbol: "string", new_name: "string", path: "string" },
  lsp_replace_symbol: { symbol: "string", file_path: "string", replacement: "string", action: "string" },
  lsp_call_hierarchy: { symbol: "string", direction: "string", path: "string" },
  lsp_symbols: { file_path: "string" },
  lsp_restart: { name: "string" },
  agent: { prompt: "string" },
  agentic_fetch: { url: "string", prompt: "string" },
  todos: TODOS_PARAMS,
  question: QUESTION_PARAMS,
};

export const META = {
  bash: { start_time: "int", end_time: "int", output: "string", description: "string", working_directory: "string", background: "bool", shell_id: "string" },
  job: { shell_id: "string", command: "string", description: "string", done: "bool", working_directory: "string" },
  view: { file_path: "string", content: "string", resource_type: "string", resource_name: "string", resource_description: "string" },
  write: { diff: "string", additions: "int", removals: "int" },
  edit: { additions: "int", removals: "int", old_content: "string", new_content: "string" },
  multiedit: {
    additions: "int",
    removals: "int",
    old_content: "string",
    new_content: "string",
    edits_applied: "int",
    edits_failed: ["array", { index: "int", error: "string", edit: EDIT_OP }],
  },
  definition: { file_path: "string", line: "int", content: "string" },
  replace: { file_path: "string", old_content: "string", new_content: "string", action: "string" },
};

// The heading name of the clipboard copy (toolSpecs copyName); "" means the
// humanized tool name.
export const COPY_NAMES = {
  bash: "Bash",
  job_output: "Job: Output",
  job_kill: "Job: Kill",
  view: "View",
  write: "Write",
  edit: "Edit",
  multiedit: "Multi-Edit",
  glob: "Glob",
  grep: "Grep",
  ls: "List",
  download: "Download",
  fetch: "Fetch",
  sourcegraph: "Sourcegraph",
  agent: "Agent",
  agentic_fetch: "Agentic Fetch",
  web_fetch: "Fetch",
  web_search: "Search",
  todos: "To-Do",
};

// Tools that never fold into a group (toolgroup.go Foldable).
export const UNFOLDED = new Set(["question", "agent", "agentic_fetch"]);

// The tools drawn with the standard layout (tool_standard.go), with their
// header and body rules.
const fmtTimeout = (t) => (t === 0 ? "" : `${t}s`);
const nonZero = (n) => (n === 0 ? "" : String(n));
const plain = (result) => ({ type: "plain", text: result.content });

const STANDARD = {
  download: {
    name: "Download",
    header: (p) => [p.url, ...(p.file_path ? ["file_path", prettyPath(p.file_path)] : []), ...(p.timeout !== 0 ? ["timeout", fmtTimeout(p.timeout)] : [])],
    body: (r) => plain(r),
  },
  fetch: {
    name: "Fetch",
    header: (p) => [p.url, ...(p.format ? ["format", p.format] : []), ...(p.timeout !== 0 ? ["timeout", fmtTimeout(p.timeout)] : [])],
    body: (r, p) => ({ type: "code", path: p.format === "text" ? "fetch.txt" : p.format === "html" ? "fetch.html" : "fetch.md", text: r.content, offset: 0 }),
  },
  web_fetch: { name: "Fetch", header: (p) => [p.url], body: (r) => ({ type: "markdown", text: r.content }) },
  web_search: { name: "Search", header: (p) => [p.query], body: (r) => ({ type: "markdown", text: r.content }) },
  glob: { name: "Glob", header: (p) => [p.pattern, ...(p.path ? ["path", p.path] : [])], body: (r) => plain(r) },
  grep: {
    name: "Grep",
    header: (p) => [p.pattern, ...(p.path ? ["path", p.path] : []), ...(p.include ? ["include", p.include] : []), ...(p.literal_text ? ["literal", "true"] : [])],
    body: (r) => plain(r),
  },
  ls: { name: "List", header: (p) => [prettyPath(p.path || ".")], body: (r) => plain(r) },
  sourcegraph: {
    name: "Sourcegraph",
    header: (p) => [p.query, ...(p.count !== 0 ? ["count", nonZero(p.count)] : []), ...(p.context_window !== 0 ? ["context", nonZero(p.context_window)] : [])],
    body: (r) => plain(r),
  },
  lsp_diagnostics: { name: "Diagnostics", lenient: true, header: (p) => [p.file_path ? prettyPath(p.file_path) : "project"], body: (r) => plain(r) },
  lsp_references: { name: "Find References", lenient: true, header: (p) => [p.symbol, ...(p.path ? ["path", prettyPath(p.path)] : [])], body: (r) => plain(r) },
  lsp_definition: {
    name: "Find Definition",
    lenient: true,
    header: (p) => [p.symbol],
    body: (r) => {
      const { value: meta, ok } = decode(r.metadata ?? "", META.definition);
      return ok && meta.content !== "" ? { type: "code", path: meta.file_path, text: meta.content, offset: 0 } : plain(r);
    },
  },
  lsp_rename: { name: "Rename Symbol", lenient: true, header: (p) => [p.symbol + " → " + p.new_name, ...(p.path ? ["path", prettyPath(p.path)] : [])], body: (r) => plain(r) },
  // The direction is a second parameter without a key, so the header drops it (call_hierarchy.go:38-44).
  lsp_call_hierarchy: { name: "Call Hierarchy", lenient: true, header: (p) => [p.symbol, p.direction === "outgoing" ? "outgoing" : "incoming"], body: (r) => plain(r) },
  lsp_symbols: { name: "List Symbols", lenient: true, header: (p) => [p.file_path], body: (r, p) => ({ type: "code", path: p.file_path, text: r.content, offset: 0 }) },
  lsp_restart: { name: "Restart LSP", lenient: true, header: (p) => (p.name ? [p.name] : []), body: (r) => plain(r) },
};

// The main parameter and the key=value pairs of a parameter list (the
// first entry, then key and value pairs; a pair with an empty value is
// skipped, and a key without a value is dropped).
export function splitParams(params) {
  const list = params ?? [];
  const pairs = [];
  for (let i = 1; i + 1 < list.length; i += 2) if (list[i + 1] !== "") pairs.push([list[i], list[i + 1]]);
  return { main: list[0] ?? "", pairs };
}

// toolParamList: the pairs show only when 30 or more cells stay for the main
// parameter after them ("(…)" and its space take 3).
export const MIN_MAIN_ROOM = 30;
export function pairsText(pairs) {
  return pairs.map(([k, v]) => `${k}=${v}`).join(", ");
}
export function pairsFit(pairs, width, measure = (s) => [...s].length) {
  if (!pairs.length) return false;
  return width - measure(pairsText(pairs)) - 3 >= MIN_MAIN_ROOM;
}

function base(name) {
  return { name: { text: name }, pending: false, invalid: "", main: "", mainLang: "", pairs: [], note: "", job: null, wrap: false, wide: false, body: [], agent: null };
}

function withParams(view, params) {
  const { main, pairs } = splitParams(params);
  view.main = main;
  view.pairs = pairs;
  return view;
}

// The display name of a tool while its input streams (pendingTool).
export function pendingName(call) {
  return toolView({ ...call, result: null }, { status: "pending" }).name;
}

// The view of one call. status is lib/tools/status.js toolStatus(call, marks).
// A compact (nested) header runs its parameters together on one line
// (toolParamList's strings.Fields join).
export function toolView(call, options = {}) {
  const view = viewOf(call, options);
  if (options.compact) {
    view.main = collapseSpace(view.main);
    view.pairs = view.pairs.map(([k, v]) => [k, collapseSpace(v)]);
  }
  return view;
}

function viewOf(call, { status = "running", workingDir = "", compact = false }) {
  const name = String(call?.name ?? "");
  const input = String(call?.input ?? "");
  const result = call?.result ?? null;

  if (name === "bash") return bashView(input, result, status, workingDir, compact);
  if (name === "job_output" || name === "job_kill") return jobView(name, input, result, status, compact);
  if (name === "view") return viewView(input, result, status, compact);
  if (name === "write") return writeView(input, result, status, compact);
  if (name === "edit" || name === "multiedit") return editView(name, input, result, status, compact);
  if (name === "lsp_replace_symbol") return replaceView(input, result, status, compact);
  if (name === "todos") return todosToolView(input, result, status, compact);
  if (name === "question") return questionView(input, result, status, compact);
  if (name === "agent" || name === "agentic_fetch") return agentView(name, input, result, status, compact, call);
  if (Object.hasOwn(STANDARD, name)) return standardView(STANDARD[name], name, input, result, status, compact);
  if (isDockerTool(name)) return dockerView(name, input, result, status, compact);
  if (name.startsWith("mcp_")) return mcpView(name, input, result, status, compact);
  return genericView(name, input, result, status, compact);
}

// The early state, then the body when the result has content
// (tool_standard.go render).
function standardView(spec, name, input, result, status, compact) {
  const view = base(spec.name);
  if (status === "pending") return { ...view, pending: true };
  const { value: params, ok } = decode(input, SHAPES[name]);
  if (!ok && !spec.lenient) return { ...view, invalid: "Invalid parameters" };
  withParams(view, spec.header(params));
  if (compact) return view;
  const early = earlyState(status, result);
  if (early) return { ...view, body: [early] };
  if (!result || result.content === "") return view;
  view.body = [spec.body(result, params)];
  return view;
}

function bashView(input, result, status, workingDir, compact) {
  const view = base("Bash");
  if (status === "pending") return { ...view, pending: true };
  const decoded = decode(input, SHAPES.bash);
  const params = decoded.value;
  if (!decoded.ok) params.command = "failed to parse command";
  const meta = result ? decode(result.metadata ?? "", META.bash).value : zeroOf(META.bash);
  if (meta.background && meta.shell_id !== "") {
    const content = "Command: " + params.command + "\n" + (result?.content ?? "");
    return jobRows(view, "Start", meta.shell_id, meta.description || params.command, content, status, result, compact);
  }
  let command = params.command.replaceAll("\t", "    ");
  const prefix = `cd ${workingDir} && `;
  if (command.startsWith(prefix)) command = command.slice(prefix.length);
  withParams(view, [command, ...(params.run_in_background || movedToBackground(result, "bash") ? ["background", "true"] : [])]);
  view.mainLang = "bash";
  view.wrap = !compact;
  if (compact) return view;
  const early = earlyState(status, result);
  if (early) return { ...view, body: [early] };
  if (!result) return view;
  const output = meta.output !== "" ? meta.output : result.content !== "no output" ? result.content : "";
  if (output === "") return view;
  view.body = [{ type: "plain", text: output }];
  return view;
}

function jobView(name, input, result, status, compact) {
  const view = base("Job");
  if (status === "pending") return { ...view, pending: true };
  const { value: params, ok } = decode(input, SHAPES[name]);
  if (!ok) return { ...view, invalid: "Invalid parameters" };
  let description = "";
  if (result && result.metadata) {
    const meta = decode(result.metadata, META.job);
    if (meta.ok) description = meta.value.description || meta.value.command;
  }
  return jobRows(view, name === "job_output" ? "Output" : "Kill", params.shell_id, description, result?.content ?? "", status, result, compact);
}

// renderJobTool: "Job (Action) PID id description", then the early state or
// the content.
function jobRows(view, action, pid, description, content, status, result, compact) {
  view.name = { text: "Job" };
  view.job = { action, pid, description };
  if (compact) return view;
  const early = earlyState(status, result);
  if (early) return { ...view, body: [early] };
  if (content === "") return view;
  view.body = [{ type: "plain", text: content }];
  return view;
}

function imagePart(result) {
  return { type: "image", data: result.data, mime: result.mime_type, size: formatSize(base64Size(result.data)) };
}

function viewView(input, result, status, compact) {
  const view = base("View");
  if (status === "pending") return { ...view, pending: true };
  const { value: params, ok } = decode(input, SHAPES.view);
  if (!ok) return { ...view, invalid: "Invalid parameters" };
  withParams(view, [
    prettyPath(params.file_path),
    ...(params.limit !== 0 ? ["limit", String(params.limit)] : []),
    ...(params.offset !== 0 ? ["offset", String(params.offset)] : []),
  ]);
  if (compact) return view;
  const early = earlyState(status, result);
  if (early) return { ...view, body: [early] };
  if (!result) return view;
  if (result.data && String(result.mime_type ?? "").startsWith("image/")) return { ...view, body: [imagePart(result)] };
  const meta = decode(result.metadata ?? "", META.view);
  const content = meta.ok && meta.value.content !== "" ? meta.value.content : result.content;
  if (meta.value.resource_type === "skill") {
    return { ...view, body: [{ type: "skill", name: meta.value.resource_name, description: meta.value.resource_description }] };
  }
  if (content === "") return view;
  view.body = [{ type: "code", path: params.file_path, text: content, offset: params.offset }];
  return view;
}

function writeView(input, result, status, compact) {
  const view = base("Write");
  if (status === "pending") return { ...view, pending: true };
  const { value: params, ok } = decode(input, SHAPES.write);
  if (!ok) return { ...view, invalid: "Invalid parameters" };
  withParams(view, [prettyPath(params.file_path)]);
  if (compact) return view;
  if (!result) {
    const early = earlyState(status, result);
    return early ? { ...view, body: [early] } : view;
  }
  if (result.is_error) {
    const meta = decode(result.metadata ?? "", META.write);
    if (meta.ok && meta.value.diff !== "") return { ...view, body: [errorLine(result.content), unifiedBody(meta.value.diff)] };
    return { ...view, body: [errorLine(result.content)] };
  }
  if (params.content !== "") view.body = [{ type: "code", path: params.file_path, text: params.content, offset: 0 }];
  return view;
}

function editView(name, input, result, status, compact) {
  const multi = name === "multiedit";
  const view = base(multi ? "Multi-Edit" : "Edit");
  view.wide = true;
  if (status === "pending") return { ...view, pending: true };
  const { value: params, ok } = decode(input, SHAPES[name]);
  if (!ok) return { ...view, invalid: "Invalid parameters" };
  const file = prettyPath(params.file_path);
  withParams(view, [file, ...(multi && params.edits.length > 0 ? ["edits", String(params.edits.length)] : [])]);
  if (compact) return view;
  if (!result) {
    const early = earlyState(status, result);
    return early ? { ...view, body: [early] } : view;
  }
  const meta = decode(result.metadata ?? "", multi ? META.multiedit : META.edit);
  if (!meta.ok) return { ...view, body: [{ type: "plain", text: result.content }] };
  const m = meta.value;
  const diff = { type: "diff", files: [{ path: file, before: m.old_content, after: m.new_content }], cutAbove: multi };
  const parts = [diff];
  if (multi && m.edits_failed.length > 0) parts.push({ type: "note", tag: "Note", text: `${m.edits_applied} of ${params.edits.length} edits succeeded` });
  if (result.is_error) {
    if (m.old_content === "" && m.new_content === "") return { ...view, body: [errorLine(result.content)] };
    return { ...view, body: [errorLine(result.content), ...parts] };
  }
  view.body = parts;
  return view;
}

function replaceView(input, result, status, compact) {
  const view = base("Replace Symbol");
  view.wide = true;
  if (status === "pending") return { ...view, pending: true };
  const params = decode(input, SHAPES.lsp_replace_symbol).value;
  const file = prettyPath(params.file_path);
  // The file is a second parameter without a key, so the header drops it.
  withParams(view, [params.symbol, file]);
  if (compact) return view;
  const early = earlyState(status, result);
  if (early) return { ...view, body: [early] };
  if (!result) return view;
  const meta = decode(result.metadata ?? "", META.replace);
  // Go's (err == nil && old != "") || new != "" (replace_symbol.go:58).
  if ((meta.ok && meta.value.old_content !== "") || meta.value.new_content !== "") {
    const diff = { type: "diff", files: [{ path: file, before: meta.value.old_content, after: meta.value.new_content }], cutAbove: false };
    view.body = result.is_error ? [errorLine(result.content), diff] : [diff];
    return view;
  }
  view.body = [{ type: "plain", text: result.content }];
  return view;
}

function todosToolView(input, result, status, compact) {
  const view = base("To-Do");
  if (status === "pending") return { ...view, pending: true };
  const todos = todosView(input, result);
  view.main = todos.main;
  view.note = todos.note;
  view.ratio = todos.ratio;
  if (compact) return view;
  const early = earlyState(status, result);
  if (early) return { ...view, body: [early] };
  if (todos.body) view.body = [todos.body];
  return view;
}

function questionView(input, result, status, compact) {
  const view = base("Question");
  if (status === "pending") return { ...view, pending: true };
  const { value: params, ok } = decode(input, SHAPES.question);
  if (!ok) return { ...view, invalid: "Invalid parameters" };
  view.main = questionSummary(params.questions);
  if (compact) return view;
  const early = earlyState(status, result);
  if (early) return { ...view, body: [early] };
  if (!result || result.content === "") return view;
  const body = questionBody(result.content);
  if (body) view.body = [body];
  return view;
}

// Agent and Agentic Fetch: no early states; the task, the nested tools, the
// activity mark until a result, then the result as Markdown (agent.go).
function agentView(name, input, result, status, compact, call) {
  const fetch = name === "agentic_fetch";
  const view = base(fetch ? "Agentic Fetch" : "Agent");
  const children = call?.children ?? [];
  if (status === "pending" && children.length === 0) return { ...view, pending: true };
  const params = decode(input, SHAPES[name]).value;
  if (fetch && params.url) view.main = params.url;
  if (compact) return view;
  view.agent = { tag: fetch ? "Prompt" : "Task", prompt: params.prompt, running: !result && status !== "cancelled" };
  if (result && result.content !== "") view.body = [{ type: "markdown", text: result.content }];
  return view;
}

function mcpView(name, input, result, status, compact) {
  const parts = splitN(name, "_", 3);
  const view = base("");
  if (parts.length !== 3) return { ...view, invalid: "Invalid tool name" };
  view.name = { server: humanize(parts[1]), tool: humanize(parts[2]) };
  if (status === "pending") return { ...view, pending: true };
  const { value: params, ok } = decodeMap(input);
  if (!ok) return { ...view, invalid: "Invalid parameters" };
  if (Object.keys(params).length > 0) view.main = goMarshal(params);
  if (compact) return view;
  const early = earlyState(status, result);
  if (early) return { ...view, body: [early] };
  if (!result || result.content === "") return view;
  view.body = [sniffBody(result.content)];
  return view;
}

function dockerView(name, input, result, status, compact) {
  const tool = name.slice(DOCKER_PREFIX.length);
  const { action, tone } = dockerAction(tool);
  const view = base("");
  view.name = { docker: "Docker MCP", action, tone };
  if (status === "pending") return { ...view, pending: true };
  withParams(view, dockerParams(tool, input));
  if (compact) {
    // The nested header is "Docker MCP: action" with a done mark, whatever the status.
    view.name = { text: dockerCompactName(tool) };
    view.forceDone = true;
    return view;
  }
  const early = earlyState(status, result);
  if (early) return { ...view, body: [early] };
  if (tool === "mcp-find") {
    const servers = result && result.content ? serversBody(result.content) : null;
    view.body = servers ? [servers] : [];
    return view;
  }
  if (!result) return view;
  const parts = [];
  if (result.content) parts.push(sniffBody(result.content));
  if (result.data && String(result.mime_type ?? "").startsWith("image/")) parts.push(imagePart(result));
  view.body = parts;
  return view;
}

function genericView(name, input, result, status, compact) {
  const view = base(humanize(name));
  if (status === "pending") return { ...view, pending: true };
  const { value: params, ok } = decodeMap(input);
  if (!ok) return { ...view, invalid: "Invalid parameters" };
  if (Object.keys(params).length > 0) view.main = goMarshal(params);
  if (compact) return view;
  const early = earlyState(status, result);
  if (early) return { ...view, body: [early] };
  // A result without text shows nothing, even a picture (generic.go:60-69).
  if (!result || result.content === "") return view;
  if (result.data && String(result.mime_type ?? "").startsWith("image/")) return { ...view, body: [imagePart(result)] };
  view.body = [sniffBody(result.content)];
  return view;
}

// strings.SplitN(s, sep, n).
function splitN(s, sep, n) {
  const out = [];
  let rest = s;
  while (out.length < n - 1) {
    const at = rest.indexOf(sep);
    if (at < 0) break;
    out.push(rest.slice(0, at));
    rest = rest.slice(at + sep.length);
  }
  out.push(rest);
  return out;
}

// The "+N −N" at a tool line's right end (desktop-design 4.6): an edit,
// multi-edit or write that succeeded, from its metadata; null otherwise.
export function lineCounts(call) {
  const result = call?.result;
  if (!result || result.is_error) return null;
  const shape = { edit: META.edit, multiedit: META.multiedit, write: META.write }[call.name];
  if (!shape) return null;
  const { value, ok } = decode(result.metadata ?? "", shape);
  if (!ok || (value.additions === 0 && value.removals === 0)) return null;
  return { adds: value.additions, dels: value.removals };
}

// The name a tool's line shows, as plain text ("Bash", "Github → Issues",
// "Docker MCP → Find").
export function nameText(name) {
  if (name.server !== undefined) return `${name.server} → ${name.tool}`;
  if (name.docker !== undefined) return `${name.docker} → ${name.action}`;
  return name.text;
}
