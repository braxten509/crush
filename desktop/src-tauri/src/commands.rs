//! Every Tauri command (desktop-design 1.3), each a short delegate into the
//! module that does the work. With `--fixture`, the engine commands go to the
//! fixture player instead of the engine connection (6.3). Every failure is an
//! AppError (1.2.2).

use crate::connection::{self, OpenResult};
use crate::error::AppResult;
use crate::files::{self, FileEntry, FileInfo};
use crate::fixture::Fixture;
use crate::settings::{self, Settings};
use crate::state::{count_lines, AppState, Launch};
use crate::windows::{self, Opened, WindowLaunch, Windows};
use crate::{
    alerts, clipboard, configfiles, crash, crushinfo, dirlist, docker, editor, folderdialog, logs, opener,
    projects, pty, remote, selfupdate,
};
use serde::Serialize;
use serde_json::{Map, Value};
use std::collections::HashMap;
use std::sync::{LazyLock, Mutex};
use std::time::{Duration, Instant};
use tauri::ipc::Channel;
use tauri::{AppHandle, Manager, State, WebviewWindow, Window};

#[derive(Serialize, Debug, PartialEq)]
pub struct AppInfo {
    version: &'static str,
    probe: bool,
    fixture: bool,
    /// The calling window's folder, chat and continue flag (desktop-design-5-6 6.4).
    cwd: String,
    session: Option<String>,
    continue_last: bool,
    ui_debug: bool,
    platform: &'static str,
    first_window: bool,
    /// The window's launch ID the first time it is asked, then null (REM-009).
    launch_id: Option<String>,
    new_chat: bool,
    home: String,
    /// This computer's name, as Go's os.Hostname gives it: a terminal's
    /// OSC 7 folder report may name it (terminal.go:100-115).
    host: String,
}

#[tauri::command]
pub fn app_info(window: Window, state: State<AppState>, windows: State<Windows>) -> AppInfo {
    let label = window.label();
    let opened = windows::info_launch(&state, Some(&windows), label);
    let launch_id = windows.take_launch_id(label);
    info_for(&state.launch, &opened, label == windows::FIRST, launch_id)
}

/// `app_info` for one window: the process's flags, the window's own launch.
fn info_for(launch: &Launch, opened: &WindowLaunch, first_window: bool, launch_id: Option<String>) -> AppInfo {
    AppInfo {
        version: env!("CARGO_PKG_VERSION"),
        probe: launch.probe.is_some(),
        fixture: launch.fixture.is_some(),
        cwd: opened.cwd.to_string_lossy().into_owned(),
        session: opened.session.clone(),
        continue_last: opened.continue_last,
        ui_debug: launch.ui_debug,
        platform: if cfg!(target_os = "macos") { "macos" } else { "linux" },
        first_window,
        launch_id,
        new_chat: opened.new_chat,
        home: dirs::home_dir().map(|h| h.to_string_lossy().into_owned()).unwrap_or_default(),
        host: host_name(),
    }
}

extern "C" {
    fn gethostname(name: *mut std::ffi::c_char, len: usize) -> std::ffi::c_int;
}

/// gethostname(2), or "" when it fails.
fn host_name() -> String {
    let mut buf = [0u8; 256];
    // SAFETY: the buffer is writable for its whole length.
    if unsafe { gethostname(buf.as_mut_ptr().cast(), buf.len()) } != 0 {
        return String::new();
    }
    let end = buf.iter().position(|&b| b == 0).unwrap_or(buf.len());
    String::from_utf8_lossy(&buf[..end]).into_owned()
}

/// The first window writes the probe file, every other window `<probe file>.<label>` (8.2).
#[tauri::command]
pub fn probe_write(window: Window, state: State<AppState>, json: String) -> Result<(), String> {
    let probe = state.launch.probe.as_ref().ok_or("not started with --probe")?;
    let label = window.label();
    let path = windows::probe_file(probe, label);
    // Write then rename, so a reader never sees half a report.
    let tmp = if label == windows::FIRST {
        path.with_extension("tmp")
    } else {
        let mut name = path.clone().into_os_string();
        name.push(".tmp");
        name.into()
    };
    std::fs::write(&tmp, json).map_err(|e| e.to_string())?;
    std::fs::rename(&tmp, &path).map_err(|e| e.to_string())
}

#[derive(Serialize)]
pub struct ProbeRequest {
    /// The JSON of `<probe file>.request.json`; null when there is none.
    request: Option<Value>,
    /// The number of lines in `<probe file>.opened.jsonl` (the probe's `opened`).
    opened: usize,
}

/// Probe mode only: `{request, opened}`, else null (1.9). Reads, never writes.
#[tauri::command(async)]
pub fn probe_request(state: State<'_, AppState>) -> AppResult<Option<ProbeRequest>> {
    let launch = &state.launch;
    let (Some(request), Some(opened)) = (
        launch.probe_side_file("request.json"),
        launch.probe_side_file("opened.jsonl"),
    ) else {
        return Ok(None);
    };
    Ok(Some(ProbeRequest {
        request: std::fs::read(request)
            .ok()
            .and_then(|bytes| serde_json::from_slice(&bytes).ok()),
        opened: count_lines(&opened),
    }))
}

/// True to the first window that asks, once per app start: that window runs
/// the automatic update checks when it is ready (desktop-design-5-6 2.19),
/// whichever window that turns out to be.
#[tauri::command]
pub fn updates_claim() -> bool {
    static CLAIMED: std::sync::atomic::AtomicBool = std::sync::atomic::AtomicBool::new(false);
    !CLAIMED.swap(true, std::sync::atomic::Ordering::SeqCst)
}

/// Probe mode only: runs `<probe file>.eval.js` in the calling window, for
/// the alignment check, which reads the real page's boxes (8.6). The page's
/// CSP forbids eval; a script the app itself runs is not held to it. The
/// script reports through `window.__probeEval`, which the probe passes on.
#[tauri::command]
pub fn probe_eval(window: WebviewWindow, state: State<AppState>) -> Result<(), String> {
    let path = state.launch.probe_side_file("eval.js").ok_or("not started with --probe")?;
    let script = std::fs::read_to_string(&path).map_err(|e| format!("{}: {e}", path.display()))?;
    window.eval(script).map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn engine_open(
    app: AppHandle,
    window: Window,
    state: State<'_, AppState>,
    cwd: String,
) -> AppResult<OpenResult> {
    if let Some(fixture) = app.try_state::<Fixture>() {
        let sent_yolo = state
            .launch
            .yolo
            .unwrap_or_else(|| state.settings.lock().unwrap().yolo);
        return fixture.open(&app, window.label(), &cwd, sent_yolo);
    }
    connection::open(&app, window.label(), &cwd).await
}

#[tauri::command]
pub async fn engine_retry(app: AppHandle, window: Window) -> AppResult<()> {
    if app.try_state::<Fixture>().is_some() {
        return Ok(());
    }
    connection::retry(&app, window.label()).await
}

#[tauri::command]
pub async fn engine_set_current_session(
    app: AppHandle,
    window: Window,
    state: State<'_, AppState>,
    session_id: String,
) -> AppResult<()> {
    if let Some(fixture) = app.try_state::<Fixture>() {
        return fixture.set_current_session(&state.launch, window.label(), &session_id);
    }
    connection::set_current_session(&app, window.label(), &session_id).await
}

/// Once per animation frame (1.5.4).
#[tauri::command]
pub fn events_take(app: AppHandle, window: Window) -> Vec<Value> {
    if let Some(fixture) = app.try_state::<Fixture>() {
        return fixture.take(window.label());
    }
    let mut events = connection::take(&app, window.label());
    // An MCP server's error is built from its URL, which may carry a
    // password (6.1); events carry it near the top.
    for event in &mut events {
        scrub_errors(event, 3);
    }
    events
}

#[tauri::command]
#[allow(clippy::too_many_arguments)] // one argument per field of the `api` command (1.3)
pub async fn api(
    app: AppHandle,
    window: Window,
    state: State<'_, AppState>,
    method: String,
    path: String,
    query: Option<Map<String, Value>>,
    body: Option<Value>,
    call_id: Option<String>,
) -> AppResult<Value> {
    let answer = if let Some(fixture) = app.try_state::<Fixture>() {
        fixture.api(&state.launch, window.label(), &method, &path, query, body, call_id)
    } else {
        connection::call(&app, window.label(), &method, &path, query, body, call_id).await
    };
    answer.map(|value| redact_answer(&method, &path, value))
}

/// GET …/config carries every provider's key and sign-in token, and MCP and
/// LSP entries carry env, headers and command lines; GET …/providers carries
/// the catalog's key templates and headers. None of it may reach the page
/// (desktop-design-5-6 6.1), so it is cut here, before the answer crosses:
/// a provider keeps what decides a group, a sheet or a label, plus
/// `has_api_key`, `has_oauth` and the token's expiry times.
fn redact_answer(method: &str, path: &str, mut value: Value) -> Value {
    let route = path.trim_start_matches('/');
    // The Copilot import answers with the imported token itself: only
    // whether it worked crosses (handlers_config.go:49).
    if method == "POST" && route == "config/import-copilot" {
        if let Some(fields) = value.as_object_mut() {
            fields.retain(|key, _| key == "success" || key == "imported" || key == "ok");
        }
        return value;
    }
    if method != "GET" {
        return value;
    }
    match route {
        "config" => redact_config(&mut value),
        // A server's configured URL may carry a password or key in its user
        // part or query (mcp/auth.go:72): only scheme, host and path cross.
        "mcp/pending-auth" => {
            if let Some(list) = value.as_array_mut() {
                for server in list.iter_mut().filter_map(Value::as_object_mut) {
                    if let Some(url) = server.get("url").and_then(Value::as_str).map(bare_url) {
                        server.insert("url".into(), Value::String(url));
                    }
                }
            }
        }
        // Each server's error text may name its URL with a password in it.
        "mcp/states" => scrub_errors(&mut value, 3),
        "providers" => {
            if let Some(list) = value.as_array_mut() {
                for provider in list.iter_mut().filter_map(Value::as_object_mut) {
                    for key in ["api_key", "api_endpoint", "default_headers"] {
                        provider.remove(key);
                    }
                }
            }
        }
        _ => {}
    }
    value
}

/// Takes passwords out of the URLs in every "error" string down to depth
/// levels: the user part and the query of each `scheme://` word go.
fn scrub_errors(value: &mut Value, depth: usize) {
    match value {
        Value::Object(fields) => {
            for (key, field) in fields.iter_mut() {
                match field {
                    Value::String(text) if key == "error" => *text = scrub_urls(text),
                    _ if depth > 0 => scrub_errors(field, depth - 1),
                    _ => {}
                }
            }
        }
        Value::Array(items) if depth > 0 => items.iter_mut().for_each(|item| scrub_errors(item, depth - 1)),
        _ => {}
    }
}

/// `text` with each URL in it cut to scheme, host and path (bare_url).
fn scrub_urls(text: &str) -> String {
    let mut out = String::with_capacity(text.len());
    let mut rest = text;
    while let Some(at) = rest.find("://") {
        // The scheme is the letters right before "://".
        let start = rest[..at].rfind(|c: char| !(c.is_ascii_alphanumeric() || "+-.".contains(c))).map_or(0, |i| i + 1);
        let end = rest[at..].find(|c: char| c.is_whitespace() || "\"'<>`".contains(c)).map_or(rest.len(), |i| at + i);
        // Punctuation that ends the sentence is not part of the URL.
        let end = start + rest[start..end].trim_end_matches([':', ',', ';', '.', ')']).len();
        out.push_str(&rest[..start]);
        let word = &rest[start..end];
        let bare = bare_url(word);
        out.push_str(if bare.is_empty() { word } else { &bare });
        rest = &rest[end..];
    }
    out.push_str(rest);
    out
}

/// `scheme://host[:port]/path` of a URL: no user part, query or fragment.
fn bare_url(url: &str) -> String {
    let url = url.split(['?', '#']).next().unwrap_or("");
    let Some((scheme, rest)) = url.split_once("://") else { return String::new() };
    let (authority, path) = rest.split_once('/').map(|(a, p)| (a, format!("/{p}"))).unwrap_or((rest, String::new()));
    let host = authority.rsplit_once('@').map(|(_, h)| h).unwrap_or(authority);
    format!("{scheme}://{host}{path}")
}

fn redact_config(config: &mut Value) {
    const PROVIDER_KEEP: &[&str] = &["id", "name", "type", "disable", "models", "chatgpt_models", "grok_models", "discover_models", "flat_rate"];
    const ENTRY_KEEP: &[&str] = &["type", "disabled", "timeout"];
    let Some(root) = config.as_object_mut() else { return };
    if let Some(providers) = root.get_mut("providers").and_then(Value::as_object_mut) {
        for provider in providers.values_mut() {
            let Some(fields) = provider.as_object_mut() else {
                *provider = Value::Null;
                continue;
            };
            let has_key = fields.get("api_key").and_then(Value::as_str).is_some_and(|k| !k.is_empty());
            let oauth = fields.get("oauth").and_then(Value::as_object).map(|token| {
                let mut kept = Map::new();
                for key in ["expires_at", "expires_in"] {
                    if let Some(v) = token.get(key) {
                        kept.insert(key.to_string(), v.clone());
                    }
                }
                Value::Object(kept)
            });
            fields.retain(|key, _| PROVIDER_KEEP.contains(&key.as_str()));
            fields.insert("has_api_key".into(), Value::Bool(has_key));
            fields.insert("has_oauth".into(), Value::Bool(oauth.is_some()));
            if let Some(token) = oauth {
                fields.insert("oauth_times".into(), token);
            }
        }
    }
    for section in ["mcp", "lsp"] {
        if let Some(entries) = root.get_mut(section).and_then(Value::as_object_mut) {
            for entry in entries.values_mut() {
                if let Some(fields) = entry.as_object_mut() {
                    fields.retain(|key, _| ENTRY_KEEP.contains(&key.as_str()));
                }
            }
        }
    }
    // `env` holds the engine's startup environment values (config.go:109).
    for key in ["hooks", "tools", "env"] {
        root.remove(key);
    }
}

/// Stops the `api` call that carries this `callId` (1.2.4); false when none is running.
#[tauri::command]
pub fn api_abort(app: AppHandle, window: Window, state: State<AppState>, call_id: String) -> bool {
    if let Some(fixture) = app.try_state::<Fixture>() {
        return fixture.abort(&state.launch, window.label(), &call_id);
    }
    connection::abort(&app, window.label(), &call_id)
}

#[tauri::command]
pub fn settings_get(state: State<AppState>) -> Settings {
    state.settings.lock().unwrap().clone()
}

#[tauri::command(async)]
pub fn settings_set(app: AppHandle, key: String, value: Value) -> AppResult<Settings> {
    settings::set_key(&app, &key, value)
}

/// Phase 4: the closed chats, kept per engine data folder.
#[tauri::command(async)]
pub fn closed_set(app: AppHandle, project: String, session_id: String, closed: bool) -> AppResult<Settings> {
    settings::set_closed(&app, &project, &session_id, closed)
}

#[tauri::command(async)]
pub fn clipboard_write(app: AppHandle, text: String) -> AppResult<()> {
    clipboard::write_text(&app, &text)
}

#[tauri::command(async)]
pub fn clipboard_read(app: AppHandle) -> AppResult<Option<String>> {
    clipboard::read_text(&app)
}

/// After a click only (1.7).
#[tauri::command(async)]
pub fn open_link(app: AppHandle, url: String, cwd: String) -> AppResult<()> {
    opener::open(&app, &url, &cwd)
}

#[tauri::command(async)]
pub fn file_info(path: String, cwd: String) -> AppResult<FileInfo> {
    files::info(&path, &cwd)
}

#[derive(Serialize)]
pub struct FileData {
    mime: String,
    base64: String,
}

#[tauri::command(async)]
pub fn file_read(path: String, cwd: String, max_bytes: u64) -> AppResult<FileData> {
    let (mime, base64) = files::read(&path, &cwd, max_bytes)?;
    Ok(FileData { mime, base64 })
}

/// A window's last `@` list, kept 5 s: (when, cwd, max depth, max items, entries).
type CachedList = (Instant, String, u32, u32, Vec<FileEntry>);
static FILE_LISTS: LazyLock<Mutex<HashMap<String, CachedList>>> = LazyLock::new(Default::default);
const FILE_LIST_FOR: Duration = Duration::from_secs(5);

/// The `@` popup; cached per window for 5 s.
#[tauri::command(async)]
pub fn files_list(window: Window, cwd: String, max_depth: u32, max_items: u32) -> AppResult<Vec<FileEntry>> {
    let label = window.label().to_string();
    if let Some((at, c, d, n, entries)) = FILE_LISTS.lock().unwrap().get(&label) {
        if at.elapsed() < FILE_LIST_FOR && (c.as_str(), *d, *n) == (cwd.as_str(), max_depth, max_items) {
            return Ok(entries.clone());
        }
    }
    let entries = files::list(&cwd, max_depth, max_items)?;
    FILE_LISTS
        .lock()
        .unwrap()
        .insert(label, (Instant::now(), cwd, max_depth, max_items, entries.clone()));
    Ok(entries)
}

// Phases 5 and 6 (desktop-design-5-6 6.4): one delegate per command, into the
// module of the piece that owns it (7.3).

/// A POST that carries a secret (2.4, 2.7): only to the routes of 6.4. The
/// body goes to Rust as bytes that are zeroed after the call; the value Tauri
/// handed over is zeroed too. In fixture mode only the route and the body's
/// top-level key names are recorded.
#[tauri::command]
pub async fn api_secret(app: AppHandle, window: Window, state: State<'_, AppState>, path: String, mut body: Value) -> AppResult<Value> {
    if let Some(fixture) = app.try_state::<Fixture>() {
        let answer = fixture.api_secret(&state.launch, &path, &body);
        connection::wipe_value(&mut body);
        return answer;
    }
    let bytes = serde_json::to_vec(&body).unwrap_or_default();
    connection::wipe_value(&mut body);
    connection::call_secret(&app, window.label(), &path, bytes).await
}

#[tauri::command(async)]
pub fn window_open(
    app: AppHandle,
    project_dir: String,
    session_id: Option<String>,
    new_chat: Option<bool>,
    launch_id: Option<String>,
) -> AppResult<Opened> {
    windows::open(&app, &project_dir, session_id, new_chat.unwrap_or(false), launch_id)
}

/// After the window's close steps (lib/closing.js); the last window ends the app.
#[tauri::command]
pub async fn window_close(app: AppHandle, window: Window) -> AppResult<()> {
    windows::close(&app, window.label()).await;
    Ok(())
}

#[tauri::command]
pub async fn project_switch(app: AppHandle, window: Window, project_dir: String) -> AppResult<()> {
    windows::switch(&app, window.label(), &project_dir).await
}

#[tauri::command(async)]
pub fn pty_spawn(
    app: AppHandle,
    window: Window,
    cwd: String,
    argv: Option<Vec<String>>,
    cols: u16,
    rows: u16,
    on_data: Channel<Value>,
) -> AppResult<pty::Spawned> {
    pty::spawn(&app, window.label(), &cwd, argv, cols, rows, on_data)
}

#[tauri::command(async)]
pub fn pty_write(app: AppHandle, id: String, data: String) -> AppResult<()> {
    pty::write(&app, &id, &data)
}

#[tauri::command(async)]
pub fn pty_resize(app: AppHandle, id: String, cols: u16, rows: u16) -> AppResult<()> {
    pty::resize(&app, &id, cols, rows)
}

#[tauri::command(async)]
pub fn pty_cwd(app: AppHandle, id: String) -> AppResult<Option<String>> {
    pty::cwd(&app, &id)
}

#[tauri::command(async)]
pub fn pty_close(app: AppHandle, id: String) -> AppResult<()> {
    pty::close(&app, &id)
}

#[tauri::command(async)]
pub fn pty_close_all(app: AppHandle, window: Window) -> AppResult<()> {
    pty::close_all(&app, window.label())
}

#[tauri::command(async)]
pub fn editor_prepare(edits: State<editor::Edits>, text: String, line: u32, column: u32) -> AppResult<editor::Prepared> {
    editor::prepare(&edits, &text, line, column)
}

#[tauri::command(async)]
pub fn editor_finish(edits: State<editor::Edits>, path: String) -> AppResult<editor::Finished> {
    editor::finish(&edits, &path)
}

#[tauri::command(async)]
pub fn alert_notify(app: AppHandle, title: String, body: String) -> AppResult<alerts::Notified> {
    alerts::notify(&app, &title, &body)
}

#[tauri::command(async)]
pub fn alert_sound(app: AppHandle, id: String) -> AppResult<alerts::Sounded> {
    alerts::sound(&app, &id)
}

#[tauri::command(async)]
pub fn alert_window_state(
    app: AppHandle,
    window: Window,
    state: String,
    project_dir: String,
    session_id: String,
    focused: bool,
) -> AppResult<()> {
    alerts::window_state(&app, window.label(), &state, &project_dir, &session_id, focused)
}

#[tauri::command(async)]
pub fn alert_handler(app: AppHandle, project_dir: String, session_id: String) -> AppResult<String> {
    alerts::handler(&app, &project_dir, &session_id)
}

#[tauri::command(async)]
pub fn crash_pending(app: AppHandle, window: Window) -> AppResult<Option<crash::Pending>> {
    crash::pending(&app, window.label())
}

#[tauri::command(async)]
pub fn crash_restart(app: AppHandle, window: Window) -> AppResult<()> {
    crash::restart(&app, window.label())
}

#[tauri::command(async)]
pub fn logs_tail(app: AppHandle, window: Window, lines: u32) -> AppResult<logs::Tail> {
    logs::tail(&app, window.label(), lines)
}

#[tauri::command(async)]
pub fn logs_follow(app: AppHandle, window: Window, from: Option<u64>, on_lines: Channel<Value>) -> AppResult<logs::Followed> {
    logs::follow(&app, window.label(), from, on_lines)
}

#[tauri::command(async)]
pub fn logs_unfollow(app: AppHandle, follow_id: String) -> AppResult<()> {
    logs::unfollow(&app, &follow_id)
}

#[tauri::command(async)]
pub fn selfupdate_run(app: AppHandle, tag: String) -> AppResult<selfupdate::RunResult> {
    selfupdate::run(&app, &tag)
}

#[tauri::command(async)]
pub fn selfupdate_install(app: AppHandle) -> AppResult<selfupdate::RunResult> {
    selfupdate::install(&app)
}

#[tauri::command(async)]
pub fn crush_dirs(app: AppHandle, window: Window) -> AppResult<crushinfo::Dirs> {
    crushinfo::dirs(&app, window.label())
}

#[tauri::command(async)]
pub fn crush_stats(app: AppHandle, window: Window) -> AppResult<crushinfo::Stats> {
    crushinfo::stats(&app, window.label())
}

#[tauri::command(async)]
pub fn docker_mcp_version() -> AppResult<docker::DockerMcp> {
    docker::mcp_version()
}

#[tauri::command(async)]
pub fn projects_list() -> AppResult<projects::Projects> {
    projects::list()
}

#[tauri::command(async)]
pub fn project_save(path: String) -> AppResult<()> {
    projects::save(&path)
}

#[tauri::command(async)]
pub fn project_remove(path: String) -> AppResult<()> {
    projects::remove(&path)
}

#[tauri::command(async)]
pub fn recent_folders(limit: u32) -> AppResult<Vec<String>> {
    projects::recent_folders(limit)
}

#[tauri::command(async)]
pub fn subagent_instructions_read() -> AppResult<String> {
    configfiles::subagent_instructions_read()
}

#[tauri::command(async)]
pub fn subagent_instructions_write(text: String) -> AppResult<()> {
    configfiles::subagent_instructions_write(&text)
}

#[tauri::command(async)]
pub fn remote_password_exists() -> AppResult<bool> {
    configfiles::remote_password_exists()
}

#[tauri::command(async)]
pub fn remote_password_set(app: AppHandle, first: String, second: String) -> AppResult<remote::PasswordSet> {
    remote::password_set(&app, first, second)
}

#[tauri::command(async)]
pub fn dir_list(path: String) -> AppResult<dirlist::DirList> {
    dirlist::list(&path)
}

#[tauri::command(async)]
pub fn folder_pick(app: AppHandle, start: String) -> AppResult<Option<String>> {
    folderdialog::pick(&app, &start)
}

#[cfg(test)]
mod scrub_tests {
    use super::*;
    use serde_json::json;

    // Nothing secret in GET config crosses: keys, tokens, env, headers,
    // commands; what decides a label does.
    #[test]
    fn config_keeps_no_secrets() {
        let config = json!({
            "providers": {"openai": {"id": "openai", "name": "OpenAI", "api_key": "sk-secret", "api_endpoint": "https://x",
                "extra_headers": {"Authorization": "Bearer t"}, "oauth": {"access_token": "at", "refresh_token": "rt", "expires_at": 9}}},
            "mcp": {"srv": {"type": "http", "url": "https://u:p@h/", "headers": {"k": "v"}, "env": {"K": "v"}, "disabled": true}},
            "lsp": {"go": {"command": "gopls", "env": {"K": "v"}, "timeout": 5}},
            "env": {"HOME": "/h"}, "hooks": {"x": 1}, "tools": {"y": 2}, "options": {"engine": "shared"}
        });
        let out = redact_answer("GET", "/config", config);
        let text = out.to_string();
        for secret in ["sk-secret", "Bearer t", "\"at\"", "\"rt\"", "u:p@", "gopls", "\"HOME\"", "https://x"] {
            assert!(!text.contains(secret), "{secret} crossed: {text}");
        }
        let p = &out["providers"]["openai"];
        assert_eq!(p["has_api_key"], true);
        assert_eq!(p["has_oauth"], true);
        assert_eq!(p["oauth_times"], json!({"expires_at": 9}));
        assert_eq!(out["mcp"]["srv"], json!({"type": "http", "disabled": true}));
        assert_eq!(out["lsp"]["go"], json!({"timeout": 5}));
        assert_eq!(out["options"]["engine"], "shared", "plain options still cross");
    }

    #[test]
    fn providers_and_imports_lose_keys() {
        let out = redact_answer("GET", "providers", json!([{"id": "a", "api_key": "k", "api_endpoint": "e", "default_headers": {"h": "v"}}]));
        assert_eq!(out, json!([{"id": "a"}]));
        let out = redact_answer("POST", "config/import-copilot", json!({"success": true, "token": "gho_secret"}));
        assert_eq!(out, json!({"success": true}));
        let out = redact_answer("GET", "mcp/pending-auth", json!([{"name": "s", "url": "https://u:pw@host/sse?key=k#f"}]));
        assert_eq!(out[0]["url"], "https://host/sse");
    }

    // Other routes and methods pass unchanged.
    #[test]
    fn other_answers_pass() {
        let v = json!({"api_key": "visible-on-purpose"});
        assert_eq!(redact_answer("GET", "sessions", v.clone()), v);
        assert_eq!(redact_answer("POST", "config", v.clone()), v);
    }

    #[test]
    fn errors_lose_url_passwords() {
        let mut v = serde_json::json!({"a": {"state": "error", "error": "dial https://u:secret@host.dev/sse?key=k2: refused"}, "list": [{"error": "plain"}]});
        scrub_errors(&mut v, 3);
        assert_eq!(v["a"]["error"], "dial https://host.dev/sse: refused");
        assert_eq!(v["list"][0]["error"], "plain");
        assert_eq!(scrub_urls("x \"ws://a:b@h/p\" y"), "x \"ws://h/p\" y");
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::PathBuf;

    #[test]
    fn app_info_is_per_window() {
        let launch = Launch {
            probe: Some(PathBuf::from("/tmp/probe.json")),
            cwd: PathBuf::from("/tmp/home/proj"),
            session: Some("s-first".into()),
            launch_id: Some("launch-first".into()),
            ..Default::default()
        };
        let windows = Windows::new(WindowLaunch::first(&launch));
        let plan = windows.plan_open(std::path::Path::new("/tmp/home/proj2"), Some("s-2".into()), true, Some("launch-2".into()));
        assert_eq!(plan, windows::OpenPlan::Create("w2".into()));
        let info = |label: &str| {
            let opened = windows.launch_for(label).unwrap();
            info_for(&launch, &opened, label == windows::FIRST, windows.take_launch_id(label))
        };
        let first = info(windows::FIRST);
        assert_eq!(
            (first.cwd.as_str(), first.session.as_deref(), first.first_window, first.launch_id.as_deref(), first.new_chat),
            ("/tmp/home/proj", Some("s-first"), true, Some("launch-first"), false)
        );
        let second = info("w2");
        assert_eq!(
            (second.cwd.as_str(), second.session.as_deref(), second.first_window, second.launch_id.as_deref(), second.new_chat),
            ("/tmp/home/proj2", Some("s-2"), false, Some("launch-2"), true)
        );
        assert!(second.probe && !second.continue_last);
        // The launch ID is reported the first time only, per window.
        assert_eq!(info("w2").launch_id, None);
        assert_eq!(info(windows::FIRST).launch_id, None);
        // A switched window reports its new folder and no chat.
        windows.set_project("w2", std::path::Path::new("/tmp/home/proj3"));
        let switched = info("w2");
        assert_eq!((switched.cwd.as_str(), switched.session, switched.new_chat), ("/tmp/home/proj3", None, false));
    }
}
