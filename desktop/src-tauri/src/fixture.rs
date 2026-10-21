//! Test-only fixture mode (desktop-design 6.3). With `--fixture FILE` the
//! window talks to a player of recorded engine answers instead of an engine,
//! so screen pieces can be checked in the real window before the engine
//! client exists, and the look checks get fixed data.
//!
//! File shape:
//!
//! ```json
//! {"workspace": {}, "agent": {}, "engine": {},
//!  "get": {"sessions": [], "sessions/S1/messages": [], "config": {}},
//!  "events": [{"at_ms": 0, "entries": [{"kind": "event", "type": "message", "event": "updated", "payload": {}}]}]}
//! ```
//!
//! - `engine_open` answers from `workspace` (without `env` and `config`) and
//!   `engine` (optional), and pushes a `connected` connection event. With
//!   `projects` (`{"/folder": {"workspace": {}, "get": {}}}`), a window opening
//!   one of those folders gets its workspace, and its `get` answers before
//!   the top-level ones (a folder switch in place, `project_enter`).
//! - `api` GET answers `get[path]`, or 404 `not_found` when absent; `agent`
//!   also answers GET "agent". Every other call is appended to
//!   `<probe file>.api.jsonl` and answered with null.
//! - `slow` (`{"sessions/S1/messages": 2000}`) holds a GET answer back that
//!   many milliseconds; `"engine_open"` holds back joining a folder.
//! - `app_update` (`{"check": {}, "run": {}, "run_ms": 0}`) answers the app
//!   updater's check and run (appupdate.rs).
//! - `api_abort` answers false (no fixture call runs long) and is recorded too.
//! - The entries of `events` reach the window's batcher at their `at_ms` after
//!   `engine_open`, through the same doorbell as the engine's events, so the
//!   frame loop and the stores run as they do with the engine.

use crate::batch::{Batcher, Entry, Event, Kind};
use crate::connection::OpenResult;
use crate::engine_cli::{EngineVersion, Ensured, APP_PROTOCOL};
use crate::error::{AppError, AppResult};
use crate::state::{append_json_line, Launch};
use serde_json::{json, Map, Value};
use std::collections::HashMap;
use std::path::Path;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};
use crate::host::AppHandle;

#[derive(serde::Deserialize, Default)]
#[serde(default)]
struct FixtureFile {
    workspace: Value,
    agent: Value,
    engine: Option<Ensured>,
    get: Map<String, Value>,
    events: Vec<Timed>,
    projects: Map<String, Value>,
    /// The chats open at the start (openchats.rs); absent, every recorded
    /// top-level chat of `get.sessions`, in the workspace's folder.
    open_chats: Option<Vec<crate::openchats::Chat>>,
    /// GET paths answered late, path → milliseconds (a chat that loads
    /// slowly, for checks that act while it loads).
    slow: Map<String, Value>,
    /// The app updater's answers: {"check": {}, "run": {}, "run_ms": 0}.
    app_update: Map<String, Value>,
}

#[derive(serde::Deserialize, Clone)]
struct Timed {
    #[serde(default)]
    at_ms: u64,
    #[serde(default)]
    entries: Vec<Value>,
}

/// The loaded fixture, managed by main.rs in fixture mode.
pub struct Fixture {
    file: FixtureFile,
    loaded: Instant,
    players: Mutex<HashMap<String, Arc<Player>>>,
}

/// One window's side of the fixture: its open answer and its batch.
struct Player {
    opened: OpenResult,
    /// The opened folder's own answers (`projects[cwd].get`).
    get: Map<String, Value>,
    queue: Mutex<Queue>,
    stop: AtomicBool,
}

struct Queue {
    batcher: Batcher,
    /// Wire entries that come before everything the batcher holds (a fixture
    /// `resync` entry drains the batcher here first, so order is kept).
    ready: Vec<Value>,
    /// A doorbell went out and no take has happened since.
    rung: bool,
}

impl Fixture {
    pub fn load(path: &Path) -> Result<Fixture, String> {
        let text = std::fs::read_to_string(path)
            .map_err(|e| format!("cannot read the fixture {}: {e}", path.display()))?;
        let file: FixtureFile = serde_json::from_str(&text)
            .map_err(|e| format!("cannot parse the fixture {}: {e}", path.display()))?;
        Ok(Fixture {
            file,
            loaded: Instant::now(),
            players: Mutex::new(HashMap::new()),
        })
    }

    /// How long a GET of path is held back (`slow`).
    pub fn delay(&self, method: &str, path: &str) -> Option<Duration> {
        if !method.eq_ignore_ascii_case("GET") {
            return None;
        }
        answer_in(&self.file.slow, path).and_then(|ms| ms.as_u64()).map(Duration::from_millis)
    }

    /// The app updater's answer to `which` ("check" or "run") and, for a
    /// run, how long it takes (`app_update.run_ms`).
    pub fn app_update(&self, which: &str) -> (Value, Duration) {
        let value = self.file.app_update.get(which).cloned().unwrap_or_else(|| serde_json::json!({"error": "no fixture answer"}));
        let wait = if which == "run" { self.file.app_update.get("run_ms").and_then(Value::as_u64).unwrap_or(0) } else { 0 };
        (value, Duration::from_millis(wait))
    }

    /// The chats a fixture run starts with open.
    pub fn open_chats(&self) -> Vec<crate::openchats::Chat> {
        if let Some(chats) = &self.file.open_chats {
            return chats.clone();
        }
        let dir = self.file.workspace.get("path").and_then(Value::as_str).unwrap_or_default().to_string();
        let sessions = self.file.get.get("sessions").and_then(Value::as_array).cloned().unwrap_or_default();
        sessions
            .iter()
            .filter(|s| s.get("parent_session_id").and_then(Value::as_str).unwrap_or_default().is_empty())
            .filter_map(|s| {
                Some(crate::openchats::Chat {
                    id: s.get("id")?.as_str()?.to_string(),
                    dir: dir.clone(),
                    title: s.get("title").and_then(Value::as_str).unwrap_or_default().to_string(),
                    opened: 0,
                })
            })
            .collect()
    }

    /// `engine_open`. A second call for the same window returns the same answer.
    pub fn open(&self, app: &AppHandle, label: &str, cwd: &str, sent_yolo: bool) -> AppResult<OpenResult> {
        let player = {
            let mut players = self.players.lock().unwrap();
            if let Some(player) = players.get(label) {
                return Ok(player.opened.clone());
            }
            let project = self.file.projects.get(cwd.trim_end_matches('/'));
            let workspace = public_copy(project.and_then(|p| p.get("workspace")).unwrap_or(&self.file.workspace));
            let get = project.and_then(|p| p.get("get")).and_then(Value::as_object).cloned().unwrap_or_default();
            let mut engine = self.file.engine.clone().unwrap_or_else(|| Ensured {
                address: "fixture".to_string(),
                socket: String::new(),
                protocol: APP_PROTOCOL,
                version: "fixture".to_string(),
                pid: 0,
                data_dir: workspace
                    .get("data_dir")
                    .and_then(Value::as_str)
                    .unwrap_or_default()
                    .to_string(),
                started: false,
            });
            if project.is_some() {
                // The engine's data folder is the workspace's (connection.rs).
                engine.data_dir = workspace.get("data_dir").and_then(Value::as_str).unwrap_or_default().to_string();
            }
            let opened = OpenResult {
                client_id: format!("fixture-{label}"),
                workspace,
                version: EngineVersion {
                    version: engine.version.clone(),
                    protocol: engine.protocol,
                },
                engine,
                sent_yolo,
                cwd: cwd.to_string(),
            };
            let player = Arc::new(Player {
                opened,
                get,
                queue: Mutex::new(Queue {
                    batcher: Batcher::new(5_000),
                    ready: Vec::new(),
                    rung: false,
                }),
                stop: AtomicBool::new(false),
            });
            players.insert(label.to_string(), player.clone());
            player
        };
        let connected = json!({
            "kind": "event", "type": "connection", "event": "updated",
            "payload": {
                "state": "connected", "attempt": 0, "resync": false, "fresh_engine": false,
                "workspace": player.opened.workspace, "engine": player.opened.engine,
            },
        });
        player.push(app, label, vec![connected]);
        play(app.clone(), label.to_string(), player.clone(), self.file.events.clone());
        Ok(player.opened.clone())
    }

    /// `api`: GET from the recording, everything else recorded and answered with null.
    #[allow(clippy::too_many_arguments)] // mirrors connection::call
    pub fn api(
        &self,
        launch: &Launch,
        label: &str,
        method: &str,
        path: &str,
        query: Option<Map<String, Value>>,
        body: Option<Value>,
        call_id: Option<String>,
    ) -> AppResult<Value> {
        if method.eq_ignore_ascii_case("GET") {
            let own = self.players.lock().unwrap().get(label).map(|p| p.get.clone()).unwrap_or_default();
            return answer_in(&own, path).or_else(|| self.answer(path)).ok_or_else(|| AppError {
                code: "not_found".to_string(),
                message: format!("the fixture has no answer for GET {path}"),
                field: None,
                status: Some(404),
            });
        }
        let mut line = json!({"command": "api", "window": label, "method": method, "path": path, "query": query, "body": body});
        if let Some(call_id) = call_id {
            line["call_id"] = json!(call_id);
        }
        self.record(launch, line);
        Ok(Value::Null)
    }

    /// `api_secret` (desktop-design-5-6 6.4): records only the route and the
    /// body's top-level key names, `{"secret": path, "keys": [...]}`, never a
    /// value, and answers null.
    pub fn api_secret(&self, launch: &Launch, path: &str, body: &Value) -> AppResult<Value> {
        crate::connection::check_secret_route(path)?;
        let keys: Vec<&String> = body.as_object().map(|map| map.keys().collect()).unwrap_or_default();
        self.record(launch, json!({"secret": path, "keys": keys}));
        Ok(Value::Null)
    }

    /// `api_abort`: a fixture call never runs long, so none is running and the answer is
    /// false (1.2.9). Recorded, so a check can see that the window asked to stop a call.
    pub fn abort(&self, launch: &Launch, label: &str, call_id: &str) -> bool {
        self.record(
            launch,
            json!({"command": "api_abort", "window": label, "call_id": call_id}),
        );
        false
    }

    /// `engine_set_current_session`: recorded, so a check can see which chat the window showed.
    pub fn set_current_session(&self, launch: &Launch, label: &str, session_id: &str) -> AppResult<()> {
        self.record(
            launch,
            json!({"command": "engine_set_current_session", "window": label, "session_id": session_id}),
        );
        Ok(())
    }

    /// `events_take`.
    pub fn take(&self, label: &str) -> Vec<Value> {
        let player = self.players.lock().unwrap().get(label).cloned();
        player.map(|p| p.take()).unwrap_or_default()
    }

    /// The window closed: its player stops.
    pub fn close(&self, label: &str) {
        if let Some(player) = self.players.lock().unwrap().remove(label) {
            player.stop.store(true, Ordering::Relaxed);
        }
    }

    fn answer(&self, path: &str) -> Option<Value> {
        if let Some(v) = answer_in(&self.file.get, path) {
            return Some(v);
        }
        if path == "agent" && !self.file.agent.is_null() {
            return Some(self.file.agent.clone());
        }
        None
    }

    fn record(&self, launch: &Launch, mut line: Value) {
        if let Some(path) = launch.probe_side_file("api.jsonl") {
            line["at_ms"] = json!(self.loaded.elapsed().as_millis() as u64);
            append_json_line(&path, &line);
        }
    }
}

impl Player {
    fn push(&self, app: &AppHandle, label: &str, entries: Vec<Value>) {
        if entries.is_empty() {
            return;
        }
        let ring = {
            let mut queue = self.queue.lock().unwrap();
            for entry in entries {
                queue.push(entry);
            }
            !std::mem::replace(&mut queue.rung, true)
        };
        if ring {
            let _ = app.emit_to(label, "events-ready", ());
        }
    }

    fn take(&self) -> Vec<Value> {
        let mut queue = self.queue.lock().unwrap();
        queue.rung = false;
        let mut out = std::mem::take(&mut queue.ready);
        out.extend(queue.batcher.take().into_iter().map(wire_entry));
        out
    }
}

impl Queue {
    fn push(&mut self, entry: Value) {
        match entry.get("kind").and_then(Value::as_str) {
            Some("resync") => {
                let held = self.batcher.take();
                self.ready.extend(held.into_iter().map(wire_entry));
                self.ready.push(json!({"kind": "resync"}));
            }
            Some("event") => match event_of(&entry) {
                Some(ev) => self.batcher.push(ev),
                None => eprintln!("crush-desktop: the fixture has a bad event entry: {entry}"),
            },
            _ => eprintln!("crush-desktop: the fixture has an entry of no known kind: {entry}"),
        }
    }
}

/// Pushes each timed group of entries at its `at_ms` after the open.
fn play(app: AppHandle, label: String, player: Arc<Player>, mut events: Vec<Timed>) {
    if events.is_empty() {
        return;
    }
    events.sort_by_key(|t| t.at_ms);
    std::thread::spawn(move || {
        let start = Instant::now();
        for timed in events {
            let due = start + Duration::from_millis(timed.at_ms);
            loop {
                if player.stop.load(Ordering::Relaxed) {
                    return;
                }
                let now = Instant::now();
                if now >= due {
                    break;
                }
                std::thread::sleep((due - now).min(Duration::from_millis(20)));
            }
            player.push(&app, &label, timed.entries);
        }
    });
}

/// `get[path]`; "/v1/workspaces/{id}/x" asks for the same as the relative "x".
fn answer_in(get: &Map<String, Value>, path: &str) -> Option<Value> {
    if let Some(v) = get.get(path) {
        return Some(v.clone());
    }
    let (_, relative) = path.strip_prefix("/v1/workspaces/")?.split_once('/')?;
    get.get(relative).cloned()
}

/// A workspace as the window may see it: without `env` and `config` (1.2.5).
fn public_copy(workspace: &Value) -> Value {
    let mut copy = workspace.clone();
    if let Some(map) = copy.as_object_mut() {
        map.remove("env");
        map.remove("config");
    }
    copy
}

fn event_of(entry: &Value) -> Option<Event> {
    let ty = entry.get("type")?.as_str()?.to_string();
    let kind = match entry.get("event")?.as_str()? {
        "created" => Kind::Created,
        "updated" => Kind::Updated,
        "deleted" => Kind::Deleted,
        _ => return None,
    };
    let payload = entry.get("payload").cloned().unwrap_or(Value::Null);
    Some(Event { ty, kind, payload })
}

/// The wire form of a batch entry (1.4.2), the same one `connection::take` hands
/// the window: `{kind:"event", type, event, payload}` or `{kind:"resync"}`.
pub fn wire_entry(entry: Entry) -> Value {
    match entry {
        Entry::Event(e) => {
            let event = match e.kind {
                Kind::Created => "created",
                Kind::Updated => "updated",
                Kind::Deleted => "deleted",
            };
            json!({"kind": "event", "type": e.ty, "event": event, "payload": e.payload})
        }
        Entry::Resync => json!({"kind": "resync"}),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn queue() -> Queue {
        Queue {
            batcher: Batcher::new(5_000),
            ready: Vec::new(),
            rung: false,
        }
    }

    fn take(q: &mut Queue) -> Vec<Value> {
        let mut out = std::mem::take(&mut q.ready);
        out.extend(q.batcher.take().into_iter().map(wire_entry));
        out
    }

    #[test]
    fn entries_merge_like_the_engines_and_resync_keeps_its_place() {
        let mut q = queue();
        let msg = |v: u32| json!({"kind": "event", "type": "message", "event": "updated", "payload": {"id": "m1", "v": v}});
        q.push(msg(1));
        q.push(msg(2));
        q.push(json!({"kind": "resync"}));
        q.push(msg(3));
        assert_eq!(
            take(&mut q),
            vec![
                json!({"kind": "event", "type": "message", "event": "updated", "payload": {"id": "m1", "v": 2}}),
                json!({"kind": "resync"}),
                json!({"kind": "event", "type": "message", "event": "updated", "payload": {"id": "m1", "v": 3}}),
            ]
        );
        assert!(take(&mut q).is_empty());
    }

    #[test]
    fn bad_entries_are_skipped() {
        let mut q = queue();
        q.push(json!({"kind": "event", "type": "message", "event": "exploded"}));
        q.push(json!({"kind": "later"}));
        q.push(json!({"kind": "event", "type": "x", "event": "created"}));
        assert_eq!(
            take(&mut q),
            vec![json!({"kind": "event", "type": "x", "event": "created", "payload": null})]
        );
    }

    #[test]
    fn the_window_never_gets_env_or_config() {
        let ws = json!({"id": "w", "path": "/p", "env": ["KEY=secret"], "config": {"providers": {}}});
        assert_eq!(public_copy(&ws), json!({"id": "w", "path": "/p"}));
    }

    #[test]
    fn get_answers_relative_and_absolute_paths() {
        let fixture = Fixture {
            file: serde_json::from_value(json!({
                "agent": {"is_ready": true},
                "get": {"sessions": [1], "/v1/limits": {"clis": []}},
            }))
            .unwrap(),
            loaded: Instant::now(),
            players: Mutex::new(HashMap::new()),
        };
        let launch = Launch::default();
        let get = |path: &str| fixture.api(&launch, "main", "GET", path, None, None, None);
        assert_eq!(get("sessions").unwrap(), json!([1]));
        assert_eq!(get("/v1/workspaces/w1/sessions").unwrap(), json!([1]));
        assert_eq!(get("/v1/limits").unwrap(), json!({"clis": []}));
        assert_eq!(get("agent").unwrap(), json!({"is_ready": true}));
        let missing = get("config").unwrap_err();
        assert_eq!((missing.code.as_str(), missing.status), ("not_found", Some(404)));
        let post = fixture.api(&launch, "main", "POST", "agent", None, Some(json!({})), Some("c1".into()));
        assert_eq!(post.unwrap(), Value::Null);
        assert!(!fixture.abort(&launch, "main", "c1"));
    }
}
