//! The taskbar badge, progress and attention (desktop-design-5-6 2.18,
//! NOTE-007, NOTE-008). Placeholder from scaffold S5; SYS owns this file (7.3).

use crate::error::{AppError, AppResult};
use tauri::AppHandle;

/// Applies the app-wide badge count, progress and attention.
#[allow(dead_code)] // alerts.rs (SYS) calls it
pub fn apply(_app: &AppHandle, _badge: Option<u32>, _progress: Option<&str>, _attention: bool) -> AppResult<()> {
    Err(AppError::not_built("taskbar"))
}
