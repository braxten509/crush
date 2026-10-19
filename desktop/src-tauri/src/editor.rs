//! The external editor (desktop-design-5-6 2.26, 6.4). Placeholder from
//! scaffold S5; TERM owns this file (7.3).

use crate::error::{AppError, AppResult};
use serde::Serialize;

/// The temporary files `editor_prepare` made (managed by main.rs).
#[derive(Default)]
pub struct Edits {}

#[derive(Serialize)]
pub struct Prepared {
    pub path: String,
    pub argv: Vec<String>,
}

#[derive(Serialize)]
pub struct Finished {
    pub text: String,
}

/// `editor_prepare`: writes `text` to a new 0600 file and builds the editor command.
pub fn prepare(_edits: &Edits, _text: &str, _line: u32, _column: u32) -> AppResult<Prepared> {
    Err(AppError::not_built("editor_prepare"))
}

/// `editor_finish`: reads and deletes a file `prepare` made.
pub fn finish(_edits: &Edits, _path: &str) -> AppResult<Finished> {
    Err(AppError::not_built("editor_finish"))
}
