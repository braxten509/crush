//! The taskbar badge, progress and attention (desktop-design-5-6 2.18,
//! NOTE-007, NOTE-008), applied to every window of the app (the badge and the
//! progress belong to the app's icon). A desktop that does not show them
//! ignores the calls. With --probe each call is also recorded. SYS owns this
//! file (7.3).

use crate::error::AppResult;
use serde_json::json;
use crate::host::AppHandle;

/// Applies the app-wide badge count and progress, and asks for attention on
/// the window labelled `attention` alone: the one that became blocked.
pub fn apply(app: &AppHandle, badge: Option<u32>, progress: Option<&str>, attention: Option<&str>) -> AppResult<()> {
    let state = app.state::<crate::state::AppState>();
    if let Some(file) = state.launch.probe_side_file("alerts.jsonl") {
        crate::state::append_json_line(&file, &json!({ "kind": "taskbar", "badge": badge, "progress": progress, "attention": attention.is_some(), "window": attention }));
    }
    for (label, window) in app.webview_windows() {
        let _ = window.set_taskbar(badge.filter(|n| *n > 0).map(i64::from), progress.is_some());
        if attention == Some(label.as_str()) && !window.is_focused().unwrap_or(false) {
            let _ = window.request_user_attention();
        }
    }
    Ok(())
}
