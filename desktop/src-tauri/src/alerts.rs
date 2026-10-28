//! Notifications, sounds and the app-wide window states (desktop-design-5-6
//! 2.18, 6.4). One table for every window: its alert state, project, shown
//! chat, focus and the chats it sees waiting for an answer. From it, the
//! badge counts the chats waiting anywhere in the app; the progress shows
//! while any works; attention is asked when
//! a window becomes blocked while not focused. SYS owns this file (7.3).

use crate::error::{AppError, AppResult};
use serde::Serialize;
use serde_json::json;
use std::collections::{HashMap, HashSet};
use std::path::Path;
use std::process::{Command, Stdio};
use std::sync::Mutex;
use std::time::{Duration, Instant};
use crate::host::AppHandle;

#[derive(Clone)]
struct WindowState {
    state: String,
    project: String,
    session: String,
    /// When the window first reported, so the earliest window leads.
    since: Instant,
    /// The open chats, of every folder, the window sees waiting for a
    /// question or permission answer.
    waiting: Vec<String>,
}

/// The badge: each chat waiting for an answer anywhere in the app, once
/// (owner, 2026-10-25). A blocked window counts its shown chat, or itself
/// when it shows none (the AWS sign-in sheet on the landing page).
fn badge_count<'a>(windows: impl Iterator<Item = &'a WindowState> + Clone) -> u32 {
    let mut chats: HashSet<&str> = windows.clone().flat_map(|w| w.waiting.iter().map(String::as_str)).collect();
    let mut extra = 0;
    for w in windows.filter(|w| w.state == "blocked") {
        if w.session.is_empty() {
            extra += 1;
        } else {
            chats.insert(w.session.as_str());
        }
    }
    chats.len() as u32 + extra
}

/// The window states behind the badge and progress (managed by main.rs).
#[derive(Default)]
pub struct Alerts {
    windows: Mutex<HashMap<String, WindowState>>,
}

impl Alerts {
    /// What each open window reports showing (label, folder, chat, when it
    /// first reported), earliest first.
    fn reported(&self, open: &[String]) -> Vec<(String, String, String, Instant)> {
        let windows = self.windows.lock().unwrap();
        let mut out: Vec<(String, String, String, Instant)> = windows
            .iter()
            .filter(|(label, _)| open.contains(label))
            .map(|(label, w)| (label.clone(), w.project.clone(), w.session.clone(), w.since))
            .collect();
        out.sort_by_key(|(_, _, _, since)| *since);
        out
    }

    /// The folder each open window reports showing (watch.rs), by label.
    pub fn projects(&self, open: &[String]) -> Vec<(String, String)> {
        self.reported(open).into_iter().map(|(label, project, _, _)| (label, project)).collect()
    }
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

/// `alert_notify`. `session` and `project` name the chat a click on the
/// toast opens (owner, 2026-10-28); Electron's main side does the opening.
pub fn notify(app: &AppHandle, title: &str, body: &str, session: Option<&str>, project: Option<&str>) -> AppResult<Notified> {
    let state = app.state::<crate::state::AppState>();
    if let Some(file) = state.launch.probe_side_file("alerts.jsonl") {
        let at = std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).map(|d| d.as_millis()).unwrap_or(0);
        crate::state::append_json_line(&file, &json!({ "kind": "notify", "title": title, "body": body, "session": session, "project": project, "at": at as u64 }));
        return Ok(Notified { sent: true, error: None });
    }
    match app.ask("notify", json!({"title": title, "body": body, "session": session, "project": project}), Some(std::time::Duration::from_secs(5))) {
        Ok(_) => Ok(Notified { sent: true, error: None }),
        Err(e) => Ok(Notified { sent: false, error: Some(e) }),
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
pub fn window_state(app: &AppHandle, label: &str, state: &str, project_dir: &str, session_id: &str, focused: bool, waiting: Vec<String>) -> AppResult<()> {
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
        let since = windows.get(label).map_or_else(Instant::now, |w| w.since);
        windows.insert(label.to_string(), WindowState { state: state.to_string(), project: project_dir.to_string(), session: session_id.to_string(), since, waiting });
        let badge = badge_count(windows.values());
        let working = windows.values().any(|w| w.state == "working");
        (badge, working, state == "blocked" && !was_blocked && !focused)
    };
    crate::taskbar::apply(app, Some(badge), working.then_some("indeterminate"), attention.then_some(label))
}

/// A window is gone: it leaves the table and the taskbar follows.
pub fn window_gone(app: &AppHandle, label: &str) {
    let alerts = app.state::<Alerts>();
    let (badge, working) = {
        let mut windows = alerts.windows.lock().unwrap();
        if windows.remove(label).is_none() {
            return;
        }
        (badge_count(windows.values()), windows.values().any(|w| w.state == "working"))
    };
    let _ = crate::taskbar::apply(app, Some(badge), working.then_some("indeterminate"), None);
}

/// `alert_handler`: the window that shows the chat, else the window showing
/// its folder, else the project's lead window.
pub fn handler(app: &AppHandle, project_dir: &str, session_id: &str) -> AppResult<String> {
    let open: Vec<String> = app.webview_windows().keys().cloned().collect();
    if let Some(label) = pick_handler(&app.state::<Alerts>().reported(&open), project_dir, session_id) {
        return Ok(label);
    }
    crate::windows::lead_window(app, Path::new(project_dir)).ok_or_else(|| AppError::new("no_window", "no window shows this project"))
}

/// pick_handler chooses from the windows that reported what they show (label,
/// folder, chat, when): the one showing the chat, else the earliest showing
/// its folder. None when no window reports the folder.
fn pick_handler(reported: &[(String, String, String, Instant)], project_dir: &str, session_id: &str) -> Option<String> {
    let mut reported = reported.to_vec();
    reported.sort_by_key(|(_, _, _, since)| *since);
    let showing_chat = reported.iter().find(|(_, project, session, _)| !session_id.is_empty() && session == session_id && same_folder(project, project_dir));
    showing_chat
        .or_else(|| reported.iter().find(|(_, project, _, _)| same_folder(project, project_dir)))
        .map(|(label, ..)| label.clone())
}

fn same_folder(a: &str, b: &str) -> bool {
    let canon = |p: &str| std::fs::canonicalize(p).unwrap_or_else(|_| p.into());
    a == b || canon(a) == canon(b)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn shown(label: &str, project: &str, session: &str, order: u64) -> (String, String, String, Instant) {
        let base = Instant::now();
        (label.to_string(), project.to_string(), session.to_string(), base + Duration::from_millis(order))
    }

    // A chat that is not the selected one still finds the window showing its
    // folder, from the folder the window reports (owner, 2026-10-22: dings
    // missed for chats that aren't selected).
    #[test]
    fn picks_the_window_that_reports_the_chats_folder() {
        let reported = vec![shown("w1", "/p/a", "s-a", 1), shown("w2", "/p/b", "s-b", 2)];
        assert_eq!(pick_handler(&reported, "/p/b", "s-other").as_deref(), Some("w2"));
        assert_eq!(pick_handler(&reported, "/p/a", "s-other").as_deref(), Some("w1"));
    }

    // The window showing the chat wins over one that shows only its folder,
    // whichever reported first.
    #[test]
    fn picks_the_window_showing_the_chat_first() {
        let reported = vec![shown("w1", "/p/a", "s-a", 1), shown("w2", "/p/a", "s-a", 2)];
        assert_eq!(pick_handler(&reported, "/p/a", "s-a").as_deref(), Some("w1"));
        let reported = vec![shown("w1", "/p/a", "s-b", 1), shown("w2", "/p/a", "s-a", 2)];
        assert_eq!(pick_handler(&reported, "/p/a", "s-a").as_deref(), Some("w2"));
    }

    fn state(state: &str, session: &str, waiting: &[&str]) -> WindowState {
        WindowState {
            state: state.to_string(),
            project: String::new(),
            session: session.to_string(),
            since: Instant::now(),
            waiting: waiting.iter().map(|s| s.to_string()).collect(),
        }
    }

    // The badge counts chats waiting for an answer anywhere in the app, each
    // once however many windows list it (owner, 2026-10-25). A blocked window
    // whose wait no list names yet (the AWS sign-in sheet) counts too; done
    // and error don't.
    #[test]
    fn badge_counts_each_waiting_chat_once() {
        let windows = [
            state("idle", "s-a", &["s-b", "s-c"]),
            state("done", "s-x", &["s-c", "s-d"]),
            state("blocked", "s-b", &["s-b", "s-c"]),
            state("error", "s-y", &[]),
        ];
        assert_eq!(badge_count(windows.iter()), 3);
        let windows = [state("blocked", "", &[]), state("blocked", "s-a", &["s-a"])];
        assert_eq!(badge_count(windows.iter()), 2);
        assert_eq!(badge_count([state("done", "s-a", &[])].iter()), 0);
    }

    // No window reports the folder: the caller falls back to the launch
    // registry.
    #[test]
    fn none_when_no_window_reports_the_folder() {
        let reported = vec![shown("w1", "/p/a", "s-a", 1)];
        assert_eq!(pick_handler(&reported, "/p/c", "s-c"), None);
        assert_eq!(pick_handler(&[], "/p/c", "s-c"), None);
    }
}
