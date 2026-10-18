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

export async function app_info() {
  return invoke("app_info");
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
