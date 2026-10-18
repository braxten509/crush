//! The version check and `crush engine ensure` (desktop-design 1.2.3). Scaffold
//! S3 placeholder: piece P3.1 writes the bodies; the public signatures are
//! pinned.
#![allow(dead_code, unused_variables)]

use crate::crush_bin::CrushBin;
use crate::error::{AppError, AppResult};

#[derive(serde::Deserialize, serde::Serialize, Clone)]
pub struct EngineVersion {
    pub version: String,
    pub protocol: i64,
}

#[derive(serde::Deserialize, serde::Serialize, Clone)]
pub struct Ensured {
    pub address: String,
    pub socket: String,
    pub protocol: i64,
    pub version: String,
    pub pid: i64,
    pub data_dir: String,
    pub started: bool,
}

pub const APP_PROTOCOL: i64 = 1; // proto.ProtocolVersion, internal/proto/protocol.go:6

/// Runs `<bin> engine version` (5 s limit). Ok only when it printed {"version","protocol"} and
/// protocol == APP_PROTOCOL; any other outcome is Err(crush_too_old) with what crush said.
pub fn version(bin: &CrushBin) -> AppResult<EngineVersion> {
    Err(AppError::not_built("engine_cli::version"))
}

/// Runs `<bin> engine ensure --cwd DIR [--data-dir D] --timeout 15s` (20 s limit) with `env`
/// as the child's whole environment. Parses the success JSON or the error JSON
/// {"error":{"code","message"}} (exit 3/4/5/6/1, engine design §5.2).
pub fn ensure(
    bin: &CrushBin,
    cwd: &str,
    data_dir: Option<&str>,
    env: &[(String, String)],
) -> AppResult<Ensured> {
    Err(AppError::not_built("engine_cli::ensure"))
}
