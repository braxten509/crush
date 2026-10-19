// The text `c`/`y` copies from a tool or shell item (CHAT-044, CHAT-056).
// Ports of chat/tool_copy.go:16-543, tool_registry.go:44-221 and shell.go:179-181.
import { stripAnsi } from "../render/ansi.js";
import { generateDiff } from "../diff.js";
import { decode, decodeMap, extName, goDuration, goSprintV, humanize, prettyPath } from "./go.js";
import { COPY_NAMES, META, SHAPES } from "./registry.js";

export const TOOL_COPIED = "Tool content copied to clipboard";
export const SHELL_COPIED = "Shell output copied to clipboard";

// prettifyToolName: the heading name of a copy.
export function copyName(name) {
  return COPY_NAMES[name] || humanize(name);
}

// Formatters of decoded parameters (copyParamsAs): each runs only when the
// input decodes into the tool's shape.
const PARAMS = {
  bash: (p) => `**Command:** ${p.command.replaceAll("\n", " ").replaceAll("\t", "    ")}`,
  view: (p) =>
    [`**File:** ${prettyPath(p.file_path)}`, ...(p.limit > 0 ? [`**Limit:** ${p.limit}`] : []), ...(p.offset > 0 ? [`**Offset:** ${p.offset}`] : [])].join("\n"),
  write: (p) => `**File:** ${prettyPath(p.file_path)}`,
  edit: (p) => `**File:** ${prettyPath(p.file_path)}`,
  multiedit: (p) => [`**File:** ${prettyPath(p.file_path)}`, `**Edits:** ${p.edits.length}`].join("\n"),
  glob: (p) => [`**Pattern:** ${p.pattern}`, ...(p.path ? [`**Path:** ${p.path}`] : [])].join("\n"),
  grep: (p) =>
    [
      `**Pattern:** ${p.pattern}`,
      ...(p.path ? [`**Path:** ${p.path}`] : []),
      ...(p.include ? [`**Include:** ${p.include}`] : []),
      ...(p.literal_text ? ["**Literal:** true"] : []),
    ].join("\n"),
  ls: (p) => `**Path:** ${prettyPath(p.path || ".")}`,
  download: (p) =>
    [`**URL:** ${p.url}`, `**File Path:** ${prettyPath(p.file_path)}`, ...(p.timeout > 0 ? [`**Timeout:** ${goDuration(p.timeout)}`] : [])].join("\n"),
  fetch: (p) => [`**URL:** ${p.url}`, ...(p.format ? [`**Format:** ${p.format}`] : []), ...(p.timeout > 0 ? [`**Timeout:** ${p.timeout}s`] : [])].join("\n"),
  sourcegraph: (p) =>
    [`**Query:** ${p.query}`, ...(p.count > 0 ? [`**Count:** ${p.count}`] : []), ...(p.context_window > 0 ? [`**Context:** ${p.context_window}`] : [])].join("\n"),
  agent: (p) => `**Task:**\n${p.prompt}`,
  agentic_fetch: (p) => [...(p.url ? [`**URL:** ${p.url}`] : []), ...(p.prompt ? [`**Prompt:** ${p.prompt}`] : [])].join("\n"),
  web_fetch: (p) => `**URL:** ${p.url}`,
};

// formatParametersForCopy: the tool's own formatter, else every decoded
// key as "**Key:** value" (keys in the input's order; Go's map order varies).
export function copyParams(name, input) {
  if (name === "lsp_diagnostics") return "**Project:** diagnostics";
  if (Object.hasOwn(PARAMS, name)) {
    const { value, ok } = decode(input, SHAPES[name]);
    if (ok) return PARAMS[name](value);
  }
  const { value: params, ok } = decodeMap(input);
  if (!ok) return "";
  return Object.entries(params)
    .map(([key, value]) => {
      let label = key.replaceAll("_", " ");
      // Go upper-cases the first byte only; a multi-byte first letter stays.
      if (label.length > 0 && label.charCodeAt(0) < 0x80) label = label[0].toUpperCase() + label.slice(1);
      return `**${label}:** ${goSprintV(value)}`;
    })
    .join("\n");
}

const LANGS = {
  ".go": "go",
  ".js": "javascript",
  ".mjs": "javascript",
  ".ts": "typescript",
  ".py": "python",
  ".rs": "rust",
  ".java": "java",
  ".c": "c",
  ".cpp": "cpp",
  ".cc": "cpp",
  ".cxx": "cpp",
  ".sh": "bash",
  ".bash": "bash",
  ".json": "json",
  ".yaml": "yaml",
  ".yml": "yaml",
  ".xml": "xml",
  ".html": "html",
  ".css": "css",
  ".md": "markdown",
};

const fence = (lang, text) => "```" + lang + "\n" + text + "\n```";
const codeBlock = (result) => fence("", result.content);

function editCopy(call, result, multi) {
  if (!result.metadata) return result.content;
  const meta = decode(result.metadata, multi ? META.multiedit : META.edit);
  if (!meta.ok) return result.content;
  const params = decode(call.input ?? "", SHAPES[multi ? "multiedit" : "edit"]).value;
  if (meta.value.old_content === "" && meta.value.new_content === "") return "";
  const file = params.file_path ? prettyPath(params.file_path) : "";
  const { diff, additions, removals } = generateDiff(meta.value.old_content, meta.value.new_content, file);
  return `Changes: +${additions} -${removals}\n` + fence("diff", diff);
}

// The copy formatters of successful text results.
const RESULTS = {
  bash: (call, result) => {
    const meta = decode(result.metadata ?? "", META.bash).value;
    const output = meta.output !== "" ? meta.output : result.content !== "no output" ? result.content : "";
    return output === "" ? "" : fence("bash", output);
  },
  view: (call, result) => {
    const meta = decode(result.metadata ?? "", META.view).value;
    if (meta.content === "") return result.content;
    const lang = meta.file_path ? (LANGS[extName(meta.file_path).toLowerCase()] ?? "") : "";
    return fence(lang, meta.content);
  },
  write: (call, result) => {
    const { value: params, ok } = decode(call.input ?? "", SHAPES.write);
    if (!ok) return result.content;
    const lang = params.file_path ? (LANGS[extName(params.file_path).toLowerCase()] ?? "") : "";
    return `File: ${prettyPath(params.file_path)}\n` + fence(lang, params.content);
  },
  edit: (call, result) => editCopy(call, result, false),
  multiedit: (call, result) => editCopy(call, result, true),
  glob: (call, result) => codeBlock(result),
  grep: (call, result) => codeBlock(result),
  ls: (call, result) => codeBlock(result),
  download: (call, result) => codeBlock(result),
  sourcegraph: (call, result) => codeBlock(result),
  lsp_diagnostics: (call, result) => codeBlock(result),
  todos: (call, result) => codeBlock(result),
  fetch: (call, result) => {
    const { value: p, ok } = decode(call.input ?? "", SHAPES.fetch);
    if (!ok) return result.content;
    let out = "";
    if (p.url) out += `URL: ${p.url}\n`;
    if (p.format) out += `Format: ${p.format}\n`;
    if (p.timeout > 0) out += `Timeout: ${p.timeout}s\n`;
    return out + "\n" + result.content;
  },
  agentic_fetch: (call, result) => {
    const { value: p, ok } = decode(call.input ?? "", SHAPES.agentic_fetch);
    if (!ok) return result.content;
    let out = "";
    if (p.url) out += `URL: ${p.url}\n`;
    if (p.prompt) out += `Prompt: ${p.prompt}\n\n`;
    return out + fence("markdown", result.content);
  },
  web_fetch: (call, result) => {
    const { value: p, ok } = decode(call.input ?? "", SHAPES.web_fetch);
    if (!ok) return result.content;
    return `URL: ${p.url}\n\n` + fence("markdown", result.content);
  },
  agent: (call, result) => (result.content !== "" ? fence("markdown", result.content) : ""),
};

// formatResultForCopy.
export function copyResult(call, result) {
  if (!result) return "";
  if (result.data) {
    const mime = result.mime_type ?? "";
    return mime.startsWith("image/") ? `[Image: ${mime}]` : `[Media: ${mime}]`;
  }
  const format = RESULTS[call.name];
  return format ? format(call, result) : result.content;
}

// formatToolForCopy: "## Name Tool Call", the parameters, then the error,
// the result, or the status ("Cancelled" for a stopped call, else "Pending...").
// status is the call's lib/tools/status.js toolStatus.
export function toolCopyText(call, status) {
  const parts = [`## ${copyName(call.name)} Tool Call`];
  if (call.input) {
    const params = copyParams(call.name, call.input);
    if (params !== "") parts.push("### Parameters:", params);
  }
  const result = call.result;
  if (result && result.tool_call_id !== "") {
    if (result.is_error) parts.push("### Error:", result.content);
    else {
      parts.push("### Result:");
      const content = copyResult(call, result);
      if (content !== "") parts.push(content);
    }
  } else if (status === "cancelled") parts.push("### Status:", "Cancelled");
  else parts.push("### Status:", "Pending...");
  return parts.join("\n\n");
}

// The shell item's copy: "$ command" and the output without escape codes.
export function shellCopyText(command, output) {
  return "$ " + command + "\n" + stripAnsi(output ?? "");
}
