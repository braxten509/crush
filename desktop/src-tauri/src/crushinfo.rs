//! `crush dirs` and `crush stats` for the window's project (desktop-design-5-6
//! 2.28, 6.4), and the one way SYS runs a crush command: the binary P3.1's
//! `crush_bin::resolve` finds, the login environment, its output read and a
//! time limit. SYS owns this file (7.3).

use crate::error::{AppError, AppResult};
use serde::Serialize;
use std::io::Read;
use std::process::{Command, ExitStatus, Stdio};
use std::time::{Duration, Instant};
use tauri::{AppHandle, Manager};

#[derive(Serialize)]
pub struct Dirs {
    pub lines: Vec<String>,
    /// The project's crush.log and the crash report folder (About, 2.28).
    pub log: String,
    pub crashes: String,
}

#[derive(Serialize)]
pub struct Stats {
    pub ok: bool,
    pub lines: Vec<String>,
}

pub struct Ran {
    pub status: ExitStatus,
    pub out: String,
    pub err: String,
}

/// Runs `crush <args>` and waits at most `limit`.
pub fn run_crush(app: &AppHandle, args: &[&str], limit: Duration) -> AppResult<Ran> {
    let env = crate::login_env::login_env();
    let setting = app.state::<crate::state::AppState>().settings.lock().unwrap().crush_path.clone();
    let path = env.iter().find(|(name, _)| name == "PATH").map(|(_, value)| value.as_str());
    let bin = crate::crush_bin::resolve(setting.as_deref(), path)?;
    let mut child = Command::new(&bin.path)
        .args(args)
        .envs(env.iter().map(|(k, v)| (k.as_str(), v.as_str())))
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .map_err(|e| AppError::new("crush_failed", format!("could not run crush: {e}")))?;
    // Read both pipes on their own threads so a chatty child never blocks.
    let mut stdout = child.stdout.take();
    let mut stderr = child.stderr.take();
    let out = std::thread::spawn(move || {
        let mut text = String::new();
        if let Some(pipe) = stdout.as_mut() {
            let _ = pipe.read_to_string(&mut text);
        }
        text
    });
    let err = std::thread::spawn(move || {
        let mut text = String::new();
        if let Some(pipe) = stderr.as_mut() {
            let _ = pipe.read_to_string(&mut text);
        }
        text
    });
    let started = Instant::now();
    let status = loop {
        match child.try_wait() {
            Ok(Some(status)) => break status,
            Ok(None) if started.elapsed() >= limit => {
                let _ = child.kill();
                let _ = child.wait();
                return Err(AppError::new("crush_failed", format!("crush {} took longer than {} s", args.first().unwrap_or(&""), limit.as_secs())));
            }
            Ok(None) => std::thread::sleep(Duration::from_millis(50)),
            Err(e) => return Err(AppError::new("crush_failed", e.to_string())),
        }
    };
    Ok(Ran { status, out: out.join().unwrap_or_default(), err: err.join().unwrap_or_default() })
}

fn project(app: &AppHandle, label: &str) -> AppResult<String> {
    crate::windows::launch_for(app, label)
        .map(|launch| launch.cwd.to_string_lossy().into_owned())
        .ok_or_else(|| AppError::new("no_window", "this window has no project"))
}

fn lines(text: &str) -> Vec<String> {
    text.lines().map(str::trim_end).filter(|l| !l.trim().is_empty()).map(String::from).collect()
}

/// `crush_dirs`: one folder per line.
pub fn dirs(app: &AppHandle, label: &str) -> AppResult<Dirs> {
    let cwd = project(app, label)?;
    let ran = run_crush(app, &["dirs", "--cwd", &cwd], Duration::from_secs(5))?;
    if !ran.status.success() {
        return Err(AppError::new("crush_failed", lines(&ran.err).pop().unwrap_or_else(|| "crush dirs failed".into())));
    }
    let log = crate::logs::log_path(app, label).map(|p| p.to_string_lossy().into_owned()).unwrap_or_default();
    Ok(Dirs { lines: lines(&ran.out), log, crashes: crate::crash::folder().to_string_lossy().into_owned() })
}

/// `crush_stats`: crush writes the report and opens it itself.
pub fn stats(app: &AppHandle, label: &str) -> AppResult<Stats> {
    let cwd = project(app, label)?;
    let ran = run_crush(app, &["stats", "--cwd", &cwd], Duration::from_secs(60))?;
    let mut all = lines(&ran.out);
    all.extend(lines(&ran.err));
    Ok(Stats { ok: ran.status.success(), lines: all })
}
