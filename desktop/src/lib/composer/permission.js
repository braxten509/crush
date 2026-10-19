// The permission prompt's words and keys as pure rules (DLG-119 to DLG-140):
// the header lines, the content per tool and what each key does. A port of
// internal/ui/dialog/permissions/permissions.go and content.go. The params
// arrive as the engine's JSON (internal/agent/tools/*PermissionsParams).

export const TITLE = "Permission Required";

// The buttons in order (renderButtons, permissions.go:531-536): the key letter
// is the one underlined.
export const BUTTONS = [
  { label: "Allow", key: "A", underline: 0, action: "allow" },
  { label: "Allow for Session", key: "S", underline: 10, action: "allow_session" },
  { label: "Deny", key: "D", underline: 0, action: "deny" },
];

// The tools that show a diff (hasDiffView, permissions.go:313-319).
export const DIFF_TOOLS = new Set(["edit", "write", "multiedit", "lsp_replace_symbol"]);

// The tools that name their own file or folder, so no generic Path line
// (renderHeader, permissions.go:450-458).
const OWN_PATH = new Set(["edit", "write", "multiedit", "view", "lsp_replace_symbol", "download", "ls"]);
const FILE_LINE = new Set(["edit", "write", "multiedit", "view", "lsp_replace_symbol"]);

// The content box width (in mono characters) from which a diff starts split
// (splitModeMinWidth), and the size under which the diff fills the column
// (minWindowWidth, minWindowHeight; permissions.go:37-44).
export const SPLIT_MIN_COLUMNS = 140;
export const FULL_BELOW_COLUMNS = 77;
export const FULL_BELOW_LINES = 20;
export const X_STEP = 5;

// stringext.Capitalize (cases.Title): each word's first letter in capitals,
// the rest small.
function titleCase(text) {
  return text.replace(/[^\s]+/gu, (word) => {
    const [first, ...rest] = [...word];
    return first.toUpperCase() + rest.join("").toLowerCase();
  });
}

// prettyName (permissions.go:524-529).
export function prettyName(name) {
  return titleCase(name.replaceAll("_", " ").replaceAll("-", " "));
}

// renderToolName (permissions.go:508-522): MCP tools (mcp_<server>_<tool>) as
// "Server → Tool".
export function toolLabel(toolName) {
  if (toolName.startsWith("mcp_")) {
    const first = toolName.indexOf("_");
    const second = toolName.indexOf("_", first + 1);
    if (second > -1) return `${prettyName(toolName.slice(first + 1, second))} → ${prettyName(toolName.slice(second + 1))}`;
  }
  return toolName;
}

// home.Short (internal/home/home.go:34-45), the terminal's fsext.PrettyPath.
export function prettyPath(path, home) {
  const text = String(path ?? "");
  if (!home || !text.startsWith(home)) return text;
  if (text.length === home.length) return "~";
  if (text[home.length] !== "/") return text;
  return `~${text.slice(home.length).replace(/\/+/g, "/")}`;
}

const isObject = (value) => value !== null && typeof value === "object" && !Array.isArray(value);

// The key-value lines under the title (renderHeader, permissions.go:439-495).
export function headerLines(request, home = "") {
  const tool = request?.tool_name ?? "";
  const params = request?.params;
  const lines = [{ key: "Tool", value: toolLabel(tool) }];
  if (!OWN_PATH.has(tool)) lines.push({ key: "Path", value: prettyPath(request?.path, home) });
  if (!isObject(params)) return lines;
  if (tool === "bash") lines.push({ key: "Desc", value: params.description ?? "" });
  if (tool === "download") {
    lines.push({ key: "URL", value: params.url ?? "" });
    lines.push({ key: "File", value: prettyPath(params.file_path, home) });
  }
  if (FILE_LINE.has(tool) && params.file_path) lines.push({ key: "File", value: prettyPath(params.file_path, home) });
  if (tool === "ls") lines.push({ key: "Directory", value: prettyPath(params.path, home) });
  return lines;
}

// common.StripBashDisplayPrefix (internal/ui/common/bash.go:11-14).
export function stripBashPrefix(command, workingDir) {
  const prefix = `cd ${workingDir} && `;
  return command.startsWith(prefix) ? command.slice(prefix.length) : command;
}

// Go's json.MarshalIndent of a decoded value: two spaces, object keys sorted.
function sortedJson(value) {
  const sort = (v) => {
    if (Array.isArray(v)) return v.map(sort);
    if (isObject(v)) return Object.fromEntries(Object.keys(v).sort().map((k) => [k, sort(v[k])]));
    return v;
  };
  return JSON.stringify(sort(value), null, 2);
}

// The content under the header (renderContent, content.go:13-228):
//   {kind: "diff", file: {path, before, after}}    the diff tools (DLG-123)
//   {kind: "text", parts: [{text, lang?}], text}   every other tool; lang marks
//        the parts the terminal highlights (the bash command, the JSON);
//        text is the whole as the terminal shows it
//   null                                           nothing to show
export function contentOf(request, { workingDir = "", home = "" } = {}) {
  const tool = request?.tool_name ?? "";
  const params = request?.params;
  const one = (text, lang) => ({ kind: "text", parts: [lang ? { text, lang } : { text }], text });
  if (DIFF_TOOLS.has(tool)) {
    if (!isObject(params)) return null;
    return { kind: "diff", file: { path: prettyPath(params.file_path, home), before: params.old_content ?? "", after: params.new_content ?? "" } };
  }
  switch (tool) {
    case "bash":
      return isObject(params) ? one(stripBashPrefix(params.command ?? "", workingDir), "bash") : null;
    case "download": {
      if (!isObject(params)) return null;
      let text = `URL: ${params.url ?? ""}\nFile: ${prettyPath(params.file_path, home)}`;
      if (params.timeout > 0) text += `\nTimeout: ${params.timeout}s`;
      return one(text);
    }
    case "fetch":
      return isObject(params) ? one(params.url ?? "") : null;
    case "agentic_fetch":
      if (!isObject(params)) return null;
      return one(params.url ? `URL: ${params.url}\n\nPrompt: ${params.prompt ?? ""}` : `Prompt: ${params.prompt ?? ""}`);
    case "view": {
      if (!isObject(params)) return null;
      let text = `File: ${prettyPath(params.file_path, home)}`;
      if (params.offset > 0) text += `\nStarting from line: ${params.offset + 1}`;
      if (params.limit > 0 && params.limit !== 2000) text += `\nLines to read: ${params.limit}`;
      return one(text);
    }
    case "ls": {
      if (!isObject(params)) return null;
      let text = `Directory: ${prettyPath(params.path, home)}`;
      if (Array.isArray(params.ignore) && params.ignore.length > 0) text += `\nIgnore patterns: ${params.ignore.join(", ")}`;
      return one(text);
    }
    default:
      return defaultContent(request);
  }
}

// renderDefaultContent (content.go:185-228): the description (not for MCP
// tools), then the params as indented JSON, or as they are when they are not
// JSON; trimmed.
function defaultContent(request) {
  const parts = [];
  if (!String(request?.tool_name ?? "").startsWith("mcp_") && request?.description) parts.push({ text: request.description });
  const params = request?.params;
  if (params !== null && params !== undefined) {
    let json = null;
    let raw = "";
    if (typeof params === "string") {
      raw = params;
      try {
        json = sortedJson(JSON.parse(params));
      } catch {
        json = null;
      }
    } else {
      json = sortedJson(params);
    }
    if (json !== null) parts.push({ text: json, lang: "json" });
    else if (raw !== "") parts.push({ text: raw });
  }
  if (parts.length === 0) return null;
  for (let i = 1; i < parts.length; i += 1) parts[i] = { ...parts[i], text: `\n\n${parts[i].text}` };
  parts[0] = { ...parts[0], text: parts[0].text.replace(/^\s+/u, "") };
  const end = parts.length - 1;
  parts[end] = { ...parts[end], text: parts[end].text.replace(/\s+$/u, "") };
  const text = parts.map((part) => part.text).join("");
  if (text === "") return null;
  return { kind: "text", parts, text };
}

// Whether a diff starts split: options.tui.diff_mode when set, else the
// content box's width (Draw, permissions.go:392; dialogs.go:262-266).
export function startsSplit(diffMode, columns) {
  if (diffMode === "split") return true;
  if (diffMode === "unified") return false;
  return columns >= SPLIT_MIN_COLUMNS;
}

// The diff fills the middle column when F is on, or when the slot would be
// under 77 characters wide or 20 lines tall (dialogSize, permissions.go:344-359).
export function forcedFull(columns, lines) {
  return columns <= FULL_BELOW_COLUMNS || lines <= FULL_BELOW_LINES;
}

// What a key does in the prompt (HandleMsg, permissions.go:225-293), from
// keys.js's combo spelling. state: {selected, diff}. Returns
//   {answer: action} | {selected} | {toggleSplit} | {toggleFull}
//   | {scrollY: ±1} | {scrollX: ±5 (diff) or ±1 (text)} | {} (taken, nothing
//   happens: PgUp, PgDn and the rest) | null (not the prompt's key)
export function promptKey(combo, { selected, diff }) {
  switch (combo) {
    case "Escape":
    case "d":
    case "D":
      return { answer: "deny" };
    case "ArrowRight":
    case "l":
    case "Tab":
      return { selected: (selected + 1) % 3 };
    case "ArrowLeft":
    case "h":
      return { selected: (selected + 2) % 3 };
    case "Enter":
    case "Ctrl+Y":
      return { answer: BUTTONS[selected].action };
    case "a":
    case "A":
    case "Ctrl+A":
      return { answer: "allow" };
    case "s":
    case "S":
    case "Ctrl+S":
      return { answer: "allow_session" };
    case "t":
      return diff ? { toggleSplit: true } : {};
    case "f":
      return diff ? { toggleFull: true } : {};
    case "Shift+ArrowDown":
    case "J":
      return { scrollY: 1 };
    case "Shift+ArrowUp":
    case "K":
      return { scrollY: -1 };
    case "Shift+ArrowLeft":
    case "H":
      return { scrollX: diff ? -X_STEP : -1 };
    case "Shift+ArrowRight":
    case "L":
      return { scrollX: diff ? X_STEP : 1 };
    case "PageUp":
    case "PageDown":
    case "Home":
    case "End":
    case "ArrowUp":
    case "ArrowDown":
    case "Space":
      return {};
    default:
      return null;
  }
}

// The hint line (4.9's words), with only the keys that apply: scrolling when
// the content scrolls or is a diff, t and f for diffs (ShortHelp,
// permissions.go:567-588).
export function hints({ diff, scrollable }) {
  const list = ["a allow", "s allow for session", "d deny"];
  if (scrollable || diff) list.push("shift+↑↓ scroll");
  if (diff) list.push("t split/unified", "f full screen");
  return list.join(" · ");
}
