//! Windows per project (desktop-design-5-6 6.4, 7.1; scaffold S5, complete).
//! One window shows one project and has its own engine connection. The first
//! window is the configured `main`; `window_open` focuses the window that
//! already shows a folder, else opens `w<n>` for it. A second process hands
//! its folder over through the single-instance plug-in (`handoff`) and exits.
//!
//! Closing: the system close button sends `close-requested` to the window,
//! whose page runs its close steps (`lib/closing.js`) and then calls
//! `window_close`; when it has not done so 10 s later, `close` runs anyway.
//! `close` keeps S3's order: leave the engine first, then destroy the window.
//! The app ends with its last window (LIST-017).

use crate::error::{AppError, AppResult};
use crate::state::{AppState, Launch};
use crate::{connection, fixture};
use serde::Serialize;
use std::collections::{HashMap, HashSet};
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use std::time::{Duration, Instant};
use tauri::{AppHandle, Emitter, Manager};

/// The configured first window (tauri.conf.json).
pub const FIRST: &str = "main";

/// How long a window may take over its close steps before Rust closes it (6.4, inferred bound).
pub const CLOSE_FALLBACK: Duration = Duration::from_secs(10);

/// How a window was opened: what its `app_info` reports.
#[derive(Debug, Clone, PartialEq)]
pub struct WindowLaunch {
    /// The project folder as given (absolute).
    pub cwd: PathBuf,
    pub session: Option<String>,
    pub continue_last: bool,
    pub new_chat: bool,
    /// The phone launch this window starts sharing for (REM-009); reported once.
    pub launch_id: Option<String>,
    pub opened_at: Instant,
}

impl WindowLaunch {
    /// The first window's, from the command line.
    pub fn first(launch: &Launch) -> Self {
        WindowLaunch {
            cwd: launch.cwd.clone(),
            session: launch.session.clone(),
            continue_last: launch.continue_last,
            new_chat: launch.new_chat,
            launch_id: launch.launch_id.clone(),
            opened_at: Instant::now(),
        }
    }
}

struct Entry {
    launch: WindowLaunch,
    /// The folder compared between windows: links resolved.
    project: PathBuf,
    /// `app_info` already gave this window its launch ID.
    launch_id_taken: bool,
}

#[derive(Default)]
struct Inner {
    by_label: HashMap<String, Entry>,
    /// The last `w<n>` handed out.
    last: u32,
    /// Windows whose close has begun.
    closing: HashSet<String>,
}

/// Window label → how it was opened. Managed by main.rs.
#[derive(Default)]
pub struct Windows {
    inner: Mutex<Inner>,
}

/// What `window_open` does for a folder.
#[derive(Debug, PartialEq)]
pub enum OpenPlan {
    /// A window already shows it.
    Focus(String),
    /// A new window with this label; it is already listed.
    Create(String),
}

#[derive(Serialize, Debug, PartialEq)]
pub struct Opened {
    pub label: String,
    pub created: bool,
}

/// The payload of `window-request` (6.4).
#[derive(Serialize, Clone, Debug, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct WindowRequest {
    pub session_id: Option<String>,
    pub new_chat: bool,
    pub launch_id: Option<String>,
}

/// A folder as windows compare it: links resolved, else as given.
pub fn resolved(path: &Path) -> PathBuf {
    std::fs::canonicalize(path).unwrap_or_else(|_| path.to_path_buf())
}

impl Windows {
    /// The registry with the first window in it.
    pub fn new(first: WindowLaunch) -> Self {
        let windows = Windows::default();
        windows.insert(FIRST, first);
        windows
    }

    fn inner(&self) -> std::sync::MutexGuard<'_, Inner> {
        self.inner.lock().unwrap()
    }

    fn insert(&self, label: &str, launch: WindowLaunch) {
        let project = resolved(&launch.cwd);
        self.inner().by_label.insert(
            label.to_string(),
            Entry { launch, project, launch_id_taken: false },
        );
    }

    /// Focus the window that shows `project`, or list a new one for it. The
    /// check and the listing happen under one lock, so two opens of one
    /// folder never make two windows.
    pub fn plan_open(&self, project: &Path, session: Option<String>, new_chat: bool, launch_id: Option<String>) -> OpenPlan {
        let wanted = resolved(project);
        let mut inner = self.inner();
        let mut showing: Vec<(&String, &Entry)> = inner
            .by_label
            .iter()
            .filter(|(label, entry)| entry.project == wanted && !inner.closing.contains(*label))
            .collect();
        showing.sort_by_key(|(_, entry)| entry.launch.opened_at);
        if let Some((label, _)) = showing.first() {
            return OpenPlan::Focus((*label).clone());
        }
        let label = loop {
            inner.last += 1;
            let label = format!("w{}", inner.last + 1);
            if !inner.by_label.contains_key(&label) {
                break label;
            }
        };
        let launch = WindowLaunch {
            cwd: project.to_path_buf(),
            session,
            continue_last: false,
            new_chat,
            launch_id,
            opened_at: Instant::now(),
        };
        inner.by_label.insert(label.clone(), Entry { launch, project: wanted, launch_id_taken: false });
        OpenPlan::Create(label)
    }

    /// How the window `label` was opened.
    pub fn launch_for(&self, label: &str) -> Option<WindowLaunch> {
        self.inner().by_label.get(label).map(|entry| entry.launch.clone())
    }

    /// The window's launch ID the first time it is asked, then None (REM-009).
    pub fn take_launch_id(&self, label: &str) -> Option<String> {
        let mut inner = self.inner();
        let entry = inner.by_label.get_mut(label)?;
        if entry.launch_id_taken {
            return None;
        }
        entry.launch_id_taken = true;
        entry.launch.launch_id.clone()
    }

    /// The earliest-opened window of `project` still open (2.18, LIST-052).
    pub fn lead_window(&self, project: &Path) -> Option<String> {
        let wanted = resolved(project);
        let inner = self.inner();
        inner
            .by_label
            .iter()
            .filter(|(label, entry)| entry.project == wanted && !inner.closing.contains(*label))
            .min_by_key(|(_, entry)| entry.launch.opened_at)
            .map(|(label, _)| label.clone())
    }

    /// Records the window's new folder (`project_switch`); the page reloads and joins it.
    pub fn set_project(&self, label: &str, project: &Path) {
        let mut inner = self.inner();
        if let Some(entry) = inner.by_label.get_mut(label) {
            entry.launch.cwd = project.to_path_buf();
            entry.launch.session = None;
            entry.launch.continue_last = false;
            entry.launch.new_chat = false;
            entry.project = resolved(project);
        }
    }

    /// Marks the window as closing; false when its close already began.
    pub fn begin_close(&self, label: &str) -> bool {
        self.inner().closing.insert(label.to_string())
    }

    pub fn is_closing(&self, label: &str) -> bool {
        self.inner().closing.contains(label)
    }

    /// The window is gone.
    pub fn remove(&self, label: &str) {
        let mut inner = self.inner();
        inner.by_label.remove(label);
        inner.closing.remove(label);
    }
}

/// `launch_for` on the app's registry.
pub fn launch_for(app: &AppHandle, label: &str) -> Option<WindowLaunch> {
    app.try_state::<Windows>().and_then(|w| w.launch_for(label))
}

/// `lead_window` on the app's registry.
#[allow(dead_code)] // SYS's alert_handler (alerts.rs) calls it
pub fn lead_window(app: &AppHandle, project: &Path) -> Option<String> {
    app.try_state::<Windows>().and_then(|w| w.lead_window(project))
}

/// `window_open` (6.4): focus the window that shows `project_dir`, else open one for it.
pub fn open(
    app: &AppHandle,
    project_dir: &str,
    session_id: Option<String>,
    new_chat: bool,
    launch_id: Option<String>,
) -> AppResult<Opened> {
    if project_dir.is_empty() || !Path::new(project_dir).is_absolute() {
        return Err(AppError::new("bad_path", format!("Crush Desktop opens a window only for an absolute folder, not \"{project_dir}\".")));
    }
    let windows = app.state::<Windows>();
    let plan = windows.plan_open(Path::new(project_dir), session_id.clone(), new_chat, launch_id.clone());
    match plan {
        OpenPlan::Focus(label) => {
            if let Some(window) = app.get_webview_window(&label) {
                let _ = window.unminimize();
                let _ = window.show();
                let _ = window.set_focus();
            }
            let request = WindowRequest { session_id, new_chat, launch_id };
            if let Err(e) = app.emit_to(label.as_str(), "window-request", request) {
                eprintln!("crush-desktop: could not hand the request to window {label}: {e}");
            }
            Ok(Opened { label, created: false })
        }
        OpenPlan::Create(label) => {
            let built = app
                .config()
                .app
                .windows
                .iter()
                .find(|w| w.label == FIRST)
                .cloned()
                .ok_or_else(|| AppError::new("internal", "The app's window settings are missing."))
                .and_then(|mut config| {
                    config.label = label.clone();
                    tauri::WebviewWindowBuilder::from_config(app, &config)
                        .and_then(|builder| builder.build())
                        .map_err(|e| AppError::new("internal", format!("Crush Desktop could not open a window: {e}")))
                });
            match built {
                Ok(window) => {
                    crate::crash::watch_webview(&window);
                    Ok(Opened { label, created: true })
                }
                Err(e) => {
                    windows.remove(&label);
                    Err(e)
                }
            }
        }
    }
}

/// Ends the window `label`: leave its engine (DELETE /v1/clients/{id}) or its
/// fixture player first, then destroy it. A second call while the first runs
/// does nothing. The app exits with its last window.
pub async fn close(app: &AppHandle, label: &str) {
    let windows = app.state::<Windows>();
    if !windows.begin_close(label) {
        return;
    }
    leave_engine(app, label).await;
    if let Some(window) = app.get_webview_window(label) {
        let _ = window.destroy();
    }
    windows.remove(label);
}

/// The window's engine connection (or fixture player) ends; Q2 is the engine's job.
async fn leave_engine(app: &AppHandle, label: &str) {
    if let Some(fixture) = app.try_state::<fixture::Fixture>() {
        fixture.close(label);
        return;
    }
    connection::close(app, label).await;
}

/// `project_switch` (6.4): this window leaves its engine and takes `project_dir`.
pub async fn switch(app: &AppHandle, label: &str, project_dir: &str) -> AppResult<()> {
    if project_dir.is_empty() || !Path::new(project_dir).is_absolute() {
        return Err(AppError::new("bad_path", format!("Crush Desktop switches only to an absolute folder, not \"{project_dir}\".")));
    }
    leave_engine(app, label).await;
    app.state::<Windows>().set_project(label, Path::new(project_dir));
    Ok(())
}

/// The system close button: the page runs its close steps; after
/// CLOSE_FALLBACK, a window still open is closed anyway.
pub fn close_requested(app: &AppHandle, label: &str) {
    if app.state::<Windows>().is_closing(label) {
        return;
    }
    if let Err(e) = app.emit_to(label, "close-requested", ()) {
        eprintln!("crush-desktop: could not ask window {label} to close: {e}");
    }
    let (app, label) = (app.clone(), label.to_string());
    tauri::async_runtime::spawn(async move {
        let still_open = {
            let (app, label) = (app.clone(), label.clone());
            move || app.get_webview_window(&label).is_some() && !app.state::<Windows>().is_closing(&label)
        };
        if close_fallback(CLOSE_FALLBACK, still_open).await {
            eprintln!("crush-desktop: window {label} did not close itself within {} s; closing it", CLOSE_FALLBACK.as_secs());
            close(&app, &label).await;
        }
    });
}

/// Waits `wait`, then answers whether the window still needs closing.
pub async fn close_fallback(wait: Duration, still_open: impl Fn() -> bool) -> bool {
    tokio::time::sleep(wait).await;
    still_open()
}

/// The single-instance hand-off (REM-017, LIST-049): a second process's
/// `--project`/`--cwd`, `--launch-id` and `--new-chat` open or focus a window.
pub fn handoff(app: &AppHandle, argv: Vec<String>, cwd: String) {
    let args: Vec<String> = argv.into_iter().skip(1).collect();
    let here = PathBuf::from(&cwd);
    let launch = match crate::parse_launch(&args, &here, false) {
        Ok(launch) => launch,
        Err(message) => {
            eprintln!("crush-desktop: a second start was refused: {message}");
            return;
        }
    };
    let project = launch.cwd.to_string_lossy().into_owned();
    if let Err(e) = open(app, &project, None, launch.new_chat, launch.launch_id) {
        eprintln!("crush-desktop: a second start could not open {project}: {} ({})", e.message, e.code);
    }
}

/// The calling window's probe file: the first window's is the probe file,
/// every other window's is `<probe file>.<label>` (8.2).
pub fn probe_file(probe: &Path, label: &str) -> PathBuf {
    if label == FIRST {
        return probe.to_path_buf();
    }
    let mut name = probe.as_os_str().to_owned();
    name.push(".");
    name.push(label);
    PathBuf::from(name)
}

/// `app_info`'s window fields for `label`; the first window falls back to the command line.
pub fn info_launch(state: &AppState, windows: Option<&Windows>, label: &str) -> WindowLaunch {
    windows
        .and_then(|w| w.launch_for(label))
        .unwrap_or_else(|| WindowLaunch::first(&state.launch))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn launch(cwd: &Path) -> WindowLaunch {
        WindowLaunch {
            cwd: cwd.to_path_buf(),
            session: None,
            continue_last: false,
            new_chat: false,
            launch_id: None,
            opened_at: Instant::now(),
        }
    }

    fn scratch(name: &str) -> PathBuf {
        let dir = std::env::temp_dir().join(format!("crush-desktop-windows-{name}-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(dir.join("proj")).unwrap();
        std::fs::create_dir_all(dir.join("proj2")).unwrap();
        dir
    }

    #[test]
    fn windows_open_focuses_existing() {
        let dir = scratch("focus");
        let proj = dir.join("proj");
        let windows = Windows::new(launch(&proj));
        // The same folder, also through a link, focuses the first window.
        assert_eq!(windows.plan_open(&proj, None, false, None), OpenPlan::Focus(FIRST.into()));
        std::os::unix::fs::symlink(&proj, dir.join("link")).unwrap();
        assert_eq!(windows.plan_open(&dir.join("link"), None, false, None), OpenPlan::Focus(FIRST.into()));
        // Another folder gets a new window, listed at once, with what it was opened with.
        let proj2 = dir.join("proj2");
        let plan = windows.plan_open(&proj2, Some("s-1".into()), true, Some("launch-1".into()));
        assert_eq!(plan, OpenPlan::Create("w2".into()));
        let w2 = windows.launch_for("w2").unwrap();
        assert_eq!((w2.cwd.as_path(), w2.session.as_deref(), w2.new_chat), (proj2.as_path(), Some("s-1"), true));
        // A second open of it focuses that window and opens no third.
        assert_eq!(windows.plan_open(&proj2, None, false, None), OpenPlan::Focus("w2".into()));
        assert_eq!(windows.lead_window(&proj2).as_deref(), Some("w2"));
        // The launch ID is given once.
        assert_eq!(windows.take_launch_id("w2").as_deref(), Some("launch-1"));
        assert_eq!(windows.take_launch_id("w2"), None);
        // A closing window is no longer focused; the next window gets a new label.
        assert!(windows.begin_close("w2"));
        assert!(!windows.begin_close("w2"), "a second close does nothing");
        assert_eq!(windows.plan_open(&proj2, None, false, None), OpenPlan::Create("w3".into()));
        windows.remove("w2");
        assert_eq!(windows.lead_window(&proj2).as_deref(), Some("w3"));
        std::fs::remove_dir_all(&dir).unwrap();
    }

    #[test]
    fn close_requested_fallback_closes() {
        let run = |still_open: bool| {
            tauri::async_runtime::block_on(close_fallback(Duration::from_millis(30), move || still_open))
        };
        let started = Instant::now();
        assert!(run(true), "a window that did not close itself is closed");
        assert!(started.elapsed() >= Duration::from_millis(30), "only after the wait");
        assert!(!run(false), "a window that closed itself is left alone");
        // A window whose close began is not open for the fallback.
        let windows = Windows::new(launch(Path::new("/tmp")));
        windows.begin_close(FIRST);
        assert!(windows.is_closing(FIRST));
    }
}
