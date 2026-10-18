//! Clipboard writes and reads through Rust (desktop-design 1.2.8). Scaffold S3
//! placeholder: piece P3.2 writes the bodies; the public signatures are
//! pinned. The commands that call these run off the main thread, so a body
//! may wait for `app.run_on_main_thread`.
#![allow(dead_code, unused_variables)]

use crate::error::{AppError, AppResult};

/// Linux: gtk::Clipboard (CLIPBOARD selection) on the main thread via app.run_on_main_thread;
/// Mac: NSPasteboard (inferred, objc2-app-kit is in the lock).
pub fn write_text(app: &tauri::AppHandle, text: &str) -> AppResult<()> {
    Err(AppError::not_built("clipboard::write_text"))
}

/// Ctrl+Shift+V (TYPE-010).
pub fn read_text(app: &tauri::AppHandle) -> AppResult<Option<String>> {
    Err(AppError::not_built("clipboard::read_text"))
}
