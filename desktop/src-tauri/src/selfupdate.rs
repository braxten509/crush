//! Crush's own update runs (desktop-design-5-6 2.19, G-19). Placeholder from
//! scaffold S5; SYS owns this file (7.3).

use crate::error::{AppError, AppResult};
use serde::Serialize;
use tauri::AppHandle;

/// One self-update run per app (managed by main.rs).
#[derive(Default)]
pub struct Runner {}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct RunResult {
    pub ok: bool,
    pub first_line: String,
    pub log_path: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub version: Option<String>,
}

/// `selfupdate_run`: `crush self-update run --tag <tag> --json`; `update_running` while one runs.
pub fn run(_app: &AppHandle, _tag: &str) -> AppResult<RunResult> {
    Err(AppError::not_built("selfupdate_run"))
}

/// `selfupdate_install`: `crush self-update install --json`.
pub fn install(_app: &AppHandle) -> AppResult<RunResult> {
    Err(AppError::not_built("selfupdate_install"))
}
