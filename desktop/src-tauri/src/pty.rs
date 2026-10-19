//! Terminal tabs (desktop-design-5-6 2.25, 6.4): `portable-pty` shells per
//! window. Placeholder from scaffold S5; TERM owns this file (7.3).

use crate::error::{AppError, AppResult};
use serde::Serialize;
use tauri::ipc::Channel;
use tauri::AppHandle;

/// The app's pty processes, by id (managed by main.rs).
#[derive(Default)]
pub struct Ptys {}

#[derive(Serialize)]
pub struct Spawned {
    pub id: String,
    pub pid: u32,
}

/// `pty_spawn`: `onData` gets `{data: <base64>}` batches, then `{exit: {code, signal}}` once.
pub fn spawn(
    _app: &AppHandle,
    _label: &str,
    _cwd: &str,
    _argv: Option<Vec<String>>,
    _cols: u16,
    _rows: u16,
    _on_data: Channel<serde_json::Value>,
) -> AppResult<Spawned> {
    Err(AppError::not_built("pty_spawn"))
}

/// `pty_write`: `data` is base64.
pub fn write(_app: &AppHandle, _id: &str, _data: &str) -> AppResult<()> {
    Err(AppError::not_built("pty_write"))
}

pub fn resize(_app: &AppHandle, _id: &str, _cols: u16, _rows: u16) -> AppResult<()> {
    Err(AppError::not_built("pty_resize"))
}

/// The shell's folder, or None where it cannot be read.
pub fn cwd(_app: &AppHandle, _id: &str) -> AppResult<Option<String>> {
    Err(AppError::not_built("pty_cwd"))
}

/// Ends one tab: SIGHUP, SIGTERM, then SIGKILL to its process group after 5 s.
pub fn close(_app: &AppHandle, _id: &str) -> AppResult<()> {
    Err(AppError::not_built("pty_close"))
}

/// Every tab of the window `label`, at most 6 s.
pub fn close_all(_app: &AppHandle, _label: &str) -> AppResult<()> {
    Err(AppError::not_built("pty_close_all"))
}
