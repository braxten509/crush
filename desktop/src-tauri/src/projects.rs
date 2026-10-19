//! Saved projects in `projects.json` (desktop-design-5-6 2.5, 2.21).
//! Placeholder from scaffold S5; SES owns this file (7.3).

use crate::error::{AppError, AppResult};
use serde::Serialize;

#[derive(Serialize)]
pub struct Projects {
    /// `{path, data_dir, last_accessed, saved}` each, as `crush projects --json`.
    pub projects: Vec<serde_json::Value>,
}

/// `projects_list`.
pub fn list() -> AppResult<Projects> {
    Err(AppError::not_built("projects_list"))
}

/// `project_save`: keeps unknown fields; a temporary file and a rename.
pub fn save(_path: &str) -> AppResult<()> {
    Err(AppError::not_built("project_save"))
}

/// `project_remove`.
pub fn remove(_path: &str) -> AppResult<()> {
    Err(AppError::not_built("project_remove"))
}

/// `recent_folders`: 2.21's rule.
pub fn recent_folders(_limit: u32) -> AppResult<Vec<String>> {
    Err(AppError::not_built("recent_folders"))
}
