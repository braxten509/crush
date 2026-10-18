//! Join, stay joined, reconnect: one engine connection per window
//! (desktop-design 1.2.5, 1.2.6). Scaffold S3 placeholder: piece P3.1 writes
//! the bodies, the fields of WindowConn and the route list of 1.3; the public
//! signatures are pinned.
#![allow(dead_code, unused_variables)]

use crate::engine_cli;
use crate::error::{AppError, AppResult};

/// label, client_id, http, workspace, last current-session, state, batcher (P3.1).
pub struct WindowConn {}

#[derive(serde::Serialize, Clone)]
pub struct OpenResult {
    pub client_id: String,
    /// public_workspace(the create answer): proto.Workspace without env and config (1.2.5).
    pub workspace: serde_json::Value,
    pub engine: engine_cli::Ensured,
    pub version: engine_cli::EngineVersion,
    /// What the app asked for; compare with workspace.yolo.
    pub sent_yolo: bool,
    /// What the app asked for; compare with workspace.path.
    pub cwd: String,
}

/// Version check, ensure, join steps 1-4, stream start. Called once per window, and again by
/// "Try again" after a failure. Idempotent: a second call while connected returns the same result.
pub async fn open(app: &tauri::AppHandle, label: &str, cwd: &str) -> AppResult<OpenResult> {
    Err(AppError::not_built("connection::open"))
}

pub async fn retry(app: &tauri::AppHandle, label: &str) -> AppResult<()> {
    Err(AppError::not_built("connection::retry"))
}

/// Generic call for the window (section 1.3, `api`): adds client_id, checks the route list.
/// With `call_id`: no time limit, and the call is registered for `abort` until it ends (1.2.4).
pub async fn call(
    app: &tauri::AppHandle,
    label: &str,
    method: &str,
    path: &str,
    query: Option<serde_json::Map<String, serde_json::Value>>,
    body: Option<serde_json::Value>,
    call_id: Option<String>,
) -> AppResult<serde_json::Value> {
    Err(AppError::not_built("connection::call"))
}

/// `api_abort` (1.3): aborts that window's running call with this id; false when none is
/// running. The aborted call fails with code "aborted" (1.2.2). The placeholder runs no
/// calls, so none is ever running.
pub fn abort(app: &tauri::AppHandle, label: &str, call_id: &str) -> bool {
    false
}

pub async fn set_current_session(
    app: &tauri::AppHandle,
    label: &str,
    session_id: &str,
) -> AppResult<()> {
    Err(AppError::not_built("connection::set_current_session"))
}

/// The wire form of 1.4.2.
pub fn take(app: &tauri::AppHandle, label: &str) -> Vec<serde_json::Value> {
    Vec::new()
}

/// Window closing: abort every running call of that window (so its `!` commands end with it,
/// 1.2.4), then DELETE /v1/clients/{client_id} (1 s limit, errors only logged), stop the reader.
pub async fn close(app: &tauri::AppHandle, label: &str) {}

/// Drops `env` and `config` and keeps every other field; the only way a workspace leaves
/// connection.rs (1.2.5). The placeholder keeps nothing.
pub fn public_workspace(ws: serde_json::Value) -> serde_json::Value {
    serde_json::Value::Null
}
