//! Finding `crush` (desktop-design 1.2.3). Scaffold S3 placeholder: piece P3.1
//! writes the bodies; the public signatures are pinned.
#![allow(dead_code, unused_variables)]

use crate::error::{AppError, AppResult};

pub struct CrushBin {
    pub path: std::path::PathBuf,
    pub source: BinSource,
}

pub enum BinSource {
    Env,
    Setting,
    Path,
}

/// Order: CRUSH_DESKTOP_CRUSH (tests), then settings.crush_path, then `crush` found on PATH
/// (the PATH of the login-shell environment, section 1.2.7, else the app's own PATH).
pub fn resolve(setting: Option<&str>, login_path: Option<&str>) -> AppResult<CrushBin> {
    Err(AppError::not_built("crush_bin::resolve"))
}
