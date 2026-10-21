//! Rings for open chats in folders no window shows (owner, 2026-10-20: "if
//! console and desktop are open, please only ring ONCE - from the desktop
//! one"). The terminal runs each chat on its own, so it hears every folder;
//! a window hears only its own. Every few seconds this lists the folders of
//! the shared open chats (openchats.rs), drops those a window shows, finds
//! each one's running engine by its socket and listens to its workspace
//! with `watch=1`, a stream that holds nothing, so the engine still closes
//! when its own apps go. A finished turn plays "complete" and a question
//! "message-new-instant", as the terminal does (completion_notification.go,
//! update_workspace.go). The folders heard, shown or listened to, go into
//! the shared file, and a terminal in one of them stays quiet
//! (internal/openchats DesktopHears).

use crate::host::async_runtime as host;
use crate::host::AppHandle;
use crate::uds_http::EngineHttp;
use serde_json::{json, Value};
use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::time::Duration;

const TICK: Duration = Duration::from_secs(2);
const CALL_LIMIT: Duration = Duration::from_secs(3);

/// One folder's stream; dropping it stops the stream.
struct Listener {
    task: tokio::task::JoinHandle<()>,
    /// Set once the engine answered the stream.
    open: Arc<AtomicBool>,
}

impl Drop for Listener {
    fn drop(&mut self) {
        self.task.abort();
    }
}

/// Starts the watch (main.rs; not for fixture runs, which have no engine).
pub fn start(app: AppHandle) {
    std::thread::spawn(move || {
        let mut listeners: HashMap<PathBuf, Listener> = HashMap::new();
        let mut written: Option<Vec<String>> = None;
        loop {
            tick(&app, &mut listeners, &mut written);
            std::thread::sleep(TICK);
        }
    });
}

fn resolved(dir: &str) -> PathBuf {
    std::fs::canonicalize(dir).unwrap_or_else(|_| PathBuf::from(dir))
}

fn shown(app: &AppHandle) -> Vec<PathBuf> {
    app.try_state::<crate::windows::Windows>().map(|w| w.projects()).unwrap_or_default()
}

fn tick(app: &AppHandle, listeners: &mut HashMap<PathBuf, Listener>, written: &mut Option<Vec<String>>) {
    let shown = shown(app);
    let mut wanted: Vec<PathBuf> = Vec::new();
    for chat in crate::openchats::chats() {
        if chat.dir.is_empty() {
            continue;
        }
        let dir = resolved(&chat.dir);
        if !shown.contains(&dir) && !wanted.contains(&dir) {
            wanted.push(dir);
        }
    }
    // A stream that ended (its engine closed) starts again below when its
    // engine runs again.
    listeners.retain(|dir, l| wanted.contains(dir) && !l.task.is_finished());
    let missing: Vec<&PathBuf> = wanted.iter().filter(|d| !listeners.contains_key(*d)).collect();
    if !missing.is_empty() {
        let engines = host::block_on(engines());
        for dir in missing {
            if let Some((socket, workspace)) = engines.get(dir) {
                listeners.insert(dir.clone(), listen(app.clone(), dir.clone(), socket.clone(), workspace.clone()));
            }
        }
    }
    let mut heard: Vec<String> = shown
        .iter()
        .chain(listeners.iter().filter(|(_, l)| l.open.load(Ordering::SeqCst)).map(|(d, _)| d))
        .map(|d| d.to_string_lossy().into_owned())
        .collect();
    heard.sort();
    heard.dedup();
    if written.as_ref() != Some(&heard) && crate::openchats::set_dirs(&heard).is_ok() {
        *written = Some(heard);
    }
}

/// The folders where the engines run (internal/server/socketdir.go), each
/// with the engine's socket and workspace, from GET /v1/engine.
async fn engines() -> HashMap<PathBuf, (String, String)> {
    let mut found = HashMap::new();
    let mut dirs = Vec::new();
    if let Some(runtime) = std::env::var_os("XDG_RUNTIME_DIR").filter(|v| !v.is_empty()) {
        dirs.push(PathBuf::from(runtime).join("crush"));
    }
    // SAFETY: getuid cannot fail.
    dirs.push(std::env::temp_dir().join(format!("crush-{}", unsafe { libc::getuid() })));
    for dir in dirs {
        let Ok(entries) = std::fs::read_dir(&dir) else { continue };
        for entry in entries.flatten() {
            let name = entry.file_name().to_string_lossy().into_owned();
            if !(name.starts_with("engine-") && name.ends_with(".sock")) {
                continue;
            }
            let socket = entry.path().to_string_lossy().into_owned();
            let Ok(reply) = EngineHttp::new(&socket).call("GET", "/v1/engine", None, Some(CALL_LIMIT)).await else { continue };
            let info = reply.body;
            let workspace = info.get("workspace_id").and_then(Value::as_str).unwrap_or_default();
            let path = info.get("workspace_path").and_then(Value::as_str).unwrap_or_default();
            if workspace.is_empty() || path.is_empty() || info.get("closing").and_then(Value::as_bool) == Some(true) {
                continue;
            }
            found.insert(resolved(path), (socket, workspace.to_string()));
        }
    }
    found
}

fn listen(app: AppHandle, dir: PathBuf, socket: String, workspace: String) -> Listener {
    let open = Arc::new(AtomicBool::new(false));
    let opened = open.clone();
    let task = host::spawn(async move {
        let http = EngineHttp::new(&socket);
        let base = format!("/v1/workspaces/{}", encode(&workspace));
        let client = uuid::Uuid::new_v4().to_string();
        let Ok(mut stream) = http.stream(&format!("{base}/events?watch=1&client_id={client}")).await else { return };
        opened.store(true, Ordering::SeqCst);
        let mut lines = crate::sse::LineSplitter::new();
        let mut out = Vec::new();
        while let Some(Ok(chunk)) = stream.next_chunk().await {
            lines.push(&chunk, &mut out);
            for line in out.drain(..) {
                let Some(event) = line.strip_prefix("data:").and_then(|d| serde_json::from_str::<Value>(d.trim()).ok()) else { continue };
                if let Some(ring) = ring_for(&event) {
                    ring_once(&app, &http, &base, &dir, ring).await;
                }
            }
        }
    });
    Listener { task, open }
}

#[derive(Debug, PartialEq)]
enum Ring {
    Finished(String),
    Question(String),
}

/// What an event rings for: a finished turn, or a chat starting to wait for
/// question answers (prompt_pending is sent on each change only).
fn ring_for(event: &Value) -> Option<Ring> {
    let inner = event.pointer("/payload/payload")?;
    let session = inner.get("session_id").and_then(Value::as_str).unwrap_or_default().to_string();
    if session.is_empty() {
        return None;
    }
    match event.get("type").and_then(Value::as_str)? {
        "agent_event" if inner.get("type").and_then(Value::as_str) == Some("agent_finished") => Some(Ring::Finished(session)),
        "prompt_pending" if inner.get("kind").and_then(Value::as_str) == Some("question") && inner.get("pending").and_then(Value::as_bool) == Some(true) => {
            Some(Ring::Question(session))
        }
        _ => None,
    }
}

async fn ring_once(app: &AppHandle, http: &EngineHttp, base: &str, dir: &Path, ring: Ring) {
    // A window that shows the folder now rings for it itself.
    if shown(app).iter().any(|d| d == dir) {
        return;
    }
    let config = http.call("GET", &format!("{base}/config"), None, Some(CALL_LIMIT)).await.map(|r| r.body).unwrap_or(Value::Null);
    if config.pointer("/options/notifications").and_then(Value::as_str).is_some_and(|s| s.eq_ignore_ascii_case("disabled")) {
        return;
    }
    let (sound, session) = match &ring {
        Ring::Finished(s) => ("complete", s),
        Ring::Question(s) => ("message-new-instant", s),
    };
    if let Ring::Finished(_) = ring {
        // Only when the chat is idle with nothing queued, as the terminal checks.
        let run = format!("{base}/agent/sessions/{}", encode(session));
        let busy = http.call("GET", &run, None, Some(CALL_LIMIT)).await.map(|r| r.body.get("is_busy") == Some(&Value::Bool(true))).unwrap_or(true);
        let queued = http
            .call("GET", &format!("{run}/prompts/queued"), None, Some(CALL_LIMIT))
            .await
            .map(|r| r.body.as_i64().or_else(|| r.body.get("count").and_then(Value::as_i64)).unwrap_or(0))
            .unwrap_or(1);
        if busy || queued > 0 {
            return;
        }
    }
    let state = app.state::<crate::state::AppState>();
    if let Some(file) = state.launch.probe_side_file("alerts.jsonl") {
        // A test run records the ring instead of playing it.
        crate::state::append_json_line(&file, &json!({ "kind": "sound", "sound": sound, "from": "watch", "dir": dir.to_string_lossy(), "session": session }));
        return;
    }
    let app = app.clone();
    let _ = host::spawn_blocking(move || crate::alerts::sound(&app, sound)).await;
}

/// Percent-encodes everything but the unreserved characters.
fn encode(text: &str) -> String {
    let mut out = String::with_capacity(text.len());
    for b in text.bytes() {
        if b.is_ascii_alphanumeric() || b"-._~".contains(&b) {
            out.push(b as char);
        } else {
            out.push_str(&format!("%{b:02X}"));
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn rings_for_a_finished_turn_and_a_new_question_only() {
        let finished = json!({"type": "agent_event", "payload": {"type": "created", "payload": {"type": "agent_finished", "session_id": "s1"}}});
        assert_eq!(ring_for(&finished), Some(Ring::Finished("s1".into())));
        let asked = json!({"type": "prompt_pending", "payload": {"type": "updated", "payload": {"session_id": "s2", "kind": "question", "pending": true}}});
        assert_eq!(ring_for(&asked), Some(Ring::Question("s2".into())));
        let answered = json!({"type": "prompt_pending", "payload": {"type": "updated", "payload": {"session_id": "s2", "kind": "question", "pending": false}}});
        assert_eq!(ring_for(&answered), None);
        let permission = json!({"type": "prompt_pending", "payload": {"type": "updated", "payload": {"session_id": "s2", "kind": "permission", "pending": true}}});
        assert_eq!(ring_for(&permission), None);
        let error = json!({"type": "agent_event", "payload": {"type": "created", "payload": {"type": "error", "session_id": "s1"}}});
        assert_eq!(ring_for(&error), None);
    }
}
