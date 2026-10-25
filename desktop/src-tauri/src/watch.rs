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
//!
//! Every listed chat stays drawn and current in the windows (owner,
//! 2026-10-22), so a folder a window shows is listened to as well, and each
//! stream's chat events go to the windows that did not join its folder
//! (connection::push_folder), after a resync of that folder as the stream
//! starts. Rings stay for folders no window shows.

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
/// The events that keep another folder's drawn chats current: its messages,
/// and its sub-agents' rows (a done one leaves the list).
const CHAT_EVENTS: &[&str] = &["message", "helper"];
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

/// The folders open windows show: what each window reports showing (its page
/// knows, and follows a folder picked in the left list), else the folder it
/// was opened with.
fn shown(app: &AppHandle) -> Vec<PathBuf> {
    let open: Vec<String> = app.webview_windows().keys().cloned().collect();
    let reported = app.try_state::<crate::alerts::Alerts>().map(|a| a.projects(&open)).unwrap_or_default();
    let launched: Vec<(String, String)> = open
        .iter()
        .filter_map(|label| crate::windows::launch_for(app, label).map(|l| (label.clone(), l.cwd.to_string_lossy().into_owned())))
        .collect();
    shown_folders(&reported, &launched)
}

/// shown_folders reads the reported folders, leaving a window that has not
/// reported yet to the folder it was opened with.
fn shown_folders(reported: &[(String, String)], launched: &[(String, String)]) -> Vec<PathBuf> {
    let mut out: Vec<PathBuf> = reported.iter().map(|(_, dir)| resolved(dir)).filter(|dir| !dir.as_os_str().is_empty()).collect();
    for (label, dir) in launched {
        if reported.iter().any(|(reported, _)| reported == label) {
            continue; // the window said what it shows
        }
        let dir = resolved(dir);
        if !dir.as_os_str().is_empty() {
            out.push(dir);
        }
    }
    out
}

fn tick(app: &AppHandle, listeners: &mut HashMap<PathBuf, Listener>, written: &mut Option<Vec<String>>) {
    let shown = shown(app);
    let mut wanted: Vec<PathBuf> = Vec::new();
    for chat in crate::openchats::chats() {
        if chat.dir.is_empty() {
            continue;
        }
        let dir = resolved(&chat.dir);
        if !wanted.contains(&dir) {
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
    // SAFETY: getuid cannot fail.
    let dirs = socket_dirs(std::env::var_os("XDG_RUNTIME_DIR"), &std::env::temp_dir(), unsafe { libc::getuid() });
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

/// The folders Go may put engine sockets in ($XDG_RUNTIME_DIR/crush, then
/// <temp>/crush-<uid>), each kept only when private as Go's checkPrivateDir
/// wants it (internal/server/socketdir.go): a real folder, not a symlink,
/// ours, mode 0700. Only its owner can put a socket in such a folder, so
/// another user cannot serve the windows a fake engine.
fn socket_dirs(runtime: Option<std::ffi::OsString>, temp: &Path, uid: u32) -> Vec<PathBuf> {
    use std::os::unix::fs::MetadataExt;
    let mut dirs = Vec::new();
    if let Some(runtime) = runtime.filter(|v| !v.is_empty()) {
        dirs.push(PathBuf::from(runtime).join("crush"));
    }
    dirs.push(temp.join(format!("crush-{uid}")));
    dirs.retain(|dir| {
        std::fs::symlink_metadata(dir).is_ok_and(|meta| meta.file_type().is_dir() && meta.uid() == uid && meta.mode() & 0o7777 == 0o700)
    });
    dirs
}

/// What each open chat of another folder is doing, by chat id: its record
/// from that folder's engine, for the left list's 30 s check (owner,
/// 2026-10-21). A folder with no running engine runs nothing, so its chats
/// get an idle stand-in; a chat its engine cannot read is left out.
pub async fn chat_states(chats: Vec<ChatDir>) -> HashMap<String, Value> {
    let engines = engines().await;
    let mut states = HashMap::new();
    for chat in chats {
        if chat.id.is_empty() || chat.dir.is_empty() {
            continue;
        }
        let Some((socket, workspace)) = engines.get(&resolved(&chat.dir)) else {
            states.insert(chat.id.clone(), json!({ "id": chat.id, "is_busy": false, "has_unfinished_work": false, "pending_prompt": "" }));
            continue;
        };
        let path = format!("/v1/workspaces/{}/sessions/{}", encode(workspace), encode(&chat.id));
        if let Ok(reply) = EngineHttp::new(socket).call("GET", &path, None, Some(CALL_LIMIT)).await {
            if (200..300).contains(&reply.status) && reply.body.get("id").and_then(Value::as_str) == Some(chat.id.as_str()) {
                states.insert(chat.id, reply.body);
            }
        }
    }
    states
}

/// A chat and its folder, as the page sends them to chat_states.
#[derive(serde::Deserialize)]
pub struct ChatDir {
    pub id: String,
    pub dir: String,
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
        let folder = dir.to_string_lossy().into_owned();
        // What happened before the stream started is read again.
        crate::connection::push_folder(&app, &folder, Vec::new(), true);
        let mut lines = crate::sse::LineSplitter::new();
        let mut out = Vec::new();
        while let Some(Ok(chunk)) = stream.next_chunk().await {
            lines.push(&chunk, &mut out);
            let mut chat_events = Vec::new();
            let mut changed: Vec<String> = Vec::new();
            for line in out.drain(..) {
                if let Some(ev) = crate::batch::parse_sse_line(&line) {
                    if let Some(sid) = work_changed(&ev).filter(|sid| !changed.contains(sid)) {
                        changed.push(sid);
                    }
                    if CHAT_EVENTS.contains(&ev.ty.as_str()) {
                        chat_events.push(ev);
                    }
                }
                let Some(event) = line.strip_prefix("data:").and_then(|d| serde_json::from_str::<Value>(d.trim()).ok()) else { continue };
                if let Some(ring) = ring_for(&event) {
                    ring_once(&app, &http, &base, &dir, ring).await;
                }
            }
            // The record's busy state is computed on read, so it is read
            // again: the left list's dots follow it at once (owner,
            // 2026-10-22: "I DO want live busy dots").
            if !crate::connection::heard_elsewhere(&app, &folder) {
                continue;
            }
            for sid in changed {
                let path = format!("{base}/sessions/{}", encode(&sid));
                if let Ok(reply) = http.call("GET", &path, None, Some(CALL_LIMIT)).await {
                    if (200..300).contains(&reply.status) && reply.body.get("id").and_then(Value::as_str) == Some(sid.as_str()) {
                        chat_events.push(crate::batch::Event { ty: "chat_state".to_string(), kind: crate::batch::Kind::Updated, payload: reply.body });
                    }
                }
            }
            crate::connection::push_folder(&app, &folder, chat_events, false);
        }
    });
    Listener { task, open }
}

/// The chat whose work may have started, ended or begun waiting: a new
/// message, an agent event, a finished run or a prompt's change. Agent tool
/// chats ("message$$call") have no record of their own.
fn work_changed(ev: &crate::batch::Event) -> Option<String> {
    let starts = match ev.ty.as_str() {
        "message" => ev.kind == crate::batch::Kind::Created,
        "agent_event" | "run_complete" | "prompt_pending" => true,
        _ => false,
    };
    let sid = ev.payload.get("session_id").and_then(Value::as_str).unwrap_or_default();
    (starts && !sid.is_empty() && !sid.contains("$$")).then(|| sid.to_string())
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

    // A socket folder another user made (or one open to others) is never
    // read: only a real folder of ours with mode 0700, as Go accepts.
    #[test]
    fn only_private_socket_folders_are_read() {
        use std::os::unix::fs::PermissionsExt;
        let base = std::env::temp_dir().join(format!("crush-desktop-watch-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&base);
        let open = base.join("run").join("crush");
        // SAFETY: getuid cannot fail.
        let uid = unsafe { libc::getuid() };
        let private = base.join("tmp").join(format!("crush-{uid}"));
        for (dir, mode) in [(&open, 0o755), (&private, 0o700)] {
            std::fs::create_dir_all(dir).unwrap();
            std::fs::set_permissions(dir, std::fs::Permissions::from_mode(mode)).unwrap();
        }
        let dirs = socket_dirs(Some(base.join("run").into_os_string()), &base.join("tmp"), uid);
        let _ = std::fs::remove_dir_all(&base);
        assert_eq!(dirs, vec![private]);
    }

    #[test]
    fn work_changes_are_new_messages_agent_events_run_ends_and_prompts() {
        use crate::batch::{Event, Kind};
        let ev = |ty: &str, kind: Kind, sid: &str| Event { ty: ty.to_string(), kind, payload: json!({"session_id": sid}) };
        assert_eq!(work_changed(&ev("message", Kind::Created, "s1")), Some("s1".into()));
        assert_eq!(work_changed(&ev("message", Kind::Updated, "s1")), None, "a streaming update is not a change of work");
        assert_eq!(work_changed(&ev("run_complete", Kind::Created, "s2")), Some("s2".into()));
        assert_eq!(work_changed(&ev("prompt_pending", Kind::Updated, "s3")), Some("s3".into()));
        assert_eq!(work_changed(&ev("agent_event", Kind::Created, "m$$call")), None);
        assert_eq!(work_changed(&ev("session", Kind::Updated, "s1")), None);
    }

    // A window rings for the folder it reports showing, not the one it was
    // opened with, so a folder it stopped showing still rings once.
    #[test]
    fn shown_follows_the_folder_each_window_reports() {
        let reported = vec![("w1".to_string(), "/p/b".to_string())];
        let launched = vec![("w1".to_string(), "/p/a".to_string()), ("w2".to_string(), "/p/c".to_string())];
        assert_eq!(shown_folders(&reported, &launched), vec![PathBuf::from("/p/b"), PathBuf::from("/p/c")], "a window that reported nothing keeps its launch folder");
        assert_eq!(shown_folders(&[], &launched), vec![PathBuf::from("/p/a"), PathBuf::from("/p/c")]);
    }

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
