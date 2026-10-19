//! The launcher password (desktop-design-5-6 2.27, G-20). Placeholder from
//! scaffold S5; SHARE owns this file (7.3).

use crate::error::{AppError, AppResult};
use serde::Serialize;
use tauri::AppHandle;

#[derive(Serialize)]
pub struct PasswordSet {
    pub ok: bool,
    pub message: String,
}

/// `remote_password_set`: `crush remote password --stdin` with the two lines;
/// neither value is logged and the buffers are zeroed.
pub fn password_set(_app: &AppHandle, _first: String, _second: String) -> AppResult<PasswordSet> {
    Err(AppError::not_built("remote_password_set"))
}
