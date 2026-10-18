//! File reads for attachments and the `@` popup (desktop-design 1.2.8).
//! Scaffold S3 placeholder: piece P3.2 writes the bodies; the public
//! signatures are pinned.
#![allow(dead_code, unused_variables)]

use crate::error::{AppError, AppResult};

#[derive(serde::Serialize)]
pub struct FileInfo {
    pub exists: bool,
    pub is_dir: bool,
    pub size: u64,
    pub mime: String,
}

/// Mime from the infer crate, then the extension.
pub fn info(path: &str, cwd: &str) -> AppResult<FileInfo> {
    Err(AppError::not_built("files::info"))
}

/// (mime, base64); too_big over max_bytes.
pub fn read(path: &str, cwd: &str, max_bytes: u64) -> AppResult<(String, String)> {
    Err(AppError::not_built("files::read"))
}

/// Path relative to cwd, "/" separators. Clone lets the command keep a window's list for 5 s.
#[derive(serde::Serialize, Clone)]
pub struct FileEntry {
    pub path: String,
    pub is_dir: bool,
}

/// The `@` popup's file list (TYPE-026). Inside a Git work tree: `git ls-files --cached --others
/// --exclude-standard` (2 s limit), which applies the ignore files; elsewhere a walk that skips
/// hidden folders. Both stop at max_depth folders deep and max_items entries. Inferred to match
/// the terminal's walker closely enough; the builder compares with `internal/fsext` and notes
/// any difference (for example `.crushignore`) in the piece's report.
pub fn list(cwd: &str, max_depth: u32, max_items: u32) -> AppResult<Vec<FileEntry>> {
    Err(AppError::not_built("files::list"))
}
