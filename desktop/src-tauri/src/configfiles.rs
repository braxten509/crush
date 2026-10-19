//! Files beside the config the settings sheets read and write
//! (desktop-design-5-6 2.12, section 4). Placeholder from scaffold S5; SET
//! owns this file (7.3).

use crate::error::{AppError, AppResult};

/// `subagent_instructions_read`.
pub fn subagent_instructions_read() -> AppResult<String> {
    Err(AppError::not_built("subagent_instructions_read"))
}

/// `subagent_instructions_write`: a temporary file and a rename.
pub fn subagent_instructions_write(_text: &str) -> AppResult<()> {
    Err(AppError::not_built("subagent_instructions_write"))
}

/// `remote_password_exists`: presence only.
pub fn remote_password_exists() -> AppResult<bool> {
    Err(AppError::not_built("remote_password_exists"))
}
