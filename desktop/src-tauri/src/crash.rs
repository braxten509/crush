//! The crash guard (desktop-design-5-6 2.28, MISC-025, START-020).
//! Placeholder from scaffold S5: `install` and `watch_webview` do nothing yet.
//! SYS owns this file (7.3).

use crate::error::{AppError, AppResult};
use serde::Serialize;
use tauri::AppHandle;

/// The first statement of main(): installs the panic report.
pub fn install() {}

/// Watches a window's web process for a crash.
pub fn watch_webview<R: tauri::Runtime>(_window: &tauri::WebviewWindow<R>) {}

#[derive(Serialize)]
pub struct Pending {
    pub path: String,
    /// "panic" or "webview".
    pub kind: String,
}

/// `crash_pending`: each report once.
pub fn pending(_app: &AppHandle) -> AppResult<Option<Pending>> {
    Err(AppError::not_built("crash_pending"))
}

/// `crash_restart`: relaunches after a panic, or reloads the window after a web view crash.
pub fn restart(_app: &AppHandle, _label: &str) -> AppResult<()> {
    Err(AppError::not_built("crash_restart"))
}
