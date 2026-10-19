//! `docker mcp version` (desktop-design-5-6 2.9). Placeholder from scaffold
//! S5; EXT owns this file (7.3).

use crate::error::{AppError, AppResult};
use serde::Serialize;

#[derive(Serialize)]
pub struct DockerMcp {
    pub ok: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub version: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
}

/// `docker_mcp_version`: 5 s; the answer is kept 10 s.
pub fn mcp_version() -> AppResult<DockerMcp> {
    Err(AppError::not_built("docker_mcp_version"))
}
