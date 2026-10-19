//! A folder's entries for the pickers (desktop-design-5-6 2.10, 2.21).
//! Placeholder from scaffold S5; VIEW owns this file (7.3).

use crate::error::{AppError, AppResult};
use serde::Serialize;

#[derive(Serialize)]
pub struct DirEntry {
    pub name: String,
    pub is_dir: bool,
    pub is_symlink: bool,
    pub readable: bool,
}

#[derive(Serialize)]
pub struct DirList {
    pub path: String,
    pub entries: Vec<DirEntry>,
}

/// `dir_list`: `~` expanded; errors `bad_path` and `not_a_folder`.
pub fn list(_path: &str) -> AppResult<DirList> {
    Err(AppError::not_built("dir_list"))
}
