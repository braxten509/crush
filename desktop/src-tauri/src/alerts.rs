//! Notifications, sounds and the app-wide window states (desktop-design-5-6
//! 2.18, 6.4). One table for every window: its alert state, project, shown
//! chat and focus. From it, the badge counts windows that are blocked, done
//! or in error; the progress shows while any works; attention is asked when
//! a window becomes blocked while not focused. SYS owns this file (7.3).

use crate::error::{AppError, AppResult};
use serde::Serialize;
use serde_json::json;
use std::collections::HashMap;
use std::path::Path;
use std::process::{Command, Stdio};
use std::sync::Mutex;
use std::time::{Duration, Instant};
use tauri::{AppHandle, Manager};
use tauri_plugin_notification::NotificationExt;

#[derive(Clone)]
struct WindowState {
    state: String,
    project: String,
    session: String,
}

/// The window states behind the badge and progress (managed by main.rs).
#[derive(Default)]
pub struct Alerts {
    windows: Mutex<HashMap<String, WindowState>>,
}

#[derive(Serialize)]
pub struct Notified {
    pub sent: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
}

#[derive(Serialize)]
pub struct Sounded {
    /// The player that worked; None when none did.
    pub player: Option<String>,
}

const STATES: &[&str] = &["idle", "working", "blocked", "done", "error"];
const SOUND_LIMIT: Duration = Duration::from_secs(5);

/// `alert_notify`.
pub fn notify(app: &AppHandle, title: &str, body: &str) -> AppResult<Notified> {
    let state = app.state::<crate::state::AppState>();
    if let Some(file) = state.launch.probe_side_file("alerts.jsonl") {
        let at = std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).map(|d| d.as_millis()).unwrap_or(0);
        crate::state::append_json_line(&file, &json!({ "kind": "notify", "title": title, "body": body, "at": at as u64 }));
        return Ok(Notified { sent: true, error: None });
    }
    match app.notification().builder().title(title).body(body).show() {
        Ok(()) => Ok(Notified { sent: true, error: None }),
        Err(e) => Ok(Notified { sent: false, error: Some(e.to_string()) }),
    }
}

/// The player chain (internal/ui/notification/sound.go:17-64).
fn players(id: &str) -> Vec<(String, Vec<String>)> {
    if cfg!(target_os = "macos") {
        return vec![("afplay".into(), vec!["/System/Library/Sounds/Glass.aiff".into()])];
    }
    let sound = if id == "bell" { "bell" } else { id };
    let file = format!("/usr/share/sounds/freedesktop/stereo/{sound}.oga");
    vec![
        ("canberra-gtk-play".into(), vec!["--id".into(), sound.into(), "--description".into(), "Crush".into()]),
        ("pw-play".into(), vec![file.clone()]),
        ("paplay".into(), vec![file]),
    ]
}

/// `alert_sound`: the player chain, 5 s for the whole chain; "bell" is the bell style.
pub fn sound(_app: &AppHandle, id: &str) -> AppResult<Sounded> {
    if id.is_empty() || id.contains('/') || id.contains("..") {
        return Err(AppError::new("bad_sound", "unknown sound"));
    }
    let env = crate::login_env::login_env();
    let started = Instant::now();
    for (program, args) in players(id) {
        let left = SOUND_LIMIT.saturating_sub(started.elapsed());
        if left.is_zero() {
            break;
        }
        let Ok(mut child) = Command::new(&program)
            .args(&args)
            .envs(env.iter().map(|(k, v)| (k.as_str(), v.as_str())))
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
        else {
            continue; // a missing player is skipped
        };
        let ok = loop {
            match child.try_wait() {
                Ok(Some(status)) => break status.success(),
                Ok(None) if started.elapsed() >= SOUND_LIMIT => {
                    let _ = child.kill();
                    let _ = child.wait();
                    return Ok(Sounded { player: None });
                }
                Ok(None) => std::thread::sleep(Duration::from_millis(20)),
                Err(_) => break false,
            }
        };
        if ok {
            return Ok(Sounded { player: Some(program) });
        }
    }
    Ok(Sounded { player: None })
}

/// `alert_window_state`: state is "idle", "working", "blocked", "done" or "error".
pub fn window_state(app: &AppHandle, label: &str, state: &str, project_dir: &str, session_id: &str, focused: bool) -> AppResult<()> {
    if !STATES.contains(&state) {
        return Err(AppError::new("bad_state", format!("unknown alert state \"{state}\"")));
    }
    let alerts = app.state::<Alerts>();
    let (badge, working, attention) = {
        let mut windows = alerts.windows.lock().unwrap();
        // Windows that are gone leave the table.
        let open: Vec<String> = app.webview_windows().keys().cloned().collect();
        windows.retain(|l, _| open.contains(l));
        let was_blocked = windows.get(label).is_some_and(|w| w.state == "blocked");
        windows.insert(label.to_string(), WindowState { state: state.to_string(), project: project_dir.to_string(), session: session_id.to_string() });
        let badge = windows.values().filter(|w| matches!(w.state.as_str(), "blocked" | "done" | "error")).count() as u32;
        let working = windows.values().any(|w| w.state == "working");
        (badge, working, state == "blocked" && !was_blocked && !focused)
    };
    crate::taskbar::apply(app, Some(badge), working.then_some("indeterminate"), attention)
}

/// `alert_handler`: the window that shows the chat, else the project's lead window.
pub fn handler(app: &AppHandle, project_dir: &str, session_id: &str) -> AppResult<String> {
    {
        let windows = app.state::<Alerts>().windows.lock().unwrap().clone();
        let open: Vec<String> = app.webview_windows().keys().cloned().collect();
        if let Some((label, _)) = windows.iter().find(|(l, w)| open.contains(l) && !session_id.is_empty() && w.session == session_id && same_folder(&w.project, project_dir)) {
            return Ok(label.clone());
        }
    }
    crate::windows::lead_window(app, Path::new(project_dir)).ok_or_else(|| AppError::new("no_window", "no window shows this project"))
}

fn same_folder(a: &str, b: &str) -> bool {
    let canon = |p: &str| std::fs::canonicalize(p).unwrap_or_else(|_| p.into());
    a == b || canon(a) == canon(b)
}
