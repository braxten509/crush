//! The crash guard (desktop-design-5-6 2.28, MISC-025, START-020). Reports
//! go to the terminal's crash folder, `$XDG_STATE_HOME/crush/crashes` (else
//! `~/.local/state/crush/crashes`, mode 0700), as
//! `crash-desktop-<YYYYMMDD-HHMMSS>-<pid>.log` (mode 0600). A Rust panic
//! writes its message and backtrace, and the notice shows at the next start;
//! a crashed web view writes a report for its window and the window is
//! reloaded, so the notice shows at once. Each report is shown once (the
//! shown names are kept in `crash-desktop-shown`). SYS owns this file (7.3).

use crate::error::{AppError, AppResult};
use serde::Serialize;
use std::collections::HashMap;
use std::io::Write;
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use std::time::{Duration, SystemTime};
use crate::host::AppHandle;

#[derive(Serialize, Clone)]
pub struct Pending {
    pub path: String,
    /// "panic" or "webview".
    pub kind: String,
}

const SHOWN: &str = "crash-desktop-shown";

/// Web view crashes of this run, by window label.
static WEBVIEW: Mutex<Option<HashMap<String, Pending>>> = Mutex::new(None);
/// Held for a whole `pending` call, so two windows never claim one report.
static CLAIM: Mutex<()> = Mutex::new(());

pub fn folder() -> PathBuf {
    let state = std::env::var_os("XDG_STATE_HOME")
        .map(PathBuf::from)
        .filter(|p| p.is_absolute())
        .unwrap_or_else(|| dirs::home_dir().unwrap_or_default().join(".local").join("state"));
    state.join("crush").join("crashes")
}

fn stamp() -> String {
    let secs = SystemTime::now().duration_since(SystemTime::UNIX_EPOCH).map(|d| d.as_secs()).unwrap_or(0);
    // Local time is not available without a time crate; UTC keeps names sortable.
    let (days, rest) = (secs / 86_400, secs % 86_400);
    let (y, m, d) = civil(days as i64);
    format!("{y:04}{m:02}{d:02}-{:02}{:02}{:02}", rest / 3600, rest % 3600 / 60, rest % 60)
}

fn civil(z: i64) -> (i64, u32, u32) {
    let z = z + 719_468;
    let era = z.div_euclid(146_097);
    let doe = z.rem_euclid(146_097);
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = (doy - (153 * mp + 2) / 5 + 1) as u32;
    let m = if mp < 10 { mp + 3 } else { mp - 9 } as u32;
    (if m <= 2 { yoe + era * 400 + 1 } else { yoe + era * 400 }, m, d)
}

fn write_report(text: &str) -> Option<PathBuf> {
    use std::os::unix::fs::{DirBuilderExt, OpenOptionsExt};
    let dir = folder();
    let _ = std::fs::DirBuilder::new().recursive(true).mode(0o700).create(&dir);
    let path = dir.join(format!("crash-desktop-{}-{}.log", stamp(), std::process::id()));
    let mut file = std::fs::OpenOptions::new().write(true).create(true).truncate(true).mode(0o600).open(&path).ok()?;
    file.write_all(text.as_bytes()).ok()?;
    Some(path)
}

/// The first statement of main(): drops empty reports older than a day,
/// then installs the panic report.
pub fn install() {
    if let Ok(entries) = std::fs::read_dir(folder()) {
        for entry in entries.flatten() {
            let Ok(meta) = entry.metadata() else { continue };
            let old = meta.modified().ok().and_then(|t| t.elapsed().ok()).is_some_and(|age| age > Duration::from_secs(86_400));
            let name = entry.file_name().to_string_lossy().into_owned();
            if meta.len() == 0 && old && name.starts_with("crash-") {
                let _ = std::fs::remove_file(entry.path());
            }
        }
    }
    let previous = std::panic::take_hook();
    std::panic::set_hook(Box::new(move |info| {
        let backtrace = std::backtrace::Backtrace::force_capture();
        let thread = std::thread::current();
        let text = format!(
            "Crush Desktop {} crashed\n\nthread '{}' {info}\n\n{backtrace}\n",
            env!("CARGO_PKG_VERSION"),
            thread.name().unwrap_or("<unnamed>")
        );
        let _ = write_report(&text);
        previous(info);
    }));
}

/// A window's page process ended (Electron's render-process-gone): the
/// report is written, kept for the page to show, and the page reloads.
pub fn webview_crashed(app: &AppHandle, label: &str, reason: &str) {
    let text = format!("Crush Desktop {}: the web view of window {label} ended ({reason})\n", env!("CARGO_PKG_VERSION"));
    if let Some(path) = write_report(&text) {
        WEBVIEW
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .get_or_insert_with(HashMap::new)
            .insert(label.to_string(), Pending { path: path.to_string_lossy().into_owned(), kind: "webview".into() });
    }
    if let Some(window) = app.get_webview_window(label) {
        let _ = window.eval("location.reload()");
    }
}

fn shown_list(dir: &Path) -> Vec<String> {
    std::fs::read_to_string(dir.join(SHOWN)).map(|t| t.lines().map(String::from).collect()).unwrap_or_default()
}

fn mark_shown(dir: &Path, name: &str) {
    use std::os::unix::fs::OpenOptionsExt;
    if let Ok(mut file) = std::fs::OpenOptions::new().append(true).create(true).mode(0o600).open(dir.join(SHOWN)) {
        let _ = writeln!(file, "{name}");
    }
}

/// `crash_pending`: each report once. A web view crash of the calling
/// window first, else the newest desktop panic report not shown yet.
pub fn pending(app: &AppHandle, label: &str) -> AppResult<Option<Pending>> {
    let _ = app;
    let _claim = CLAIM.lock().unwrap_or_else(|e| e.into_inner());
    let dir = folder();
    let own = WEBVIEW.lock().unwrap_or_else(|e| e.into_inner()).as_mut().and_then(|map| map.remove(label));
    if let Some(report) = own {
        if let Some(name) = Path::new(&report.path).file_name() {
            mark_shown(&dir, &name.to_string_lossy());
        }
        return Ok(Some(report));
    }
    let shown = shown_list(&dir);
    let mut reports: Vec<(SystemTime, PathBuf)> = std::fs::read_dir(&dir)
        .map(|entries| {
            entries
                .flatten()
                .filter(|e| {
                    let name = e.file_name().to_string_lossy().into_owned();
                    name.starts_with("crash-desktop-") && name.ends_with(".log") && !shown.contains(&name)
                })
                .filter_map(|e| Some((e.metadata().ok()?.modified().ok()?, e.path())))
                .filter(|(_, p)| std::fs::metadata(p).map(|m| m.len() > 0).unwrap_or(false))
                .collect()
        })
        .unwrap_or_default();
    reports.sort();
    let Some((_, newest)) = reports.pop() else { return Ok(None) };
    // Older unshown reports are not offered again either.
    for (_, path) in reports.iter().chain(std::iter::once(&(SystemTime::UNIX_EPOCH, newest.clone()))) {
        if let Some(name) = path.file_name() {
            mark_shown(&dir, &name.to_string_lossy());
        }
    }
    Ok(Some(Pending { path: newest.to_string_lossy().into_owned(), kind: "panic".into() }))
}

/// `crash_restart`: relaunches after a panic, or reloads the window after a web view crash.
pub fn restart(app: &AppHandle, label: &str) -> AppResult<()> {
    let window = app.get_webview_window(label).ok_or_else(|| AppError::new("no_window", "the window is gone"))?;
    window.eval("location.reload()").map_err(|e| AppError::new("restart_failed", e.to_string()))
}
