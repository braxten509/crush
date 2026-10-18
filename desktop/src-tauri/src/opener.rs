//! Opening links after a click (desktop-design 1.2.8, CHAT-102). Scaffold S3
//! placeholder: piece P3.2 writes the bodies; the public signatures are
//! pinned.
#![allow(dead_code, unused_variables)]

use crate::error::{AppError, AppResult};

pub enum LinkKind {
    Web,
    File(std::path::PathBuf),
}

/// Same rules and texts as the terminal (CHECKLIST.md:211): http, https and mailto are web links;
/// file links are relative to `cwd`, may start with `~`, and a trailing `:line:col` is dropped
/// when no file has that exact name. Refusals: "cannot open links of type <scheme>",
/// "cannot open a file on another computer", "link does not name a local file".
pub fn classify(url: &str, cwd: &str) -> AppResult<LinkKind> {
    Err(AppError::not_built("opener::classify"))
}

/// Linux: the first of xdg-open, x-www-browser, www-browser that exists, detached; Mac: `open`.
/// File links use the same opener, which opens the file in the program the desktop assigns to
/// it (inferred reading of "your editor"; a terminal-tab editor is Ctrl+O, phase 6).
/// When the app runs with --probe, nothing is started: the request is appended as one JSON line
/// to "<probe file>.opened.jsonl" (so a test never shows a browser).
pub fn open(app: &tauri::AppHandle, url: &str, cwd: &str) -> AppResult<()> {
    Err(AppError::not_built("opener::open"))
}
