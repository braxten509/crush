//! The system folder dialog (desktop-design-5-6 2.21). Placeholder from
//! scaffold S5; LST owns this file (7.3).

use crate::error::{AppError, AppResult};
use tauri::AppHandle;

/// `folder_pick`: a path, or None when cancelled.
pub fn pick(_app: &AppHandle, _start: &str) -> AppResult<Option<String>> {
    Err(AppError::not_built("folder_pick"))
}
