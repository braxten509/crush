//! The taskbar badge, progress and attention (desktop-design-5-6 2.18,
//! NOTE-007, NOTE-008), applied to every window of the app (the badge and the
//! progress belong to the app's icon). A desktop that does not show them
//! ignores the calls. With --probe each call is also recorded. SYS owns this
//! file (7.3).

use crate::error::AppResult;
use serde_json::json;
use tauri::window::{ProgressBarState, ProgressBarStatus};
use tauri::UserAttentionType;
use tauri::{AppHandle, Manager};

/// Applies the app-wide badge count and progress, and asks for attention on
/// the window labelled `attention` alone: the one that became blocked.
pub fn apply(app: &AppHandle, badge: Option<u32>, progress: Option<&str>, attention: Option<&str>) -> AppResult<()> {
    let state = app.state::<crate::state::AppState>();
    if let Some(file) = state.launch.probe_side_file("alerts.jsonl") {
        crate::state::append_json_line(&file, &json!({ "kind": "taskbar", "badge": badge, "progress": progress, "attention": attention.is_some(), "window": attention }));
    }
    let progress_state = || ProgressBarState {
        status: Some(if progress.is_some() { ProgressBarStatus::Indeterminate } else { ProgressBarStatus::None }),
        progress: None,
    };
    for (label, window) in app.webview_windows() {
        let _ = window.set_badge_count(badge.filter(|n| *n > 0).map(i64::from));
        let _ = window.set_progress_bar(progress_state());
        if attention == Some(label.as_str()) && !window.is_focused().unwrap_or(false) {
            let _ = window.request_user_attention(Some(UserAttentionType::Informational));
        }
    }
    Ok(())
}
