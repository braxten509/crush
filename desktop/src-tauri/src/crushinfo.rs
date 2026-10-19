//! `crush dirs` and `crush stats` for the window's project (desktop-design-5-6
//! 2.28). Placeholder from scaffold S5; SYS owns this file (7.3).

use crate::error::{AppError, AppResult};
use serde::Serialize;
use tauri::AppHandle;

#[derive(Serialize)]
pub struct Dirs {
    pub lines: Vec<String>,
}

#[derive(Serialize)]
pub struct Stats {
    pub ok: bool,
    pub lines: Vec<String>,
}

/// `crush_dirs`: one folder per line.
pub fn dirs(_app: &AppHandle, _label: &str) -> AppResult<Dirs> {
    Err(AppError::not_built("crush_dirs"))
}

/// `crush_stats`: crush writes the report and opens it itself.
pub fn stats(_app: &AppHandle, _label: &str) -> AppResult<Stats> {
    Err(AppError::not_built("crush_stats"))
}
