//! Notifications, sounds and the app-wide window states (desktop-design-5-6
//! 2.18, 6.4). Placeholder from scaffold S5; SYS owns this file (7.3).

use crate::error::{AppError, AppResult};
use serde::Serialize;
use tauri::AppHandle;

/// The window states behind the badge and progress (managed by main.rs).
#[derive(Default)]
pub struct Alerts {}

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

/// `alert_notify`.
pub fn notify(_app: &AppHandle, _title: &str, _body: &str) -> AppResult<Notified> {
    Err(AppError::not_built("alert_notify"))
}

/// `alert_sound`: the player chain, 5 s for the whole chain; "bell" is the bell style.
pub fn sound(_app: &AppHandle, _id: &str) -> AppResult<Sounded> {
    Err(AppError::not_built("alert_sound"))
}

/// `alert_window_state`: state is "idle", "working", "blocked", "done" or "error".
pub fn window_state(
    _app: &AppHandle,
    _label: &str,
    _state: &str,
    _project_dir: &str,
    _session_id: &str,
    _focused: bool,
) -> AppResult<()> {
    Err(AppError::not_built("alert_window_state"))
}

/// `alert_handler`: the window that shows the chat, else the project's lead window.
pub fn handler(_app: &AppHandle, _project_dir: &str, _session_id: &str) -> AppResult<String> {
    Err(AppError::not_built("alert_handler"))
}
