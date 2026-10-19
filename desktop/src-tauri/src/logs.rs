//! The log viewer's reads (desktop-design-5-6 2.28, 6.4). Placeholder from
//! scaffold S5; SYS owns this file (7.3).

use crate::error::{AppError, AppResult};
use serde::Serialize;
use tauri::ipc::Channel;
use tauri::AppHandle;

/// The running follows, by id (managed by main.rs).
#[derive(Default)]
pub struct Follows {}

#[derive(Serialize)]
pub struct Tail {
    pub lines: Vec<String>,
    pub truncated: bool,
    pub missing: bool,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Followed {
    pub follow_id: String,
}

/// `logs_tail`: the window's project log (its `Ensured.data_dir`, `connection::ensured`).
pub fn tail(_app: &AppHandle, _label: &str, _lines: u32) -> AppResult<Tail> {
    Err(AppError::not_built("logs_tail"))
}

/// `logs_follow`: new lines as the file grows.
pub fn follow(_app: &AppHandle, _label: &str, _on_lines: Channel<serde_json::Value>) -> AppResult<Followed> {
    Err(AppError::not_built("logs_follow"))
}

pub fn unfollow(_app: &AppHandle, _follow_id: &str) -> AppResult<()> {
    Err(AppError::not_built("logs_unfollow"))
}
