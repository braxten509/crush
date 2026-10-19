//! Files beside the config the settings sheets read and write
//! (desktop-design-5-6 2.12, 2.27, section 4), with the engine's own path
//! rules and the login environment the engine gets:
//! `subagent-instructions.md` beside the global crush.json
//! (internal/config/subagent_instructions.go) and `remote-password` beside
//! the global data crush.json (internal/cmd/remote.go:26). SET owns this file
//! (7.3).

use crate::error::{AppError, AppResult};
use std::io::Write;
use std::path::PathBuf;

fn var(name: &str) -> Option<String> {
    let env = crate::login_env::login_env();
    env.iter()
        .find(|(k, v)| k == name && !v.is_empty())
        .map(|(_, v)| v.clone())
        .or_else(|| std::env::var(name).ok().filter(|v| !v.is_empty()))
}

fn home() -> PathBuf {
    var("HOME").map(PathBuf::from).or_else(dirs::home_dir).unwrap_or_default()
}

/// The folder of the global crush.json: `$CRUSH_GLOBAL_CONFIG`, else
/// `$XDG_CONFIG_HOME/crush`, else `~/.config/crush` (paths.go:18-23).
fn global_config_dir() -> PathBuf {
    if let Some(dir) = var("CRUSH_GLOBAL_CONFIG") {
        return PathBuf::from(dir);
    }
    var("XDG_CONFIG_HOME").map(PathBuf::from).unwrap_or_else(|| home().join(".config")).join("crush")
}

/// The folder of the global data crush.json (paths.go:65-85).
fn global_data_dir() -> PathBuf {
    if let Some(dir) = var("CRUSH_GLOBAL_DATA") {
        return PathBuf::from(dir);
    }
    if let Some(dir) = var("XDG_DATA_HOME") {
        return PathBuf::from(dir).join("crush");
    }
    home().join(".local").join("share").join("crush")
}

fn instructions_path() -> PathBuf {
    global_config_dir().join("subagent-instructions.md")
}

/// `subagent_instructions_read`: "" when there are none.
pub fn subagent_instructions_read() -> AppResult<String> {
    match std::fs::read_to_string(instructions_path()) {
        Ok(text) => Ok(text.trim().to_string()),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(String::new()),
        Err(e) => Err(AppError::new("read_failed", format!("{}: {e}", instructions_path().display()))),
    }
}

/// `subagent_instructions_write`: trimmed, one final newline, written to
/// `<path>.tmp` and renamed, as SaveSubAgentInstructions.
pub fn subagent_instructions_write(text: &str) -> AppResult<()> {
    let path = instructions_path();
    let fail = |e: std::io::Error| AppError::new("write_failed", format!("{}: {e}", path.display()));
    if let Some(dir) = path.parent() {
        std::fs::create_dir_all(dir).map_err(fail)?;
    }
    let mut body = text.trim().to_string();
    if !body.is_empty() {
        body.push('\n');
    }
    let tmp = PathBuf::from(format!("{}.tmp", path.display()));
    {
        let mut out = std::fs::File::create(&tmp).map_err(fail)?;
        out.write_all(body.as_bytes()).map_err(fail)?;
        out.sync_all().map_err(fail)?;
    }
    std::fs::rename(&tmp, &path).map_err(|e| {
        let _ = std::fs::remove_file(&tmp);
        fail(e)
    })
}

/// `remote_password_exists`: presence only; the file is never read.
pub fn remote_password_exists() -> AppResult<bool> {
    Ok(global_data_dir().join("remote-password").is_file())
}
