//! `docker mcp version` (desktop-design-5-6 2.9; the terminal's
//! internal/config/docker_mcp.go:28-62): at most 5 s, and the answer is kept
//! 10 s so the palette can ask on every open. EXT owns this file (7.3).

use crate::error::AppResult;
use serde::Serialize;
use std::process::{Command, Stdio};
use std::sync::Mutex;
use std::time::{Duration, Instant};

#[derive(Serialize, Clone)]
pub struct DockerMcp {
    pub ok: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub version: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
}

const LIMIT: Duration = Duration::from_secs(5);
const KEEP: Duration = Duration::from_secs(10);
static CACHE: Mutex<Option<(Instant, DockerMcp)>> = Mutex::new(None);

/// `docker_mcp_version`: 5 s; the answer is kept 10 s.
pub fn mcp_version() -> AppResult<DockerMcp> {
    if let Some((at, answer)) = CACHE.lock().unwrap_or_else(|e| e.into_inner()).as_ref() {
        if at.elapsed() < KEEP {
            return Ok(answer.clone());
        }
    }
    let answer = run();
    *CACHE.lock().unwrap_or_else(|e| e.into_inner()) = Some((Instant::now(), answer.clone()));
    Ok(answer)
}

fn run() -> DockerMcp {
    let fail = |error: String| DockerMcp { ok: false, version: None, error: Some(error) };
    let mut child = match Command::new("docker")
        .args(["mcp", "version"])
        .envs(crate::login_env::login_env())
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()
    {
        Ok(child) => child,
        Err(e) => return fail(e.to_string()),
    };
    let started = Instant::now();
    loop {
        match child.try_wait() {
            Ok(Some(status)) => {
                let mut out = String::new();
                if let Some(mut stdout) = child.stdout.take() {
                    use std::io::Read;
                    let _ = stdout.read_to_string(&mut out);
                }
                return if status.success() {
                    DockerMcp { ok: true, version: Some(out.trim().to_string()).filter(|v| !v.is_empty()), error: None }
                } else {
                    fail(format!("docker mcp version exited with {status}"))
                };
            }
            Ok(None) if started.elapsed() >= LIMIT => {
                let _ = child.kill();
                let _ = child.wait();
                return fail("docker mcp version took longer than 5 s".into());
            }
            Ok(None) => std::thread::sleep(Duration::from_millis(50)),
            Err(e) => return fail(e.to_string()),
        }
    }
}
