//! Crush's own update runs (desktop-design-5-6 2.19, G-19): `crush
//! self-update run --tag <tag> --json` and `crush self-update install
//! --json`, one at a time per app (`update_running` otherwise). The run is
//! long (merge, AI fixes, build, every test), so it has a generous limit. SYS
//! owns this file (7.3).

use crate::crushinfo::run_crush;
use crate::error::{codes, AppError, AppResult};
use serde::{Deserialize, Serialize};
use std::sync::atomic::{AtomicBool, Ordering};
use std::time::Duration;
use tauri::{AppHandle, Manager};

/// One self-update run per app (managed by main.rs).
#[derive(Default)]
pub struct Runner {
    running: AtomicBool,
}

#[derive(Serialize, Deserialize, Default)]
#[serde(rename_all = "camelCase")]
pub struct RunResult {
    pub ok: bool,
    #[serde(default)]
    pub first_line: String,
    #[serde(default)]
    pub log_path: String,
    #[serde(skip_serializing_if = "Option::is_none", default)]
    pub version: Option<String>,
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub fixed: Vec<String>,
}

const RUN_LIMIT: Duration = Duration::from_secs(3 * 60 * 60);
const INSTALL_LIMIT: Duration = Duration::from_secs(30 * 60);

fn exclusive<T>(app: &AppHandle, work: impl FnOnce() -> AppResult<T>) -> AppResult<T> {
    let runner = app.state::<Runner>();
    if runner.running.swap(true, Ordering::SeqCst) {
        return Err(AppError::new(codes::UPDATE_RUNNING, "Crush is already updating"));
    }
    let answer = work();
    runner.running.store(false, Ordering::SeqCst);
    answer
}

fn parse(out: &str, err: &str) -> RunResult {
    out.lines()
        .rev()
        .find_map(|line| serde_json::from_str::<RunResult>(line.trim()).ok())
        .unwrap_or_else(|| RunResult {
            ok: false,
            first_line: err.lines().map(str::trim).find(|l| !l.is_empty()).unwrap_or("crush self-update gave no result").trim_start_matches("Error: ").to_string(),
            ..Default::default()
        })
}

/// `selfupdate_run`: `crush self-update run --tag <tag> --json`; `update_running` while one runs.
pub fn run(app: &AppHandle, tag: &str) -> AppResult<RunResult> {
    exclusive(app, || {
        let ran = run_crush(app, &["self-update", "run", "--tag", tag, "--json"], RUN_LIMIT)?;
        Ok(parse(&ran.out, &ran.err))
    })
}

/// `selfupdate_install`: `crush self-update install --json`.
pub fn install(app: &AppHandle) -> AppResult<RunResult> {
    exclusive(app, || {
        let ran = run_crush(app, &["self-update", "install", "--json"], INSTALL_LIMIT)?;
        Ok(parse(&ran.out, &ran.err))
    })
}
