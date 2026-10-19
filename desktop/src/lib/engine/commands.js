// One async function per Tauri command (desktop-design 1.3). Each is named
// exactly as its command in src-tauri/src/commands.rs, so the two lists can be
// compared line by line (and `openLink` stays free for lib/render/links.js).
// Arguments are the JS names of 1.3; Tauri maps them to the Rust names. Every
// failure rejects with an AppError object {code, message, field?, status?}.
import { invoke } from "@tauri-apps/api/core";

// The last text the app put on the clipboard, for the probe's clipboard.last (1.9).
let lastClipboard = null;
export function lastClipboardWrite() {
  return lastClipboard;
}

// app_info gives the window's launch ID once (6.4) and many parts call it, so
// the first ID seen is kept here for whichever part takes it (SHARE, 7.3).
let launchId = null;
export async function app_info() {
  const info = await invoke("app_info");
  if (info?.launch_id) launchId = info.launch_id;
  return info;
}

// The window's launch ID, once; null when there was none or it was taken.
export async function takeLaunchId() {
  if (launchId === null) await app_info().catch(() => null);
  const id = launchId;
  launchId = null;
  return id;
}

export async function probe_write(json) {
  return invoke("probe_write", { json });
}

// Probe mode: {request, opened} (1.9); null otherwise.
export async function probe_request() {
  return invoke("probe_request");
}

export async function engine_open(cwd) {
  return invoke("engine_open", { cwd });
}

export async function engine_retry() {
  return invoke("engine_retry");
}

// "" clears it (the landing page).
export async function engine_set_current_session(sessionId) {
  return invoke("engine_set_current_session", { sessionId });
}

export async function events_take() {
  return invoke("events_take");
}

// method: "GET" | "POST" | "PUT" | "DELETE"; path relative to the window's
// workspace ("sessions") or absolute ("/v1/limits"); query and body optional.
// A call with a callId has no time limit and can be stopped with api_abort (1.2.4).
export async function api(method, path, query, body, callId) {
  return invoke("api", { method, path, query, body, callId });
}

// Stops the api call with this callId; false when it already ended.
export async function api_abort(callId) {
  return invoke("api_abort", { callId });
}

export async function settings_get() {
  return invoke("settings_get");
}

// key: "crush_path" | "yolo".
export async function settings_set(key, value) {
  return invoke("settings_set", { key, value });
}

export async function closed_set(project, sessionId, closed) {
  return invoke("closed_set", { project, sessionId, closed });
}

export async function clipboard_write(text) {
  const result = await invoke("clipboard_write", { text });
  lastClipboard = { text, at: Date.now() };
  return result;
}

export async function clipboard_read() {
  return invoke("clipboard_read");
}

// Only after a click (1.7).
export async function open_link(url, cwd) {
  return invoke("open_link", { url, cwd });
}

export async function file_info(path, cwd) {
  return invoke("file_info", { path, cwd });
}

export async function file_read(path, cwd, maxBytes) {
  return invoke("file_read", { path, cwd, maxBytes });
}

export async function files_list(cwd, maxDepth, maxItems) {
  return invoke("files_list", { cwd, maxDepth, maxItems });
}

// -- Phases 5 and 6 (desktop-design-5-6 6.4; scaffold S5) --
// Named in camelCase, as 7.1 pins them. A command whose owner has not built
// it yet rejects with {code: "not_built"}. Streams take a Channel
// (@tauri-apps/api/core), never window events.

// A POST that carries a secret, only to secure-forms/{fid}/submit,
// config/provider-key and auth/verify-key (else "secret_route"); the value
// never passes through api, a log or the probe.
export async function apiSecret(path, body) {
  return invoke("api_secret", { path, body });
}

// → {label, created}: focuses the window that shows projectDir, else opens one.
export async function windowOpen(projectDir, sessionId, newChat, launchId) {
  return invoke("window_open", { projectDir, sessionId, newChat, launchId });
}

// After the window's close steps (lib/closing.js); the last window ends the app.
export async function windowClose() {
  return invoke("window_close");
}

// Leaves this window's engine and records the new folder; the page then reloads.
export async function projectSwitch(projectDir) {
  return invoke("project_switch", { projectDir });
}

// onData: a Channel that gets {data: <base64>} batches, then {exit: {code, signal}}. → {id, pid}
export async function ptySpawn(cwd, argv, cols, rows, onData) {
  return invoke("pty_spawn", { cwd, argv, cols, rows, onData });
}

export async function ptyWrite(id, data) {
  return invoke("pty_write", { id, data });
}

export async function ptyResize(id, cols, rows) {
  return invoke("pty_resize", { id, cols, rows });
}

export async function ptyCwd(id) {
  return invoke("pty_cwd", { id });
}

export async function ptyClose(id) {
  return invoke("pty_close", { id });
}

export async function ptyCloseAll() {
  return invoke("pty_close_all");
}

// → {path, argv}
export async function editorPrepare(text, line, column) {
  return invoke("editor_prepare", { text, line, column });
}

// → {text}
export async function editorFinish(path) {
  return invoke("editor_finish", { path });
}

// → {sent, error?}
export async function alertNotify(title, body) {
  return invoke("alert_notify", { title, body });
}

// → {player} (null when none worked)
export async function alertSound(id) {
  return invoke("alert_sound", { id });
}

// state: "idle" | "working" | "blocked" | "done" | "error".
export async function alertWindowState(state, projectDir, sessionId, focused) {
  return invoke("alert_window_state", { state, projectDir, sessionId, focused });
}

// → the label of the window that handles that chat's alert.
export async function alertHandler(projectDir, sessionId) {
  return invoke("alert_handler", { projectDir, sessionId });
}

// → {path, kind: "panic" | "webview"} or null.
export async function crashPending() {
  return invoke("crash_pending");
}

export async function crashRestart() {
  return invoke("crash_restart");
}

// → {lines, truncated, missing, path, end}
export async function logsTail(lines) {
  return invoke("logs_tail", { lines });
}

// from: the tail's `end` (null: the file's end); onLines: a Channel. → {followId}
export async function logsFollow(from, onLines) {
  return invoke("logs_follow", { from, onLines });
}

export async function logsUnfollow(followId) {
  return invoke("logs_unfollow", { followId });
}

// → {ok, firstLine, logPath, version?}
export async function selfupdateRun(tag) {
  return invoke("selfupdate_run", { tag });
}

export async function selfupdateInstall() {
  return invoke("selfupdate_install");
}

// → {lines, log, crashes}
export async function crushDirs() {
  return invoke("crush_dirs");
}

// → {ok, lines}
export async function crushStats() {
  return invoke("crush_stats");
}

// → {ok, version?, error?}
export async function dockerMcpVersion() {
  return invoke("docker_mcp_version");
}

// → {projects: [{path, data_dir, last_accessed, saved}]}
export async function projectsList() {
  return invoke("projects_list");
}

export async function projectSave(path) {
  return invoke("project_save", { path });
}

export async function projectRemove(path) {
  return invoke("project_remove", { path });
}

// → string[]
export async function recentFolders(limit) {
  return invoke("recent_folders", { limit });
}

export async function subagentInstructionsRead() {
  return invoke("subagent_instructions_read");
}

export async function subagentInstructionsWrite(text) {
  return invoke("subagent_instructions_write", { text });
}

export async function remotePasswordExists() {
  return invoke("remote_password_exists");
}

// → {ok, message}; neither value is kept or logged.
export async function remotePasswordSet(first, second) {
  return invoke("remote_password_set", { first, second });
}

// → {path, entries: [{name, is_dir, is_symlink, readable}]}
export async function dirList(path) {
  return invoke("dir_list", { path });
}

// → a path, or null when cancelled.
export async function folderPick(start) {
  return invoke("folder_pick", { start });
}
