//! Crush Desktop's own updater (owner, 2026-10-20: "an update button ... that
//! automatically installs new versions ... and restarts it"): runs update.sh,
//! which ships beside this helper, as `check` or `run`, in the owner's
//! login-shell environment, and answers its last JSON line. One run per app.
//! The restart keeps the open chats (openchats::hold_for_restart). A fixture
//! answers from its `app_update` ({"check": {}, "run": {}, "run_ms": 0}).

use crate::error::{codes, AppError, AppResult};
use crate::fixture::Fixture;
use crate::host::AppHandle;
use serde_json::Value;
use std::path::PathBuf;
use std::process::{Command, Stdio};
use std::sync::atomic::{AtomicBool, Ordering};

/// One update run per app (managed by main.rs).
#[derive(Default)]
pub struct Runner {
    running: AtomicBool,
}

/// update.sh beside the helper (the installed app; a dev build has none).
fn script() -> Option<PathBuf> {
    let path = std::env::current_exe().ok()?.parent()?.join("update.sh");
    path.is_file().then_some(path)
}

fn call(mode: &str) -> AppResult<Value> {
    let Some(path) = script() else {
        return Err(AppError::new(codes::UPDATE_UNAVAILABLE, "This copy of Crush Desktop has no updater (it was not installed with install.sh)"));
    };
    let ran = Command::new("bash")
        .arg(path)
        .arg(mode)
        .env_clear()
        .envs(crate::login_env::login_env())
        .env("GIT_TERMINAL_PROMPT", "0")
        .stdin(Stdio::null())
        .output()
        .map_err(|e| AppError::new(codes::UPDATE_UNAVAILABLE, format!("The updater could not start: {e}")))?;
    let out = String::from_utf8_lossy(&ran.stdout);
    out.lines()
        .rev()
        .find_map(|line| serde_json::from_str::<Value>(line.trim()).ok().filter(Value::is_object))
        .ok_or_else(|| AppError::new(codes::UPDATE_UNAVAILABLE, "The updater gave no answer"))
}

/// `app_update_check`: {current, latest, newer} or {error}.
pub fn check(app: &AppHandle) -> AppResult<Value> {
    if let Some(fixture) = app.try_state::<Fixture>() {
        return Ok(fixture.app_update("check").0);
    }
    call("check")
}

/// `app_update_run`: {ok, commit, version, log} or {ok: false, error, log}.
pub fn run(app: &AppHandle) -> AppResult<Value> {
    let runner = app.state::<Runner>();
    if runner.running.swap(true, Ordering::SeqCst) {
        return Err(AppError::new(codes::UPDATE_RUNNING, "Crush Desktop is already updating"));
    }
    let answer = if let Some(fixture) = app.try_state::<Fixture>() {
        let (value, wait) = fixture.app_update("run");
        std::thread::sleep(wait);
        Ok(value)
    } else {
        call("run")
    };
    runner.running.store(false, Ordering::SeqCst);
    answer
}

/// `app_restart_hold`: the restart that follows keeps the open chats.
pub fn hold() -> bool {
    crate::openchats::hold_for_restart();
    true
}
