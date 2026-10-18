//! Every Tauri command (desktop-design 1.3), each a short delegate into the
//! module that does the work. With `--fixture`, the engine commands go to the
//! fixture player instead of the engine connection (6.3). Every failure is an
//! AppError (1.2.2).

use crate::connection::{self, OpenResult};
use crate::error::AppResult;
use crate::files::{self, FileEntry, FileInfo};
use crate::fixture::Fixture;
use crate::settings::{self, Settings};
use crate::state::AppState;
use crate::{clipboard, opener};
use serde::Serialize;
use serde_json::{Map, Value};
use std::collections::HashMap;
use std::sync::{LazyLock, Mutex};
use std::time::{Duration, Instant};
use tauri::{AppHandle, Manager, State, Window};

#[derive(Serialize)]
pub struct AppInfo {
    version: &'static str,
    probe: bool,
    fixture: bool,
    cwd: String,
    session: Option<String>,
    continue_last: bool,
    ui_debug: bool,
    platform: &'static str,
}

#[tauri::command]
pub fn app_info(state: State<AppState>) -> AppInfo {
    let launch = &state.launch;
    AppInfo {
        version: env!("CARGO_PKG_VERSION"),
        probe: launch.probe.is_some(),
        fixture: launch.fixture.is_some(),
        cwd: launch.cwd.to_string_lossy().into_owned(),
        session: launch.session.clone(),
        continue_last: launch.continue_last,
        ui_debug: launch.ui_debug,
        platform: if cfg!(target_os = "macos") { "macos" } else { "linux" },
    }
}

#[tauri::command]
pub fn probe_write(state: State<AppState>, json: String) -> Result<(), String> {
    let path = state.launch.probe.as_ref().ok_or("not started with --probe")?;
    // Write then rename, so a reader never sees half a report.
    let tmp = path.with_extension("tmp");
    std::fs::write(&tmp, json).map_err(|e| e.to_string())?;
    std::fs::rename(&tmp, path).map_err(|e| e.to_string())
}

/// Probe mode only: the JSON of `<probe file>.request.json`, else null (1.9). Reads, never writes.
#[tauri::command(async)]
pub fn probe_request(state: State<'_, AppState>) -> AppResult<Option<Value>> {
    let Some(path) = state.launch.probe_side_file("request.json") else {
        return Ok(None);
    };
    Ok(std::fs::read(path)
        .ok()
        .and_then(|bytes| serde_json::from_slice(&bytes).ok()))
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
    connection::take(&app, window.label())
}

#[tauri::command]
pub async fn api(
    app: AppHandle,
    window: Window,
    state: State<'_, AppState>,
    method: String,
    path: String,
    query: Option<Map<String, Value>>,
    body: Option<Value>,
) -> AppResult<Value> {
    if let Some(fixture) = app.try_state::<Fixture>() {
        return fixture.api(&state.launch, window.label(), &method, &path, query, body);
    }
    connection::call(&app, window.label(), &method, &path, query, body).await
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
