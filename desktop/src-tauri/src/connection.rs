//! Join, stay joined, reconnect: one engine connection per window
//! (desktop-design 1.2.5, 1.2.6; engine design "What the desktop app calls").
//!
//! A window's connection is a [`WindowConn`] in `AppState.windows`. Opening it
//! checks crush, runs `crush engine ensure`, joins the project's workspace and
//! reads its event stream into the window's batcher; the window pulls the
//! batch with `events_take` after a doorbell. Losing the engine starts one
//! reconnect: ensure again, rejoin with the same client id, then a
//! `connected` event with `resync: true`. The app's own connection events go
//! through the batcher as the barrier type `connection`, so they keep their
//! place among the engine's events.

use crate::batch::{self, Batcher, Event, Kind};
use crate::crush_bin::{self, CrushBin};
use crate::engine_cli::{self, EngineVersion, Ensured, APP_PROTOCOL};
use crate::error::{AppError, AppResult};
use crate::sse::LineSplitter;
use crate::state::{AppState, Launch};
use crate::uds_http::{ByteStream, EngineHttp, Reply};
use crate::{fixture, login_env};
use serde_json::{json, Map, Value};
use std::collections::HashMap;
use std::future::Future;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex, MutexGuard};
use std::time::{Duration, Instant};
use tauri::async_runtime::JoinHandle;
use tauri::{Emitter, Manager};

const APP_VERSION: &str = env!("CARGO_PKG_VERSION");
/// Entries a hidden window's batch may hold before it falls back to a Resync.
const BATCH_MAX: usize = 5_000;

/// Time limits and the reconnect rhythm (1.2.4, 1.2.6). Tests shorten them.
#[derive(Clone, Copy)]
struct Tuning {
    /// Every call except the event stream and an `api` call with a `callId`.
    call_limit: Duration,
    /// The wait after the first failed reconnect try; it doubles after each.
    first_wait: Duration,
    max_wait: Duration,
    /// Failed tries in a row before the window shows "failed".
    max_failures: u32,
    /// The DELETE of a closing window.
    close_limit: Duration,
}

const TUNING: Tuning = Tuning {
    call_limit: Duration::from_secs(30),
    first_wait: Duration::from_millis(250),
    max_wait: Duration::from_secs(10),
    max_failures: 3,
    close_limit: Duration::from_secs(1),
};

/// The waits between failed reconnect tries: 250 ms, then double, capped at 10 s.
struct Backoff {
    next: Duration,
    max: Duration,
}

impl Backoff {
    fn new(first: Duration, max: Duration) -> Self {
        Backoff { next: first, max }
    }

    fn next(&mut self) -> Duration {
        let wait = self.next;
        self.next = (self.next * 2).min(self.max);
        wait
    }
}

/// The window's doorbell: the `events-ready` window event.
type Ring = Arc<dyn Fn() + Send + Sync>;
/// The login-shell environment (1.2.7). Read when a request needs it, so it
/// lives only in login_env's cache and in the requests that carry it.
type EnvSource = Arc<dyn Fn() -> Vec<(String, String)> + Send + Sync>;
/// Join step 1 (blocking): find crush and check its version, given the
/// login-shell environment.
type CrushCheck = Box<dyn FnOnce(&[(String, String)]) -> AppResult<(CrushBin, EngineVersion)> + Send>;

/// Where a window's engine comes from (START-011).
trait Finder: Send + Sync {
    /// Join step 2: the engine to dial. Blocking: it may run crush.
    fn find(&self) -> AppResult<Ensured>;
    /// Whether `find` runs `crush engine ensure`; false for `-H`.
    fn ensures(&self) -> bool;
}

/// `crush engine ensure`, which finds the project's engine or starts one.
struct EnsureFinder {
    bin: CrushBin,
    cwd: String,
    data_dir: Option<String>,
    env: EnvSource,
}

impl Finder for EnsureFinder {
    fn find(&self) -> AppResult<Ensured> {
        engine_cli::ensure(&self.bin, &self.cwd, self.data_dir.as_deref(), &(self.env)())
    }
    fn ensures(&self) -> bool {
        true
    }
}

/// `-H ADDR`: that engine, as given. Losing it only re-dials (today's client mode).
struct HostFinder {
    address: String,
    socket: String,
}

impl Finder for HostFinder {
    fn find(&self) -> AppResult<Ensured> {
        // `dial` fills in the rest from GET /v1/version and GET /v1/engine.
        Ok(Ensured {
            address: self.address.clone(),
            socket: self.socket.clone(),
            protocol: 0,
            version: String::new(),
            pid: 0,
            data_dir: String::new(),
            started: false,
        })
    }
    fn ensures(&self) -> bool {
        false
    }
}

/// START-011: with `-H` the window joins that address and never looks for
/// crush or runs ensure; without it, ensure finds or starts the engine.
#[derive(Debug, PartialEq)]
enum EnginePlan {
    Ensure,
    Host { address: String, socket: String },
}

fn engine_plan(host: Option<&str>) -> AppResult<EnginePlan> {
    let Some(host) = host.filter(|h| !h.is_empty()) else {
        return Ok(EnginePlan::Ensure);
    };
    // The terminal's host form (internal/server/server.go:28-48); the app speaks unix sockets only.
    match host.split_once("://") {
        Some(("unix", socket)) if !socket.is_empty() => Ok(EnginePlan::Host {
            address: host.to_string(),
            socket: socket.to_string(),
        }),
        _ => Err(AppError::new(
            "engine_unavailable",
            format!("Crush Desktop can join an engine only at a unix:// address, not {host}."),
        )),
    }
}

/// Everything a window's joins need, fixed when it opens.
struct Plan {
    /// The project folder, resolved (START-008).
    cwd: String,
    client_id: String,
    finder: Arc<dyn Finder>,
    /// What `crush engine version` said; None with `-H` (GET /v1/version tells).
    version: Option<EngineVersion>,
    /// The create body (join step 4) without `env`, which is added as it is sent.
    body: Value,
    env: EnvSource,
    sent_yolo: bool,
}

/// The workspace create body (join step 4; proto.Workspace). START-009:
/// `-D` as `data_dir`; START-010: `debug`; START-013: `channels`. Like the
/// terminal's (`internal/cmd/workspace_setup.go:342-351`, whose `debug` and
/// `channels` are omitempty), `debug` and `channels` go only when set.
fn create_body(cwd: &str, data_dir: Option<&str>, client_id: &str, yolo: bool, debug: bool, channels: &[String]) -> Value {
    let mut body = json!({
        "path": cwd,
        "data_dir": data_dir.unwrap_or(""),
        "version": APP_VERSION,
        "protocol": APP_PROTOCOL,
        "client_id": client_id,
        "yolo": yolo,
    });
    if debug {
        body["debug"] = json!(true);
    }
    if !channels.is_empty() {
        body["channels"] = json!(channels);
    }
    body
}

/// START-008: the folder must exist, as the terminal's chdir requires
/// (internal/cmd/root.go:320-323). Answers its absolute, resolved form, which
/// the terminal sends after the chdir (os.Getwd, workspace_setup.go:257-265).
fn project_folder(cwd: &str) -> AppResult<String> {
    let fail = |reason: String| AppError::new("bad_path", format!("failed to change directory: chdir {cwd}: {reason}"));
    let path = std::fs::canonicalize(cwd).map_err(|e| fail(errno_text(&e)))?;
    if !path.is_dir() {
        return Err(fail("not a directory".to_string()));
    }
    Ok(path.to_string_lossy().into_owned())
}

/// An OS error as Go words it ("no such file or directory").
fn errno_text(e: &std::io::Error) -> String {
    let text = e.to_string();
    let text = text.split(" (os error").next().unwrap_or_default();
    let mut chars = text.chars();
    match chars.next() {
        Some(first) => first.to_lowercase().chain(chars).collect(),
        None => String::new(),
    }
}

/// Builds the plan for a window: the folder, then the login-shell
/// environment and either the `-H` address or crush (found and
/// version-checked, join step 1).
async fn plan_for(launch: &Launch, settings_yolo: bool, cwd: &str, env: EnvSource, check: CrushCheck) -> AppResult<Plan> {
    let cwd = project_folder(cwd)?;
    let source = env.clone();
    let (finder, version): (Arc<dyn Finder>, Option<EngineVersion>) = match engine_plan(launch.host.as_deref())? {
        EnginePlan::Host { address, socket } => {
            // Read now, off the async threads: the create sends it.
            blocking(move || Ok(source().len())).await?;
            (Arc::new(HostFinder { address, socket }), None)
        }
        EnginePlan::Ensure => {
            let (bin, version) = blocking(move || check(&source())).await?;
            let finder = EnsureFinder {
                bin,
                cwd: cwd.clone(),
                data_dir: launch.data_dir.clone(),
                env: env.clone(),
            };
            (Arc::new(finder), Some(version))
        }
    };
    let client_id = uuid::Uuid::new_v4().to_string();
    let sent_yolo = launch.yolo.unwrap_or(settings_yolo);
    let body = create_body(&cwd, launch.data_dir.as_deref(), &client_id, sent_yolo, launch.debug, &launch.channels);
    Ok(Plan { cwd, client_id, finder, version, body, env, sent_yolo })
}

/// Join step 1 for real: `crush_bin::resolve` with the login shell's PATH,
/// then `engine_cli::version`.
fn crush_check(setting: Option<String>) -> CrushCheck {
    Box::new(move |env| {
        let login_path = env.iter().find(|(k, _)| k == "PATH").map(|(_, v)| v.as_str());
        let bin = crush_bin::resolve(setting.as_deref(), login_path)?;
        let version = engine_cli::version(&bin)?;
        Ok((bin, version))
    })
}

/// Runs blocking work (crush, the login shell) off the async threads.
async fn blocking<T: Send + 'static>(work: impl FnOnce() -> AppResult<T> + Send + 'static) -> AppResult<T> {
    tauri::async_runtime::spawn_blocking(work)
        .await
        .unwrap_or_else(|_| Err(AppError::new("internal", "A background step of Crush Desktop stopped unexpectedly.")))
}

/// The routes the window may call through `api` (1.3), method and pattern:
/// after `/v1/workspaces/{id}/` unless absolute. `GET /v1/workspaces` and
/// `GET /v1/workspaces/{id}` are left out on purpose: they answer the
/// environment (1.2.5).
const ROUTES: &[(&str, &str)] = &[
    ("GET", "/v1/version"),
    ("GET", "/v1/engine"),
    ("GET", "/v1/limits"),
    ("POST", "/v1/limits/{kind}/refresh"),
    ("GET", "config"),
    ("GET", "providers"),
    ("GET", "agent"),
    ("GET", "agent/default-small-model"),
    ("GET", "git/branch"),
    ("GET", "lsps"),
    ("GET", "mcp/states"),
    ("GET", "skills"),
    ("GET", "messages/user"),
    ("GET", "sessions"),
    ("POST", "sessions"),
    ("GET", "sessions/{sid}"),
    ("PUT", "sessions/{sid}"),
    ("DELETE", "sessions/{sid}"),
    ("GET", "sessions/{sid}/messages"),
    ("GET", "sessions/{sid}/messages/user"),
    ("GET", "sessions/{sid}/messages/{mid}/review"),
    ("GET", "sessions/{sid}/history"),
    ("GET", "sessions/{sid}/filetracker/files"),
    ("POST", "sessions/{sid}/kept-skills"),
    ("POST", "agent"),
    ("POST", "agent/main"),
    // The model restore after `config/model` (routes.go:98).
    ("POST", "agent/update"),
    // Error counts per code checker (routes.go:79).
    ("GET", "lsps/{lsp}/diagnostics"),
    ("GET", "agent/sessions/{sid}"),
    ("GET", "agent/sessions/{sid}/prompts/queued"),
    ("GET", "agent/sessions/{sid}/prompts/list"),
    ("POST", "agent/sessions/{sid}/cancel"),
    ("POST", "agent/sessions/{sid}/interrupt"),
    ("POST", "agent/sessions/{sid}/prompts/clear"),
    ("POST", "agent/sessions/{sid}/prompts/recall"),
    ("POST", "agent/sessions/{sid}/shell"),
    ("POST", "config/set"),
    ("POST", "config/remove"),
    ("POST", "config/model"),
    ("POST", "config/compact"),
    ("GET", "permissions/skip"),
    ("POST", "permissions/skip"),
    ("POST", "permissions/grant"),
    ("GET", "project/needs-init"),
    ("GET", "project/init-prompt"),
    ("POST", "project/init"),
    ("POST", "skills/read"),
    ("POST", "mcp/read-resource"),
    ("GET", "filetracker/lastread"),
    // Phase 4 (5.6, 5.11).
    ("POST", "sessions/{sid}/branches"),
    ("GET", "chat-links"),
    ("GET", "sessions/{sid}/tree"),
    ("GET", "sessions/{sid}/tree/messages/{mid}"),
    ("POST", "sessions/{sid}/tree/label"),
    ("POST", "sessions/{sid}/tree/switch"),
    ("POST", "sessions/{sid}/tree/navigate"),
    ("POST", "sessions/{sid}/tree/copy"),
    ("POST", "sessions/{sid}/files/preview"),
    ("POST", "sessions/{sid}/files/undo-preview"),
    ("POST", "sessions/{sid}/files/undo"),
];

/// A path the route list allows, split into its parts.
#[derive(Debug)]
struct Route {
    absolute: bool,
    parts: Vec<String>,
}

impl Route {
    fn find(method: &str, path: &str) -> AppResult<Route> {
        let refused = || AppError::new("route_not_allowed", format!("Crush Desktop does not call {method} {path}."));
        let (absolute, rest) = match path.strip_prefix('/') {
            Some(rest) => (true, rest),
            None => (false, path),
        };
        let parts: Vec<&str> = rest.split('/').collect();
        if parts.iter().any(|p| p.is_empty() || *p == "." || *p == "..") {
            return Err(refused());
        }
        let listed = ROUTES.iter().any(|(m, pattern)| {
            let (p_absolute, p_rest) = match pattern.strip_prefix('/') {
                Some(rest) => (true, rest),
                None => (false, *pattern),
            };
            let pattern: Vec<&str> = p_rest.split('/').collect();
            *m == method
                && p_absolute == absolute
                && pattern.len() == parts.len()
                && pattern.iter().zip(&parts).all(|(p, s)| p.starts_with('{') || p == s)
        });
        if !listed {
            return Err(refused());
        }
        Ok(Route {
            absolute,
            parts: parts.into_iter().map(String::from).collect(),
        })
    }

    /// The path and query sent: the window's workspace for a relative route,
    /// every part percent-encoded, and always this window's `client_id`.
    fn target(&self, workspace_id: &str, query: Option<&Map<String, Value>>, client_id: &str) -> String {
        let mut target = if self.absolute {
            String::new()
        } else {
            format!("/v1/workspaces/{}", encode(workspace_id))
        };
        for part in &self.parts {
            target.push('/');
            target.push_str(&encode(part));
        }
        let mut pairs = Vec::new();
        for (key, value) in query.into_iter().flatten() {
            if key != "client_id" {
                query_pairs(&mut pairs, key, value);
            }
        }
        pairs.push(format!("client_id={}", encode(client_id)));
        format!("{target}?{}", pairs.join("&"))
    }
}

/// A query value as Go's form decoding reads it: strings as they are, other
/// values as JSON text, a list as the key repeated, null left out.
fn query_pairs(pairs: &mut Vec<String>, key: &str, value: &Value) {
    match value {
        Value::Null => {}
        Value::String(s) => pairs.push(format!("{}={}", encode(key), encode(s))),
        Value::Array(items) => items.iter().for_each(|item| query_pairs(pairs, key, item)),
        other => pairs.push(format!("{}={}", encode(key), encode(&other.to_string()))),
    }
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

/// Drops `env` and `config` and keeps every other field; the only way a workspace leaves
/// connection.rs (1.2.5).
pub fn public_workspace(ws: serde_json::Value) -> serde_json::Value {
    let mut ws = ws;
    if let Value::Object(map) = &mut ws {
        map.remove("env");
        map.remove("config");
    }
    ws
}

#[derive(serde::Serialize, Clone)]
pub struct OpenResult {
    pub client_id: String,
    /// public_workspace(the create answer): proto.Workspace without env and config (1.2.5).
    pub workspace: serde_json::Value,
    pub engine: engine_cli::Ensured,
    pub version: engine_cli::EngineVersion,
    /// What the app asked for; compare with workspace.yolo.
    pub sent_yolo: bool,
    /// What the app asked for; compare with workspace.path.
    pub cwd: String,
}

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
enum Phase {
    /// Not joined yet (or the last open failed).
    Idle,
    Connected,
    Reconnecting,
    /// Three reconnect tries failed in a row; waiting for "Try again".
    Failed,
    Closed,
}

/// The engine a joined window talks to.
#[derive(Clone)]
struct Link {
    http: Arc<EngineHttp>,
    workspace_id: String,
}

struct Inner {
    phase: Phase,
    plan: Option<Arc<Plan>>,
    link: Option<Link>,
    opened: Option<OpenResult>,
    /// The chat the window last reported (`""` is the landing page); sent again after a rejoin.
    current_session: Option<String>,
    /// The join the window is on; the end of an older join's stream, or a
    /// failed call made through it, starts nothing.
    generation: u64,
    counter: u64,
    /// The join under way, and whether its event stream already ended.
    joining: Option<u64>,
    joining_lost: bool,
    reader: Option<JoinHandle<()>>,
    reconnect: Option<JoinHandle<()>>,
}

struct Queue {
    batcher: Batcher,
    /// A doorbell went out and no take has happened since.
    rung: bool,
    /// Events pushed since the last take; tests wait on it.
    #[cfg(test)]
    pushed: usize,
}

/// A running `api` call with a `callId` (1.2.4).
struct Running {
    serial: u64,
    abort: tokio::task::AbortHandle,
}

static CALL_SERIAL: AtomicU64 = AtomicU64::new(1);

/// One window's engine connection: its client id and engine (in the plan and
/// the link), the workspace, the last reported chat, the state, the batch and
/// its doorbell, and the running `callId` calls.
pub struct WindowConn {
    label: String,
    ring: Ring,
    tuning: Tuning,
    /// `-d`: the app's debug log (START-010).
    debug: bool,
    queue: Mutex<Queue>,
    inner: Mutex<Inner>,
    calls: Mutex<HashMap<String, Running>>,
    /// One open at a time per window.
    opening: tokio::sync::Mutex<()>,
}

impl WindowConn {
    fn new(label: &str, ring: Ring, tuning: Tuning, debug: bool) -> Self {
        WindowConn {
            label: label.to_string(),
            ring,
            tuning,
            debug,
            queue: Mutex::new(Queue {
                batcher: Batcher::new(BATCH_MAX),
                rung: false,
                #[cfg(test)]
                pushed: 0,
            }),
            inner: Mutex::new(Inner {
                phase: Phase::Idle,
                plan: None,
                link: None,
                opened: None,
                current_session: None,
                generation: 0,
                counter: 0,
                joining: None,
                joining_lost: false,
                reader: None,
                reconnect: None,
            }),
            calls: Mutex::default(),
            opening: tokio::sync::Mutex::new(()),
        }
    }

    fn inner(&self) -> MutexGuard<'_, Inner> {
        self.inner.lock().unwrap()
    }

    fn log(&self, what: &str) {
        eprintln!("crush-desktop: [{}] {what}", self.label);
    }

    fn debug_log(&self, what: &str) {
        if self.debug {
            self.log(what);
        }
    }

    // The batch and its doorbell (1.2.5).

    /// Adds events to the batch; true when they are the first since a take,
    /// so the window's doorbell is due.
    fn enqueue(&self, events: Vec<Event>) -> bool {
        if events.is_empty() {
            return false;
        }
        let mut queue = self.queue.lock().unwrap();
        #[cfg(test)]
        {
            queue.pushed += events.len();
        }
        for ev in events {
            queue.batcher.push(ev);
        }
        !std::mem::replace(&mut queue.rung, true)
    }

    fn ring_if(&self, due: bool) {
        if due {
            (self.ring)();
        }
    }

    fn push_all(&self, events: Vec<Event>) {
        let due = self.enqueue(events);
        self.ring_if(due);
    }

    fn take(&self) -> Vec<Value> {
        let mut queue = self.queue.lock().unwrap();
        queue.rung = false;
        #[cfg(test)]
        {
            queue.pushed = 0;
        }
        queue.batcher.take().into_iter().map(fixture::wire_entry).collect()
    }

    // Join (1.2.6).

    /// Join steps 1-8, once; a second open while joined answers the same result.
    async fn open(self: &Arc<Self>, plan: impl Future<Output = AppResult<Plan>>) -> AppResult<OpenResult> {
        let _one_at_a_time = self.opening.lock().await;
        {
            let mut inner = self.inner();
            if inner.phase == Phase::Closed {
                return Err(closed());
            }
            if let Some(opened) = inner.opened.clone() {
                if inner.phase == Phase::Failed {
                    // "Try again" after a failure: the reconnect starts over.
                    self.log("trying the engine again");
                    self.start_reconnect(&mut inner);
                }
                return Ok(opened);
            }
        }
        let plan = match plan.await {
            Ok(plan) => Arc::new(plan),
            Err(e) => {
                self.log(&format!("could not start: {} ({})", e.message, e.code));
                return Err(e);
            }
        };
        self.inner().plan = Some(plan.clone());
        match self.join(&plan, 0).await {
            Ok(opened) => Ok(opened),
            Err((e, holder)) => {
                if let Some(http) = holder {
                    // That engine holds a claim for this client: give it back.
                    // The next open plans again, with a new client id.
                    self.retire(&http, &plan.client_id).await;
                }
                self.log(&format!("could not join the engine: {} ({})", e.message, e.code));
                Err(e)
            }
        }
    }

    /// Join steps 2-8 (the first join, attempt 0, and every rejoin): the
    /// engine, its version, the workspace, the event stream, agent init and
    /// the current chat. On success the window is connected and its
    /// `connected` event is in the batch. A failure after the create also
    /// answers the engine that holds this client's claim.
    async fn join(self: &Arc<Self>, plan: &Arc<Plan>, attempt: u32) -> Result<OpenResult, (AppError, Option<Arc<EngineHttp>>)> {
        let limit = Some(self.tuning.call_limit);
        self.still_open().map_err(|e| (e, None))?;
        let (mut engine, mut http, mut version) = self.dial(plan).await.map_err(|e| (e, None))?;
        self.still_open().map_err(|e| (e, None))?;
        let workspace = match self.create(&http, plan).await {
            Err(e) if e.code == "shutting_down" => {
                // The engine's exit timer fired between ensure and create: ensure
                // again and create once more; a second 503 is an error (1.2.6 step 4).
                self.debug_log("the engine was closing; ensuring again");
                (engine, http, version) = self.dial(plan).await.map_err(|e| (e, None))?;
                self.create(&http, plan).await.map_err(|e| (e, None))?
            }
            other => other.map_err(|e| (e, None))?,
        };
        let held = |e: AppError| (e, Some(http.clone()));
        let workspace_id = workspace
            .get("id")
            .and_then(Value::as_str)
            .filter(|id| !id.is_empty())
            .map(String::from)
            .ok_or_else(|| held(AppError::new("internal", "The Crush engine answered the workspace create without an id.")))?;
        let base = format!("/v1/workspaces/{}", encode(&workspace_id));
        let client = format!("client_id={}", encode(&plan.client_id));

        let generation = self.begin_join();
        let stream = match http.stream(&format!("{base}/events?{client}")).await {
            Ok(stream) => stream,
            Err(e) => {
                self.end_join(generation);
                return Err(held(e));
            }
        };
        let reader = Owned(Some(self.start_reader(stream, generation)));

        // Agent init: a failure is logged, not fatal, as in the terminal
        // (internal/cmd/workspace_setup.go:204-208): a project with no model
        // yet still opens, for onboarding.
        let init = json!({"interactive": true});
        if let Err(e) = http.call("POST", &format!("{base}/agent/init?{client}"), Some(&init), limit).await {
            self.log(&format!("agent init failed: {} ({})", e.message, e.code));
        }
        let reported = self.inner().current_session.clone();
        if let Some(session_id) = &reported {
            self.send_current_session(&http, &base, &client, session_id).await;
        }

        let opened = OpenResult {
            client_id: plan.client_id.clone(),
            workspace: public_workspace(workspace),
            engine,
            version: plan.version.clone().unwrap_or(version),
            sent_yolo: plan.sent_yolo,
            cwd: plan.cwd.clone(),
        };
        let link = Link {
            http: http.clone(),
            workspace_id,
        };
        let opened = self.install(generation, reader, link, opened, attempt).map_err(held)?;
        // A chat reported while this join ran was only remembered: send it now.
        let now = self.inner().current_session.clone();
        if let Some(session_id) = now.filter(|now| reported.as_ref() != Some(now)) {
            self.send_current_session(&http, &base, &client, &session_id).await;
        }
        Ok(opened)
    }

    /// Join steps 2-3: ensure (or the -H address), then GET /v1/version,
    /// whose protocol must be the app's.
    async fn dial(&self, plan: &Arc<Plan>) -> AppResult<(Ensured, Arc<EngineHttp>, EngineVersion)> {
        let finder = plan.finder.clone();
        let mut engine = blocking(move || finder.find()).await?;
        let http = Arc::new(EngineHttp::new(&engine.socket));
        let limit = Some(self.tuning.call_limit);
        let info = http.call("GET", "/v1/version", None, limit).await?.body;
        let protocol = info.get("protocol").and_then(Value::as_i64).unwrap_or(0);
        if protocol != APP_PROTOCOL {
            return Err(AppError::new(
                "protocol_mismatch",
                format!("The Crush engine speaks protocol {protocol}; Crush Desktop needs protocol {APP_PROTOCOL}."),
            ));
        }
        let version = EngineVersion {
            version: info.get("version").and_then(Value::as_str).unwrap_or_default().to_string(),
            protocol,
        };
        if !plan.finder.ensures() {
            // -H: what ensure would have said, from the engine itself (proto.EngineInfo).
            engine.version = version.version.clone();
            engine.protocol = protocol;
            if let Ok(Reply { body, .. }) = http.call("GET", "/v1/engine", None, limit).await {
                engine.pid = body.get("pid").and_then(Value::as_i64).unwrap_or(0);
                engine.data_dir = body.get("data_dir").and_then(Value::as_str).unwrap_or_default().to_string();
            }
        }
        self.debug_log(&format!("dialed the engine at {} (pid {})", engine.socket, engine.pid));
        Ok((engine, http, version))
    }

    /// Join step 4: create the workspace, or join the live one.
    async fn create(&self, http: &EngineHttp, plan: &Plan) -> AppResult<Value> {
        let mut body = plan.body.clone();
        let env = (plan.env)();
        body["env"] = Value::Array(env.into_iter().map(|(k, v)| Value::String(format!("{k}={v}"))).collect());
        // Never logged: the body holds the environment.
        let reply = http.call("POST", "/v1/workspaces", Some(&body), Some(self.tuning.call_limit)).await;
        reply.map(|r| r.body)
    }

    async fn send_current_session(&self, http: &EngineHttp, base: &str, client: &str, session_id: &str) {
        let body = json!({"session_id": session_id});
        let target = format!("{base}/current-session?{client}");
        if let Err(e) = http.call("POST", &target, Some(&body), Some(self.tuning.call_limit)).await {
            self.log(&format!("could not report the shown chat: {} ({})", e.message, e.code));
        }
    }

    fn still_open(&self) -> AppResult<()> {
        match self.inner().phase {
            Phase::Closed => Err(closed()),
            _ => Ok(()),
        }
    }

    fn begin_join(&self) -> u64 {
        let mut inner = self.inner();
        inner.counter += 1;
        inner.joining = Some(inner.counter);
        inner.joining_lost = false;
        inner.counter
    }

    fn end_join(&self, generation: u64) {
        let mut inner = self.inner();
        if inner.joining == Some(generation) {
            inner.joining = None;
            inner.joining_lost = false;
        }
    }

    /// The last join step: the window is connected, and its `connected` event
    /// goes into the batch under the same lock as the state change, so it
    /// keeps its order with the reconnect's events.
    fn install(&self, generation: u64, reader: Owned, link: Link, opened: OpenResult, attempt: u32) -> AppResult<OpenResult> {
        let due = {
            let mut inner = self.inner();
            let lost = inner.joining == Some(generation) && inner.joining_lost;
            if inner.joining == Some(generation) {
                inner.joining = None;
                inner.joining_lost = false;
            }
            if inner.phase == Phase::Closed {
                return Err(closed());
            }
            if lost {
                return Err(AppError::new("transport", "The Crush engine's event stream ended while the window joined it."));
            }
            let fresh_engine = attempt > 0 && inner.link.as_ref().is_some_and(|old| old.workspace_id != link.workspace_id);
            inner.phase = Phase::Connected;
            inner.generation = generation;
            inner.link = Some(link);
            inner.reader = Some(reader.keep());
            inner.reconnect = None;
            inner.opened = Some(opened.clone());
            self.enqueue(vec![connection_event(json!({
                "state": "connected",
                "attempt": attempt,
                "resync": attempt > 0,
                "fresh_engine": fresh_engine,
                "workspace": opened.workspace,
                "engine": opened.engine,
            }))])
        };
        self.ring_if(due);
        self.log(&format!(
            "joined workspace {} on the engine at {} (pid {})",
            opened.workspace.get("id").and_then(Value::as_str).unwrap_or_default(),
            opened.engine.socket,
            opened.engine.pid
        ));
        Ok(opened)
    }

    /// Join step 5's reader: the event stream's lines into the batch. Its end
    /// starts a reconnect (1.2.5 step 4).
    fn start_reader(self: &Arc<Self>, mut stream: ByteStream, generation: u64) -> JoinHandle<()> {
        let me = self.clone();
        tauri::async_runtime::spawn(async move {
            let mut lines = LineSplitter::new();
            let mut out = Vec::new();
            while let Some(Ok(chunk)) = stream.next_chunk().await {
                lines.push(&chunk, &mut out);
                me.push_all(out.drain(..).filter_map(|line| batch::parse_sse_line(&line)).collect());
            }
            drop(stream);
            me.lost(generation, "the event stream ended");
        })
    }

    /// Gives a client's claim back (a failed open, or a closing window).
    async fn retire(&self, http: &EngineHttp, client_id: &str) {
        let path = format!("/v1/clients/{}", encode(client_id));
        if let Err(e) = http.call("DELETE", &path, None, Some(self.tuning.close_limit)).await {
            self.log(&format!("could not leave the engine: {} ({})", e.message, e.code));
        }
    }

    // Losing the engine (1.2.6).

    /// Starts one reconnect; a second trigger while one runs, or a trigger
    /// from an older join, is ignored.
    fn lost(self: &Arc<Self>, generation: u64, why: &str) {
        let mut inner = self.inner();
        if inner.joining == Some(generation) {
            inner.joining_lost = true;
            return;
        }
        if inner.phase != Phase::Connected || inner.generation != generation {
            return;
        }
        self.log(&format!("lost the engine ({why}); reconnecting"));
        if let Some(reader) = inner.reader.take() {
            reader.abort();
        }
        self.start_reconnect(&mut inner);
    }

    fn start_reconnect(self: &Arc<Self>, inner: &mut Inner) {
        inner.phase = Phase::Reconnecting;
        let me = self.clone();
        inner.reconnect = Some(tauri::async_runtime::spawn(async move { me.reconnect().await }));
    }

    /// Ensure and rejoin until it works; after `max_failures` failed tries in a
    /// row, "failed" until "Try again". Waits between tries: 250 ms, doubling to 10 s.
    async fn reconnect(self: Arc<Self>) {
        let Some(plan) = self.inner().plan.clone() else { return };
        let mut backoff = Backoff::new(self.tuning.first_wait, self.tuning.max_wait);
        let mut failures = 0;
        let mut attempt = 0;
        loop {
            attempt += 1;
            let due = {
                let inner = self.inner();
                if inner.phase != Phase::Reconnecting {
                    return;
                }
                self.enqueue(vec![connection_event(json!({
                    "state": "reconnecting", "attempt": attempt, "resync": false, "fresh_engine": false,
                }))])
            };
            self.ring_if(due);
            let error = match self.join(&plan, attempt).await {
                Ok(_) => return,
                Err((e, _)) => e,
            };
            failures += 1;
            self.log(&format!("reconnect try {attempt} failed: {} ({})", error.message, error.code));
            if failures >= self.tuning.max_failures {
                let due = {
                    let mut inner = self.inner();
                    if inner.phase != Phase::Reconnecting {
                        return;
                    }
                    inner.phase = Phase::Failed;
                    inner.reconnect = None;
                    self.enqueue(vec![connection_event(json!({
                        "state": "failed", "attempt": attempt, "resync": false, "fresh_engine": false, "error": error,
                    }))])
                };
                self.ring_if(due);
                return;
            }
            tokio::time::sleep(backoff.next()).await;
        }
    }

    /// "Try again" after "failed": the reconnect starts over.
    fn retry(self: &Arc<Self>) -> AppResult<()> {
        let mut inner = self.inner();
        match inner.phase {
            Phase::Failed => {
                self.log("trying the engine again");
                self.start_reconnect(&mut inner);
                Ok(())
            }
            Phase::Connected | Phase::Reconnecting => Ok(()),
            Phase::Idle | Phase::Closed => Err(not_open()),
        }
    }

    // Calls (1.3).

    /// The link while connected; `engine_unavailable` at once otherwise (nothing is sent).
    fn ready(&self) -> AppResult<(Link, String, u64)> {
        let inner = self.inner();
        let plan = inner.plan.as_ref().filter(|_| inner.opened.is_some());
        match (inner.phase, &inner.link, plan) {
            (Phase::Connected, Some(link), Some(plan)) => Ok((link.clone(), plan.client_id.clone(), inner.generation)),
            (Phase::Reconnecting, ..) => Err(AppError::new(
                "engine_unavailable",
                "Crush Desktop is reconnecting to the Crush engine; nothing was sent.",
            )),
            (Phase::Failed, ..) => Err(AppError::new(
                "engine_unavailable",
                "Crush Desktop lost its connection to the Crush engine; nothing was sent.",
            )),
            _ => Err(not_open()),
        }
    }

    async fn call(
        self: &Arc<Self>,
        method: &str,
        path: &str,
        query: Option<Map<String, Value>>,
        body: Option<Value>,
        call_id: Option<String>,
    ) -> AppResult<Value> {
        let method = method.to_ascii_uppercase();
        let route = Route::find(&method, path)?;
        let (link, client_id, generation) = self.ready()?;
        let target = route.target(&link.workspace_id, query.as_ref(), &client_id);
        let started = Instant::now();
        let result = match call_id {
            None => link.http.call(&method, &target, body.as_ref(), Some(self.tuning.call_limit)).await,
            Some(id) => self.call_with_id(id, link.http.clone(), method.clone(), target, body).await,
        };
        let took = started.elapsed().as_millis();
        match &result {
            Ok(reply) => self.debug_log(&format!("{method} {path}: {} in {took} ms", reply.status)),
            Err(e) => {
                self.debug_log(&format!("{method} {path}: {} in {took} ms", e.code));
                if lost_engine(e) {
                    self.lost(generation, &e.code);
                }
            }
        }
        result.map(|reply| reply.body)
    }

    /// A call with a `callId`: no time limit, and `abort` stops it until it ends (1.2.4).
    async fn call_with_id(&self, id: String, http: Arc<EngineHttp>, method: String, target: String, body: Option<Value>) -> AppResult<Reply> {
        let serial = CALL_SERIAL.fetch_add(1, Ordering::Relaxed);
        // Listed before it can run, so `abort` never misses it.
        let task = {
            let mut calls = self.calls.lock().unwrap();
            let task = tauri::async_runtime::spawn(async move { http.call(&method, &target, body.as_ref(), None).await });
            let abort = task.inner().abort_handle();
            if calls.insert(id.clone(), Running { serial, abort }).is_some() {
                self.log("a call took the id of a call still running; abort now stops the new one");
            }
            task
        };
        let _listed = Listed {
            calls: &self.calls,
            id,
            serial,
            abort: task.inner().abort_handle(),
        };
        match task.await {
            Ok(result) => result,
            Err(_) => Err(AppError::new("aborted", "The call was stopped.")),
        }
    }

    fn abort_call(&self, call_id: &str) -> bool {
        match self.calls.lock().unwrap().remove(call_id) {
            Some(running) => {
                running.abort.abort();
                true
            }
            None => false,
        }
    }

    async fn set_current_session(self: &Arc<Self>, session_id: &str) -> AppResult<()> {
        let (link, client_id, generation) = {
            let mut inner = self.inner();
            if inner.opened.is_none() || inner.phase == Phase::Closed {
                return Err(not_open());
            }
            inner.current_session = Some(session_id.to_string());
            match (&inner.phase, &inner.link, &inner.plan) {
                (Phase::Connected, Some(link), Some(plan)) => (link.clone(), plan.client_id.clone(), inner.generation),
                // Remembered: the rejoin sends it (1.2.6 step 3).
                _ => return Ok(()),
            }
        };
        let target = format!(
            "/v1/workspaces/{}/current-session?client_id={}",
            encode(&link.workspace_id),
            encode(&client_id)
        );
        let body = json!({"session_id": session_id});
        match link.http.call("POST", &target, Some(&body), Some(self.tuning.call_limit)).await {
            Ok(_) => Ok(()),
            Err(e) => {
                if lost_engine(&e) {
                    self.lost(generation, &e.code);
                }
                Err(e)
            }
        }
    }

    /// The window is closing: stop its calls, leave the engine, stop reading.
    async fn close(&self) {
        let running: Vec<Running> = self.calls.lock().unwrap().drain().map(|(_, r)| r).collect();
        for call in running {
            call.abort.abort();
        }
        let (link, client_id, reader, reconnect) = {
            let mut inner = self.inner();
            inner.phase = Phase::Closed;
            let client_id = inner.plan.as_ref().map(|p| p.client_id.clone());
            (inner.link.take(), client_id, inner.reader.take(), inner.reconnect.take())
        };
        if let Some(task) = reconnect {
            task.abort();
        }
        if let (Some(link), Some(client_id)) = (link, client_id) {
            self.retire(&link.http, &client_id).await;
        }
        if let Some(reader) = reader {
            reader.abort();
        }
    }
}

/// The app's own connection event (1.2.5): a barrier in the batch.
fn connection_event(payload: Value) -> Event {
    Event {
        ty: "connection".to_string(),
        kind: Kind::Updated,
        payload,
    }
}

/// A task that stops when this handle is dropped, unless it was kept: a
/// join given up halfway leaves no reader behind.
struct Owned(Option<JoinHandle<()>>);

impl Owned {
    fn keep(mut self) -> JoinHandle<()> {
        self.0.take().expect("a task is kept once")
    }
}

impl Drop for Owned {
    fn drop(&mut self) {
        if let Some(task) = self.0.take() {
            task.abort();
        }
    }
}

/// While a `callId` call runs: removes its abort entry when it ends, however
/// it ends (unless a newer call took the id), and stops it when its caller
/// goes away.
struct Listed<'a> {
    calls: &'a Mutex<HashMap<String, Running>>,
    id: String,
    serial: u64,
    abort: tokio::task::AbortHandle,
}

impl Drop for Listed<'_> {
    fn drop(&mut self) {
        self.abort.abort();
        let mut calls = self.calls.lock().unwrap();
        if calls.get(&self.id).is_some_and(|r| r.serial == self.serial) {
            calls.remove(&self.id);
        }
    }
}

/// An answer that means the engine is gone: nothing listens on its socket, or
/// it is on its way out (503 shutting_down).
fn lost_engine(e: &AppError) -> bool {
    e.code == "engine_unavailable" || (e.code == "shutting_down" && e.status == Some(503))
}

fn not_open() -> AppError {
    AppError::new(
        "engine_unavailable",
        "Crush Desktop has not joined a Crush engine in this window; nothing was sent.",
    )
}

fn closed() -> AppError {
    AppError::new("aborted", "The window closed.")
}

// The commands' side (1.3): one connection per window label in AppState.

fn connection(app: &tauri::AppHandle, label: &str) -> Option<Arc<WindowConn>> {
    app.state::<AppState>().windows.lock().unwrap().get(label).cloned()
}

/// Version check, ensure, join steps 1-4, stream start. Called once per window, and again by
/// "Try again" after a failure. Idempotent: a second call while connected returns the same result.
pub async fn open(app: &tauri::AppHandle, label: &str, cwd: &str) -> AppResult<OpenResult> {
    let state = app.state::<AppState>();
    let launch = state.launch.clone();
    let (crush_path, settings_yolo) = {
        let settings = state.settings.lock().unwrap();
        (settings.crush_path.clone(), settings.yolo)
    };
    let conn = {
        let mut windows = state.windows.lock().unwrap();
        windows
            .entry(label.to_string())
            .or_insert_with(|| {
                let (app, target) = (app.clone(), label.to_string());
                let ring: Ring = Arc::new(move || {
                    let _ = app.emit_to(target.as_str(), "events-ready", ());
                });
                Arc::new(WindowConn::new(label, ring, TUNING, launch.debug))
            })
            .clone()
    };
    let env: EnvSource = Arc::new(login_env::login_env);
    let plan = async move { plan_for(&launch, settings_yolo, cwd, env, crush_check(crush_path)).await };
    conn.open(plan).await
}

pub async fn retry(app: &tauri::AppHandle, label: &str) -> AppResult<()> {
    connection(app, label).ok_or_else(not_open)?.retry()
}

/// Generic call for the window (section 1.3, `api`): adds client_id, checks the route list.
/// With `call_id`: no time limit, and the call is registered for `abort` until it ends (1.2.4).
pub async fn call(
    app: &tauri::AppHandle,
    label: &str,
    method: &str,
    path: &str,
    query: Option<serde_json::Map<String, serde_json::Value>>,
    body: Option<serde_json::Value>,
    call_id: Option<String>,
) -> AppResult<serde_json::Value> {
    let Some(conn) = connection(app, label) else {
        Route::find(&method.to_ascii_uppercase(), path)?;
        return Err(not_open());
    };
    conn.call(method, path, query, body, call_id).await
}

/// `api_abort` (1.3): aborts that window's running call with this id; false when none is
/// running. The aborted call fails with code "aborted" (1.2.2).
pub fn abort(app: &tauri::AppHandle, label: &str, call_id: &str) -> bool {
    connection(app, label).is_some_and(|conn| conn.abort_call(call_id))
}

pub async fn set_current_session(
    app: &tauri::AppHandle,
    label: &str,
    session_id: &str,
) -> AppResult<()> {
    connection(app, label).ok_or_else(not_open)?.set_current_session(session_id).await
}

/// The wire form of 1.4.2.
pub fn take(app: &tauri::AppHandle, label: &str) -> Vec<serde_json::Value> {
    connection(app, label).map(|conn| conn.take()).unwrap_or_default()
}

/// Window closing: abort every running call of that window (so its `!` commands end with it,
/// 1.2.4), then DELETE /v1/clients/{client_id} (1 s limit, errors only logged), stop the reader.
pub async fn close(app: &tauri::AppHandle, label: &str) {
    let conn = app.state::<AppState>().windows.lock().unwrap().remove(label);
    if let Some(conn) = conn {
        conn.close().await;
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::crush_bin::BinSource;
    use crate::engine_cli::tests::{scratch, script};
    use crate::uds_http::fake::{Answer, FakeEngine, Seen};
    use std::path::{Path, PathBuf};
    use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering::SeqCst};

    fn run<T>(f: impl Future<Output = T>) -> T {
        tauri::async_runtime::block_on(f)
    }

    /// The app's rhythm, with calls cut off sooner so a stuck test fails fast.
    const TEST_TUNING: Tuning = Tuning {
        call_limit: Duration::from_secs(5),
        ..TUNING
    };
    const WS: &str = "ws-1";

    /// A fake engine whose workspace is `ws_id`. The create answer echoes
    /// the request, `env` included, and adds a `config`, as the engine does.
    fn engine(ws_id: &'static str) -> impl Fn(&Seen) -> Answer + Send + Sync + 'static {
        move |seen: &Seen| match (seen.method.as_str(), seen.path()) {
            ("GET", "/v1/version") => Answer::Json(200, json!({"version": "v0.0.0-test", "protocol": 1})),
            ("GET", "/v1/engine") => Answer::Json(200, json!({"pid": 4343, "data_dir": "/tmp/fake/.crush", "protocol": 1})),
            ("POST", "/v1/workspaces") => {
                let mut ws = seen.json();
                ws["id"] = json!(ws_id);
                ws["config"] = json!({"providers": {"openai": {"api_key": "sk-from-config"}}});
                Answer::Json(200, ws)
            }
            ("GET", path) if path.ends_with("/events") => Answer::Events,
            ("DELETE", _) => Answer::Status(204),
            _ => Answer::Json(200, json!({"echo": seen.target})),
        }
    }

    /// `engine(ws_id)`, except where `special` answers.
    fn engine_with(
        ws_id: &'static str,
        special: impl Fn(&Seen) -> Option<Answer> + Send + Sync + 'static,
    ) -> impl Fn(&Seen) -> Answer + Send + Sync + 'static {
        let base = engine(ws_id);
        move |seen: &Seen| special(seen).unwrap_or_else(|| base(seen))
    }

    /// `crush engine ensure`, faked: it answers the fake engine's socket, or
    /// fails while `fail` is set, and notes when it ran.
    struct FakeFinder {
        socket: String,
        fail: AtomicBool,
        runs: Mutex<Vec<Instant>>,
    }

    impl FakeFinder {
        fn new(fake: &FakeEngine) -> Arc<FakeFinder> {
            Arc::new(FakeFinder {
                socket: fake.socket.to_string_lossy().into_owned(),
                fail: AtomicBool::new(false),
                runs: Mutex::default(),
            })
        }

        fn runs(&self) -> Vec<Instant> {
            self.runs.lock().unwrap().clone()
        }
    }

    impl Finder for FakeFinder {
        fn find(&self) -> AppResult<Ensured> {
            self.runs.lock().unwrap().push(Instant::now());
            if self.fail.load(SeqCst) {
                return Err(AppError::new("start_failed", "The engine did not start."));
            }
            Ok(Ensured {
                address: format!("unix://{}", self.socket),
                socket: self.socket.clone(),
                protocol: 1,
                version: "v0.0.0-test".into(),
                pid: 4242,
                data_dir: "/tmp/fake/.crush".into(),
                started: true,
            })
        }
        fn ensures(&self) -> bool {
            true
        }
    }

    /// The login shell's environment, as the tests give it.
    fn env() -> EnvSource {
        Arc::new(|| vec![("PATH".into(), "/usr/bin:/bin".into()), ("P31_KEY".into(), "sk-from-login".into())])
    }

    /// A plan as `plan_for` makes one, with `finder` in place of crush.
    fn plan(finder: Arc<dyn Finder>) -> Plan {
        let client_id = uuid::Uuid::new_v4().to_string();
        Plan {
            cwd: "/tmp/proj".into(),
            body: create_body("/tmp/proj", None, &client_id, true, false, &[]),
            client_id,
            finder,
            version: Some(EngineVersion { version: "v0.0.0-test".into(), protocol: 1 }),
            env: env(),
            sent_yolo: true,
        }
    }

    /// A window whose doorbell counts its rings.
    fn window(tuning: Tuning, debug: bool) -> (Arc<WindowConn>, Arc<AtomicUsize>) {
        let rings = Arc::new(AtomicUsize::new(0));
        let counter = rings.clone();
        let ring: Ring = Arc::new(move || {
            counter.fetch_add(1, SeqCst);
        });
        (Arc::new(WindowConn::new("main", ring, tuning, debug)), rings)
    }

    /// A window joined to a new fake engine (workspace `ws-1`); it closes when dropped.
    struct Joined {
        fake: FakeEngine,
        finder: Arc<FakeFinder>,
        conn: Arc<WindowConn>,
        rings: Arc<AtomicUsize>,
        opened: OpenResult,
    }

    fn joined(name: &str, tuning: Tuning) -> Joined {
        let fake = FakeEngine::start(name, engine(WS));
        let finder = FakeFinder::new(&fake);
        let (conn, rings) = window(tuning, false);
        let opened = run(conn.open(std::future::ready(Ok(plan(finder.clone()))))).unwrap();
        Joined { fake, finder, conn, rings, opened }
    }

    impl Drop for Joined {
        fn drop(&mut self) {
            run(self.conn.close());
        }
    }

    /// Takes the window's batch until a `connection` event in `state` comes
    /// (10 s at most); answers everything taken.
    fn take_until(conn: &WindowConn, state: &str) -> Vec<Value> {
        let end = Instant::now() + Duration::from_secs(10);
        let mut taken = Vec::new();
        loop {
            taken.extend(conn.take());
            let arrived = taken.iter().any(|e| e["type"] == "connection" && e["payload"]["state"] == state);
            if arrived || Instant::now() > end {
                return taken;
            }
            std::thread::sleep(Duration::from_millis(5));
        }
    }

    /// The connection events among `taken`, as (state, attempt).
    fn states(taken: &[Value]) -> Vec<(String, u64)> {
        taken
            .iter()
            .filter(|e| e["type"] == "connection")
            .map(|e| {
                let p = &e["payload"];
                (p["state"].as_str().unwrap_or_default().to_string(), p["attempt"].as_u64().unwrap_or(99))
            })
            .collect()
    }

    fn pairs(list: &[(&str, u64)]) -> Vec<(String, u64)> {
        list.iter().map(|(s, n)| (s.to_string(), *n)).collect()
    }

    /// The last `connection` event in `state` among `taken`.
    fn payload(taken: &[Value], state: &str) -> Value {
        taken
            .iter()
            .rev()
            .find(|e| e["type"] == "connection" && e["payload"]["state"] == state)
            .map(|e| e["payload"].clone())
            .unwrap_or_default()
    }

    fn wait_pushed(conn: &WindowConn, n: usize) -> bool {
        let end = Instant::now() + Duration::from_secs(5);
        while conn.queue.lock().unwrap().pushed < n {
            if Instant::now() > end {
                return false;
            }
            std::thread::sleep(Duration::from_millis(2));
        }
        true
    }

    fn lines(seen: &[Seen]) -> Vec<String> {
        seen.iter().map(Seen::line).collect()
    }

    #[test]
    fn one_doorbell_per_batch() {
        let (conn, rings) = window(TEST_TUNING, false);
        let message = |id: &str| Event { ty: "message".into(), kind: Kind::Created, payload: json!({"id": id}) };
        conn.push_all(vec![message("m1")]);
        conn.push_all(vec![message("m2"), message("m3")]);
        conn.push_all(vec![connection_event(json!({"state": "connected"}))]);
        assert_eq!(rings.load(SeqCst), 1, "one doorbell for everything before a take");
        let taken = conn.take();
        assert_eq!(taken.len(), 4);
        assert_eq!(taken[0], json!({"kind": "event", "type": "message", "event": "created", "payload": {"id": "m1"}}));
        assert_eq!(taken[3], json!({"kind": "event", "type": "connection", "event": "updated", "payload": {"state": "connected"}}));
        assert!(conn.take().is_empty());
        conn.push_all(Vec::new());
        assert_eq!(rings.load(SeqCst), 1, "nothing at all while the engine is quiet");
        conn.push_all(vec![message("m4")]);
        conn.push_all(vec![message("m5")]);
        assert_eq!(rings.load(SeqCst), 2, "the next batch rings once more");
    }

    #[test]
    fn stream_events_reach_the_batch_in_order() {
        let j = joined("conn-events", TEST_TUNING);
        assert_eq!(j.rings.load(SeqCst), 1, "the connected event rang");
        assert_eq!(states(&j.conn.take()), pairs(&[("connected", 0)]));
        let text = concat!(
            "data: {\"type\":\"message\",\"payload\":{\"type\":\"created\",\"payload\":{\"id\":\"m1\",\"session_id\":\"s1\"}}}\n\n",
            "data: {\"type\":\"session\",\"payload\":{\"type\":\"updated\",\"payload\":{\"id\":\"s1\",\"title\":\"hi\"}}}\r\n\r\n",
            "data: {\"type\":\"message\",\"payload\":{\"type\":\"deleted\",\"payload\":{\"id\":\"m0\"}}}\n\n",
        );
        // Three chunks that cut lines in pieces.
        let (one, rest) = text.split_at(25);
        let (two, three) = rest.split_at(100);
        for chunk in [one, two, three] {
            j.fake.send(chunk);
        }
        assert!(wait_pushed(&j.conn, 3), "three events arrived");
        assert_eq!(j.rings.load(SeqCst), 2, "one doorbell for the three");
        let taken = j.conn.take();
        let got: Vec<(&str, &str, &str)> = taken
            .iter()
            .map(|e| (e["type"].as_str().unwrap(), e["event"].as_str().unwrap(), e["payload"]["id"].as_str().unwrap()))
            .collect();
        assert_eq!(got, [("message", "created", "m1"), ("session", "updated", "s1"), ("message", "deleted", "m0")]);
    }

    #[test]
    fn backoff_250ms_doubling_to_10s() {
        let mut backoff = Backoff::new(TUNING.first_wait, TUNING.max_wait);
        let waits: Vec<u128> = (0..9).map(|_| backoff.next().as_millis()).collect();
        assert_eq!(waits, [250, 500, 1000, 2000, 4000, 8000, 10000, 10000, 10000]);
        assert_eq!(TUNING.max_failures, 3);
        assert_eq!(TUNING.call_limit, Duration::from_secs(30));
    }

    #[test]
    fn failed_after_3_then_try_again_reconnects() {
        let (conn, _) = window(TEST_TUNING, false);
        assert_eq!(conn.retry().err().unwrap().code, "engine_unavailable", "nothing to try again before a join");
        let j = joined("conn-failed", TEST_TUNING);
        assert!(j.conn.retry().is_ok(), "Try again while connected does nothing");
        j.conn.take();
        j.finder.fail.store(true, SeqCst);
        j.finder.runs.lock().unwrap().clear();
        j.fake.forget();
        j.fake.end_streams();
        let taken = take_until(&j.conn, "failed");
        assert_eq!(
            states(&taken),
            pairs(&[("reconnecting", 1), ("reconnecting", 2), ("reconnecting", 3), ("failed", 3)])
        );
        let failed = payload(&taken, "failed");
        assert_eq!(failed["error"], json!({"code": "start_failed", "message": "The engine did not start."}));
        let runs = j.finder.runs();
        assert_eq!(runs.len(), 3, "ensure ran three times");
        assert!(runs[1] - runs[0] >= Duration::from_millis(250), "250 ms before the second try");
        assert!(runs[2] - runs[1] >= Duration::from_millis(500), "500 ms before the third");
        std::thread::sleep(Duration::from_millis(1200));
        assert_eq!(j.finder.runs().len(), 3, "failed stays failed until Try again");
        assert!(j.conn.take().is_empty());
        let e = run(j.conn.call("GET", "sessions", None, None, None)).err().unwrap();
        assert_eq!(e.code, "engine_unavailable");
        assert!(j.fake.seen().is_empty(), "nothing was sent");

        // Try again while ensure still fails: three more tries, failed again.
        j.conn.retry().unwrap();
        let taken = take_until(&j.conn, "failed");
        assert_eq!(states(&taken).first(), Some(&("reconnecting".to_string(), 1)), "the count starts over");
        assert_eq!(states(&taken).last(), Some(&("failed".to_string(), 3)));
        // Try again (here through engine_open, which also starts over) once ensure works.
        j.finder.fail.store(false, SeqCst);
        let again = run(j.conn.open(std::future::ready(Err(AppError::new("internal", "planned again"))))).unwrap();
        assert_eq!(again.client_id, j.opened.client_id);
        let taken = take_until(&j.conn, "connected");
        assert_eq!(states(&taken), pairs(&[("reconnecting", 1), ("connected", 1)]));
        let connected = payload(&taken, "connected");
        assert_eq!((&connected["resync"], &connected["fresh_engine"]), (&json!(true), &json!(false)));
        let answer = run(j.conn.call("GET", "sessions", None, None, None)).unwrap();
        assert_eq!(answer, json!({"echo": format!("/v1/workspaces/ws-1/sessions?client_id={}", j.opened.client_id)}));
    }

    #[test]
    fn calls_while_reconnecting_give_engine_unavailable() {
        let j = joined("conn-unavailable", TEST_TUNING);
        j.conn.take();
        j.finder.fail.store(true, SeqCst);
        j.fake.end_streams();
        take_until(&j.conn, "reconnecting");
        j.fake.forget();
        let started = Instant::now();
        for (method, path, call_id) in [
            ("GET", "sessions", None),
            ("POST", "agent", None),
            ("GET", "/v1/version", None),
            ("POST", "agent/sessions/s1/shell", Some("c-1".to_string())),
        ] {
            let e = run(j.conn.call(method, path, None, Some(json!({"prompt": "hi"})), call_id)).err().unwrap();
            assert_eq!(e.code, "engine_unavailable", "{method} {path}");
        }
        assert!(started.elapsed() < Duration::from_millis(200), "at once");
        // The shown chat is remembered for the rejoin.
        run(j.conn.set_current_session("s9")).unwrap();
        assert!(j.fake.seen().is_empty(), "nothing was sent");
        j.finder.fail.store(false, SeqCst);
        let taken = take_until(&j.conn, "connected");
        assert_eq!(states(&taken).last().unwrap().0, "connected");
        let reported: Vec<Value> = j.fake.seen().iter().filter(|s| s.path().ends_with("/current-session")).map(Seen::json).collect();
        assert_eq!(reported, [json!({"session_id": "s9"})]);
    }

    #[test]
    fn rejoin_order() {
        let j = joined("conn-rejoin", TEST_TUNING);
        run(j.conn.set_current_session("s1")).unwrap();
        j.conn.take();
        let ensured = j.finder.runs().len();
        // The engine dies; a new one, with a new workspace, comes up at the same address.
        j.fake.kill();
        j.fake.forget();
        j.fake.set_handler(engine("ws-2"));
        j.fake.listen();
        let taken = take_until(&j.conn, "connected");
        let states = states(&taken);
        assert_eq!(states.first(), Some(&("reconnecting".to_string(), 1)));
        assert_eq!(states.last().unwrap().0, "connected");
        assert!(!states.iter().any(|(s, _)| s == "failed"));
        assert!(j.finder.runs().len() > ensured, "ensure ran again");
        let seen = j.fake.seen();
        assert_eq!(
            lines(&seen),
            [
                "GET /v1/version",
                "POST /v1/workspaces",
                "GET /v1/workspaces/ws-2/events",
                "POST /v1/workspaces/ws-2/agent/init",
                "POST /v1/workspaces/ws-2/current-session",
            ]
        );
        assert_eq!(seen[1].json()["client_id"], json!(j.opened.client_id), "the same client id");
        for s in &seen[2..] {
            assert_eq!(s.query("client_id").as_deref(), Some(j.opened.client_id.as_str()), "{}", s.line());
        }
        assert_eq!(seen[3].json(), json!({"interactive": true}));
        assert_eq!(seen[4].json(), json!({"session_id": "s1"}), "the last chat, again");
        let connected = payload(&taken, "connected");
        assert_eq!((&connected["resync"], &connected["fresh_engine"]), (&json!(true), &json!(true)));
        assert_eq!(connected["workspace"]["id"], "ws-2");
        let answer = run(j.conn.call("GET", "config", None, None, None)).unwrap();
        assert_eq!(answer["echo"], format!("/v1/workspaces/ws-2/config?client_id={}", j.opened.client_id));
    }

    #[test]
    fn each_trigger_starts_one_reconnect() {
        let j = joined("conn-triggers", TEST_TUNING);
        j.conn.take();
        // A call answered 503 shutting_down.
        j.fake.set_handler(engine_with(WS, |seen| {
            seen.path().ends_with("/sessions").then(|| Answer::Json(503, json!({"message": "server is shutting down"})))
        }));
        let e = run(j.conn.call("GET", "sessions", None, None, None)).err().unwrap();
        assert_eq!(e.code, "shutting_down");
        let taken = take_until(&j.conn, "connected");
        assert_eq!(states(&taken), pairs(&[("reconnecting", 1), ("connected", 1)]));
        // A call that finds nobody at the socket, while the stream still runs.
        std::fs::remove_file(&j.fake.socket).unwrap();
        let e = run(j.conn.call("GET", "config", None, None, None)).err().unwrap();
        assert_eq!(e.code, "engine_unavailable");
        j.fake.listen();
        let taken = take_until(&j.conn, "connected");
        let got = states(&taken);
        assert_eq!(got.first(), Some(&("reconnecting".to_string(), 1)));
        assert_eq!(got.last().unwrap().0, "connected");
        // Two triggers at once: one reconnect.
        j.finder.fail.store(true, SeqCst);
        let ensured = j.finder.runs().len();
        let generation = j.conn.inner().generation;
        j.conn.lost(generation, "first trigger");
        j.conn.lost(generation, "second trigger");
        let taken = take_until(&j.conn, "failed");
        assert_eq!(
            states(&taken),
            pairs(&[("reconnecting", 1), ("reconnecting", 2), ("reconnecting", 3), ("failed", 3)])
        );
        assert_eq!(j.finder.runs().len() - ensured, 3, "three tries of one reconnect");
    }

    #[test]
    fn route_list() {
        let mut design: Vec<(&str, &str)> =
            DESIGN_ROUTES.iter().flat_map(|(m, patterns)| patterns.iter().map(move |p| (*m, *p))).collect();
        let mut listed = ROUTES.to_vec();
        design.sort();
        listed.sort();
        assert_eq!(listed, design, "the route list is design 1.3's table, no more and no less");
        for (method, pattern) in ROUTES {
            let path = pattern
                .replace("{sid}", "s-1")
                .replace("{mid}", "m-1")
                .replace("{kind}", "claude")
                .replace("{lsp}", "gopls");
            assert!(Route::find(method, &path).is_ok(), "{method} {path}");
        }
        for (method, path) in [("POST", "agent/update"), ("GET", "lsps/gopls/diagnostics")] {
            assert!(Route::find(method, path).is_ok(), "{method} {path}");
        }
        for (method, path) in [
            // The workspace itself answers the environment.
            ("GET", "/v1/workspaces"),
            ("GET", "/v1/workspaces/ws-1"),
            ("POST", "/v1/workspaces"),
            ("DELETE", "/v1/workspaces/ws-1"),
            ("GET", "/v1/workspaces/ws-1/sessions"),
            ("DELETE", "/v1/clients/c-1"),
            ("GET", ""),
            ("GET", "/"),
            ("GET", "events"),
            ("POST", "agent/init"),
            ("POST", "current-session"),
            ("POST", "config"),
            ("PATCH", "sessions/s-1"),
            ("GET", "sessions/../config"),
            ("GET", "./sessions"),
            ("GET", "sessions//messages"),
            ("GET", "sessions/s-1/"),
            ("GET", "sessions/s-1/messages/m-1"),
            ("GET", "v1/version"),
            ("GET", "/v1/engine/x"),
            // agent/update and the diagnostics, in no other method or shape.
            ("GET", "agent/update"),
            ("POST", "lsps/gopls/diagnostics"),
            ("GET", "lsps/gopls"),
            ("GET", "lsps//diagnostics"),
            ("GET", "lsps/gopls/diagnostics/x"),
            ("POST", "lsps/start"),
        ] {
            let e = Route::find(method, path).err().unwrap_or_else(|| panic!("{method} {path} was allowed"));
            assert_eq!(e.code, "route_not_allowed", "{method} {path}");
        }
        let mut query = Map::new();
        query.insert("details".into(), json!(true));
        query.insert("path".into(), json!("src/a b&c.rs"));
        query.insert("client_id".into(), json!("someone-else"));
        query.insert("skip".into(), Value::Null);
        let route = Route::find("GET", "agent/sessions/s 1/prompts/list").unwrap();
        assert_eq!(
            route.target("ws/1", Some(&query), "c-1"),
            "/v1/workspaces/ws%2F1/agent/sessions/s%201/prompts/list?details=true&path=src%2Fa%20b%26c.rs&client_id=c-1"
        );
        assert_eq!(Route::find("GET", "/v1/limits").unwrap().target("ws-1", None, "c-1"), "/v1/limits?client_id=c-1");
        assert_eq!(
            Route::find("GET", "lsps/my lsp/diagnostics").unwrap().target("ws-1", None, "c-1"),
            "/v1/workspaces/ws-1/lsps/my%20lsp/diagnostics?client_id=c-1"
        );
        // Through a joined window: refused before anything is sent, even while connected.
        let j = joined("conn-routes", TEST_TUNING);
        j.fake.forget();
        let e = run(j.conn.call("GET", "/v1/workspaces/ws-1", None, None, None)).err().unwrap();
        assert_eq!(e.code, "route_not_allowed");
        assert!(j.fake.seen().is_empty());
        let answer = run(j.conn.call("get", "sessions/s-1", None, None, None)).unwrap();
        assert_eq!(answer["echo"], format!("/v1/workspaces/ws-1/sessions/s-1?client_id={}", j.opened.client_id));
        // The two routes P3.3 needs reach the engine.
        run(j.conn.call("POST", "agent/update", None, None, None)).unwrap();
        run(j.conn.call("GET", "lsps/gopls/diagnostics", None, None, None)).unwrap();
        let seen = j.fake.seen();
        let update = seen.iter().find(|s| s.line() == "POST /v1/workspaces/ws-1/agent/update").expect("agent/update reached the engine");
        assert_eq!(update.header("content-length"), Some("0"), "a POST without a body still says its length");
        assert_eq!(update.query("client_id").as_deref(), Some(j.opened.client_id.as_str()));
        let diagnostics = seen.iter().find(|s| s.line() == "GET /v1/workspaces/ws-1/lsps/gopls/diagnostics").expect("the diagnostics reached the engine");
        assert_eq!(diagnostics.query("client_id").as_deref(), Some(j.opened.client_id.as_str()));
    }

    /// Design 1.3's route table, row by row (method, patterns).
    const DESIGN_ROUTES: &[(&str, &[&str])] = &[
        ("GET", &["/v1/version", "/v1/engine", "/v1/limits"]),
        ("POST", &["/v1/limits/{kind}/refresh"]),
        ("GET", &["config", "providers", "agent", "agent/default-small-model", "git/branch", "lsps", "mcp/states", "skills", "messages/user"]),
        ("GET", &["sessions"]),
        ("POST", &["sessions"]),
        ("GET", &["sessions/{sid}"]),
        ("PUT", &["sessions/{sid}"]),
        ("DELETE", &["sessions/{sid}"]),
        ("GET", &["sessions/{sid}/messages", "sessions/{sid}/messages/user", "sessions/{sid}/messages/{mid}/review", "sessions/{sid}/history", "sessions/{sid}/filetracker/files"]),
        ("POST", &["sessions/{sid}/kept-skills"]),
        ("POST", &["agent", "agent/main", "agent/update"]),
        ("GET", &["lsps/{lsp}/diagnostics"]),
        ("GET", &["agent/sessions/{sid}", "agent/sessions/{sid}/prompts/queued", "agent/sessions/{sid}/prompts/list"]),
        ("POST", &["agent/sessions/{sid}/cancel", "agent/sessions/{sid}/interrupt", "agent/sessions/{sid}/prompts/clear", "agent/sessions/{sid}/prompts/recall", "agent/sessions/{sid}/shell"]),
        ("POST", &["config/set", "config/remove", "config/model", "config/compact"]),
        ("GET", &["permissions/skip"]),
        ("POST", &["permissions/skip"]),
        ("POST", &["permissions/grant"]),
        ("GET", &["project/needs-init", "project/init-prompt"]),
        ("POST", &["project/init"]),
        ("POST", &["skills/read", "mcp/read-resource"]),
        ("GET", &["filetracker/lastread"]),
        ("POST", &["sessions/{sid}/branches"]),
        ("GET", &["chat-links"]),
        ("GET", &["sessions/{sid}/tree", "sessions/{sid}/tree/messages/{mid}"]),
        ("POST", &["sessions/{sid}/tree/label", "sessions/{sid}/tree/switch", "sessions/{sid}/tree/navigate", "sessions/{sid}/tree/copy",
            "sessions/{sid}/files/preview", "sessions/{sid}/files/undo-preview", "sessions/{sid}/files/undo"]),
    ];

    #[test]
    fn public_workspace_drops_env_and_config() {
        let ws = json!({"id": "ws-1", "path": "/p", "yolo": true, "data_dir": "/p/.crush", "env": ["OPENAI_API_KEY=sk-1"],
            "config": {"providers": {}}, "channels": ["a:b"], "skills": []});
        assert_eq!(
            public_workspace(ws),
            json!({"id": "ws-1", "path": "/p", "yolo": true, "data_dir": "/p/.crush", "channels": ["a:b"], "skills": []})
        );
        assert_eq!(public_workspace(json!(null)), json!(null));
        // What leaves the connection: the open result and the connected event.
        let j = joined("conn-public", TEST_TUNING);
        let create = j.fake.seen().into_iter().find(|s| s.line() == "POST /v1/workspaces").unwrap();
        assert!(create.json()["env"].as_array().is_some_and(|env| env.contains(&json!("P31_KEY=sk-from-login"))));
        let taken = j.conn.take();
        for ws in [&j.opened.workspace, &taken[0]["payload"]["workspace"]] {
            assert_eq!(ws["id"], "ws-1");
            assert!(ws.get("env").is_none() && ws.get("config").is_none(), "{ws}");
        }
        let text = format!("{}{}", serde_json::to_string(&taken).unwrap(), serde_json::to_string(&j.opened).unwrap());
        assert!(!text.contains("sk-from-login") && !text.contains("sk-from-config"));
    }

    #[test]
    fn call_with_id_has_no_limit() {
        let j = joined("conn-nolimit", Tuning { call_limit: Duration::from_millis(100), ..TEST_TUNING });
        j.fake.set_handler(engine_with(WS, |seen| {
            seen.path().ends_with("/shell").then(|| {
                Answer::After(Duration::from_millis(300), Box::new(Answer::Json(200, json!({"output": "done"}))))
            })
        }));
        let body = Some(json!({"command": "sleep 0.3"}));
        let e = run(j.conn.call("POST", "agent/sessions/s1/shell", None, body.clone(), None)).err().unwrap();
        assert_eq!(e.code, "timeout", "without a callId the ordinary limit holds");
        let answer = run(j.conn.call("POST", "agent/sessions/s1/shell", None, body, Some("c-1".into()))).unwrap();
        assert_eq!(answer, json!({"output": "done"}));
        assert!(!j.conn.abort_call("c-1"), "the entry went when the call ended");
    }

    #[test]
    fn abort_closes_the_socket() {
        let j = joined("conn-abort", TEST_TUNING);
        j.fake.set_handler(engine_with(WS, |seen| seen.path().ends_with("/shell").then_some(Answer::Hang)));
        let call = {
            let conn = j.conn.clone();
            tauri::async_runtime::spawn(async move {
                let body = Some(json!({"command": "sleep 100"}));
                conn.call("POST", "agent/sessions/s1/shell", None, body, Some("c-1".into())).await
            })
        };
        assert!(j.fake.wait_seen(Duration::from_secs(5), |seen| seen.iter().any(|s| s.path().ends_with("/shell"))));
        assert!(j.conn.abort_call("c-1"), "the running call is found");
        let e = run(call).unwrap().err().unwrap();
        assert_eq!((e.code.as_str(), e.message.as_str()), ("aborted", "The call was stopped."));
        assert!(j.fake.wait_closed(1, Duration::from_secs(5)), "the engine saw its connection close");
        assert!(!j.conn.abort_call("c-1"), "nothing left to stop");
        assert!(!j.conn.abort_call("never-sent"));
        assert_eq!(j.conn.inner().phase, Phase::Connected, "a stopped call is no lost engine");
    }

    #[test]
    fn post_with_call_id_sends_content_length() {
        let j = joined("conn-length", TEST_TUNING);
        j.fake.forget();
        let body = json!({"command": "echo héllo"});
        let mut query = Map::new();
        query.insert("request_id".into(), json!("r-1"));
        run(j.conn.call("POST", "agent/sessions/s1/shell", Some(query), Some(body.clone()), Some("c-1".into()))).unwrap();
        let seen = j.fake.seen();
        let shell = seen.iter().find(|s| s.path().ends_with("/shell")).unwrap();
        let length = serde_json::to_vec(&body).unwrap().len().to_string();
        assert_eq!(shell.header("content-length"), Some(length.as_str()));
        assert!(!shell.chunked && shell.header("transfer-encoding").is_none());
        assert_eq!(shell.json(), body);
        assert_eq!(shell.query("request_id").as_deref(), Some("r-1"));
        assert_eq!(shell.query("client_id").as_deref(), Some(j.opened.client_id.as_str()));
    }

    /// A fake crush that notes its arguments in `crush-args` and answers
    /// `engine version` and `engine ensure` with the fake engine's socket.
    fn fake_crush(dir: &Path, socket: &Path) -> (PathBuf, PathBuf) {
        let log = dir.join("crush-args");
        let ensured = json!({"address": format!("unix://{}", socket.display()), "socket": socket, "protocol": 1,
            "version": "v0.0.0-test", "pid": 4242, "data_dir": "/tmp/fake/.crush", "started": true});
        let body = format!(
            r#"printf '%s\n' "$*" >> '{log}'
case "$1 $2" in
  "engine version") echo '{{"version":"v0.0.0-test","protocol":1}}' ;;
  "engine ensure") echo '{ensured}' ;;
  *) echo "unknown command" >&2; exit 1 ;;
esac"#,
            log = log.display()
        );
        (script(&dir.join("crush"), &body), log)
    }

    /// Join step 1 with the fake crush; counts its runs.
    fn check_with(path: PathBuf, checks: Arc<AtomicUsize>) -> CrushCheck {
        Box::new(move |_env| {
            checks.fetch_add(1, SeqCst);
            let bin = CrushBin { path, source: BinSource::Setting };
            let version = engine_cli::version(&bin)?;
            Ok((bin, version))
        })
    }

    /// A window opened as the app opens one (`plan_for`, with the fake crush)
    /// on a new fake engine, in a project folder that exists.
    struct AppOpen {
        fake: FakeEngine,
        dir: PathBuf,
        project: String,
        crush_log: PathBuf,
        checks: Arc<AtomicUsize>,
        conn: Arc<WindowConn>,
        result: AppResult<OpenResult>,
    }

    /// `host`: start with `-H unix://<the fake engine's socket>`.
    fn open_as_the_app(name: &str, mut launch: Launch, host: bool) -> AppOpen {
        let fake = FakeEngine::start(&format!("{name}-engine"), engine(WS));
        let dir = scratch(name);
        std::fs::create_dir_all(dir.join("proj")).unwrap();
        let project = std::fs::canonicalize(dir.join("proj")).unwrap().to_string_lossy().into_owned();
        let (crush, crush_log) = fake_crush(&dir, &fake.socket);
        if host {
            launch.host = Some(format!("unix://{}", fake.socket.display()));
        }
        let checks = Arc::new(AtomicUsize::new(0));
        let (conn, _) = window(TEST_TUNING, launch.debug);
        let check = check_with(crush, checks.clone());
        let result = run(conn.open(plan_for(&launch, true, &project, env(), check)));
        AppOpen { fake, dir, project, crush_log, checks, conn, result }
    }

    impl AppOpen {
        /// Each line: the arguments of one crush run.
        fn crush_ran(&self) -> Vec<String> {
            let log = std::fs::read_to_string(&self.crush_log).unwrap_or_default();
            log.lines().map(String::from).collect()
        }

        fn create(&self) -> Value {
            let seen = self.fake.seen();
            seen.iter().find(|s| s.line() == "POST /v1/workspaces").map(Seen::json).unwrap_or_default()
        }
    }

    impl Drop for AppOpen {
        fn drop(&mut self) {
            run(self.conn.close());
            let _ = std::fs::remove_dir_all(&self.dir);
        }
    }

    #[test]
    fn join_order_and_create_body() {
        let o = open_as_the_app("conn-join", Launch::default(), false);
        let opened = o.result.clone().unwrap();
        assert_eq!(o.crush_ran(), ["engine version".to_string(), format!("engine ensure --cwd {} --timeout 15s", o.project)]);
        let seen = o.fake.seen();
        assert_eq!(
            lines(&seen),
            ["GET /v1/version", "POST /v1/workspaces", "GET /v1/workspaces/ws-1/events", "POST /v1/workspaces/ws-1/agent/init"]
        );
        assert_eq!(
            o.create(),
            json!({"path": o.project, "data_dir": "", "version": APP_VERSION, "protocol": 1, "client_id": opened.client_id,
                "yolo": true, "env": ["PATH=/usr/bin:/bin", "P31_KEY=sk-from-login"]})
        );
        for s in &seen[2..] {
            assert_eq!(s.query("client_id").as_deref(), Some(opened.client_id.as_str()));
        }
        assert_eq!(seen[3].json(), json!({"interactive": true}));
        assert_eq!((opened.cwd.as_str(), opened.sent_yolo), (o.project.as_str(), true));
        assert_eq!((opened.version.version.as_str(), opened.version.protocol), ("v0.0.0-test", 1));
        assert_eq!((opened.engine.socket.as_str(), opened.engine.pid), (o.fake.socket.to_str().unwrap(), 4242));
        assert_eq!(opened.workspace["path"], json!(o.project));
        assert!(!o.conn.debug, "no debug log without -d");
    }

    #[test]
    fn data_dir_goes_to_ensure_and_the_create() {
        let launch = Launch { data_dir: Some("/tmp/elsewhere/.crush".into()), ..Default::default() };
        let o = open_as_the_app("conn-datadir", launch, false);
        o.result.as_ref().unwrap();
        assert_eq!(
            o.crush_ran(),
            [
                "engine version".to_string(),
                format!("engine ensure --cwd {} --data-dir /tmp/elsewhere/.crush --timeout 15s", o.project)
            ]
        );
        assert_eq!(o.create()["data_dir"], "/tmp/elsewhere/.crush");
    }

    #[test]
    fn debug_goes_into_the_create() {
        let o = open_as_the_app("conn-debug", Launch { debug: true, ..Default::default() }, false);
        o.result.as_ref().unwrap();
        assert_eq!(o.create()["debug"], json!(true));
        assert!(o.conn.debug, "the app's debug log is on");
        let plain = open_as_the_app("conn-nodebug", Launch::default(), false);
        assert!(plain.create().get("debug").is_none());
    }

    #[test]
    fn a_host_join_never_runs_ensure() {
        assert_eq!(engine_plan(None).unwrap(), EnginePlan::Ensure);
        assert_eq!(engine_plan(Some("")).unwrap(), EnginePlan::Ensure);
        assert_eq!(
            engine_plan(Some("unix:///run/x.sock")).unwrap(),
            EnginePlan::Host { address: "unix:///run/x.sock".into(), socket: "/run/x.sock".into() }
        );
        for other in ["tcp://127.0.0.1:1234", "/run/x.sock", "unix://"] {
            assert_eq!(engine_plan(Some(other)).err().unwrap().code, "engine_unavailable", "{other}");
        }
        let o = open_as_the_app("conn-host", Launch::default(), true);
        let opened = o.result.clone().unwrap();
        assert_eq!(o.checks.load(SeqCst), 0, "crush was not looked for");
        assert!(o.crush_ran().is_empty(), "crush never ran: no version check, no ensure");
        assert_eq!(
            lines(&o.fake.seen()),
            ["GET /v1/version", "GET /v1/engine", "POST /v1/workspaces", "GET /v1/workspaces/ws-1/events", "POST /v1/workspaces/ws-1/agent/init"]
        );
        assert_eq!(opened.engine.address, format!("unix://{}", o.fake.socket.display()));
        assert_eq!((opened.engine.pid, opened.engine.data_dir.as_str()), (4343, "/tmp/fake/.crush"));
        assert_eq!(opened.version.version, "v0.0.0-test");
        // Losing it only re-dials.
        o.conn.take();
        o.fake.kill();
        o.fake.forget();
        o.fake.listen();
        let taken = take_until(&o.conn, "connected");
        assert_eq!(states(&taken).last().unwrap().0, "connected");
        assert!(o.crush_ran().is_empty(), "no ensure on the rejoin either");
        assert_eq!(lines(&o.fake.seen())[..3], ["GET /v1/version", "GET /v1/engine", "POST /v1/workspaces"]);
        // Without -H the app always runs ensure.
        let plain = open_as_the_app("conn-nohost", Launch::default(), false);
        plain.result.as_ref().unwrap();
        assert_eq!(plain.checks.load(SeqCst), 1);
        assert!(plain.crush_ran().iter().any(|run| run.starts_with("engine ensure ")));
    }

    #[test]
    fn channels_go_into_the_create() {
        let launch = Launch { channels: vec!["slack:main".into(), "discord:dev".into()], ..Default::default() };
        let o = open_as_the_app("conn-channels", launch, false);
        o.result.as_ref().unwrap();
        assert_eq!(o.create()["channels"], json!(["slack:main", "discord:dev"]));
        let plain = open_as_the_app("conn-nochannels", Launch::default(), false);
        assert!(plain.create().get("channels").is_none());
    }

    #[test]
    fn yolo_comes_from_the_flag_else_the_setting() {
        let o = open_as_the_app("conn-yolo", Launch { yolo: Some(false), ..Default::default() }, false);
        assert!(!o.result.as_ref().unwrap().sent_yolo);
        assert_eq!(o.create()["yolo"], json!(false));
    }

    #[test]
    fn a_missing_folder_fails_before_anything_starts() {
        let dir = scratch("conn-cwd");
        let missing = dir.join("missing");
        let file = dir.join("file");
        std::fs::write(&file, "x").unwrap();
        let checks = Arc::new(AtomicUsize::new(0));
        for (cwd, reason) in [(&missing, "no such file or directory"), (&file, "not a directory")] {
            let (conn, rings) = window(TEST_TUNING, false);
            let check = check_with(dir.join("crush"), checks.clone());
            let cwd = cwd.to_str().unwrap();
            let e = run(conn.open(plan_for(&Launch::default(), true, cwd, env(), check))).err().unwrap();
            assert_eq!(e.code, "bad_path");
            assert_eq!(e.message, format!("failed to change directory: chdir {cwd}: {reason}"));
            assert_eq!(rings.load(SeqCst), 0, "no connection event");
        }
        assert_eq!(checks.load(SeqCst), 0, "crush was not even looked for, so no engine started");
        std::fs::remove_dir_all(&dir).unwrap();
    }

    #[test]
    fn a_second_open_answers_the_same() {
        let j = joined("conn-twice", TEST_TUNING);
        j.fake.forget();
        let again = run(j.conn.open(std::future::ready(Err(AppError::new("internal", "planned again"))))).unwrap();
        assert_eq!(again.client_id, j.opened.client_id);
        assert!(j.fake.seen().is_empty(), "nothing joined again");
        assert_eq!(j.finder.runs().len(), 1);
    }

    #[test]
    fn a_closing_engine_gets_one_more_create() {
        for (name, refusals, works) in [("conn-closing", 1, true), ("conn-closed", 2, false)] {
            let creates = Arc::new(AtomicUsize::new(0));
            let counted = creates.clone();
            let fake = FakeEngine::start(name, engine_with(WS, move |seen| {
                let closing = seen.line() == "POST /v1/workspaces" && counted.fetch_add(1, SeqCst) < refusals;
                closing.then(|| Answer::Json(503, json!({"message": "server is shutting down"})))
            }));
            let finder = FakeFinder::new(&fake);
            let (conn, _) = window(TEST_TUNING, false);
            let result = run(conn.open(std::future::ready(Ok(plan(finder.clone())))));
            assert_eq!(finder.runs().len(), 2, "{name}: ensure ran again");
            assert_eq!(creates.load(SeqCst), 2, "{name}: one more create");
            match result {
                Ok(_) => assert!(works),
                Err(e) => assert_eq!((works, e.code.as_str()), (false, "shutting_down")),
            }
            run(conn.close());
        }
    }

    #[test]
    fn a_failed_open_gives_its_claim_back() {
        let fake = FakeEngine::start("conn-claim", engine_with(WS, |seen| {
            seen.path().ends_with("/events").then(|| Answer::Json(404, json!({"message": "workspace not found"})))
        }));
        let finder = FakeFinder::new(&fake);
        let (conn, rings) = window(TEST_TUNING, false);
        let first = plan(finder.clone());
        let first_id = first.client_id.clone();
        let e = run(conn.open(std::future::ready(Ok(first)))).err().unwrap();
        assert_eq!(e.code, "not_found");
        assert_eq!(lines(&fake.seen()).last().unwrap(), &format!("DELETE /v1/clients/{first_id}"));
        assert_eq!(rings.load(SeqCst), 0, "no connected event");
        // Try again: a new plan, so a new client id.
        fake.set_handler(engine(WS));
        let opened = run(conn.open(std::future::ready(Ok(plan(finder.clone()))))).unwrap();
        assert_ne!(opened.client_id, first_id);
        run(conn.close());
    }

    #[test]
    fn agent_init_failure_still_opens() {
        let fake = FakeEngine::start("conn-init", engine_with(WS, |seen| {
            seen.path().ends_with("/agent/init").then(|| Answer::Json(500, json!({"message": "no model configured"})))
        }));
        let (conn, _) = window(TEST_TUNING, false);
        let opened = run(conn.open(std::future::ready(Ok(plan(FakeFinder::new(&fake)))))).unwrap();
        assert_eq!(opened.workspace["id"], "ws-1");
        assert_eq!(states(&conn.take()), pairs(&[("connected", 0)]));
        run(conn.close());
    }

    #[test]
    fn close_aborts_calls_retires_the_client_and_stops() {
        let j = joined("conn-close", TEST_TUNING);
        j.fake.set_handler(engine_with(WS, |seen| seen.path().ends_with("/shell").then_some(Answer::Hang)));
        let call = {
            let conn = j.conn.clone();
            tauri::async_runtime::spawn(async move {
                conn.call("POST", "agent/sessions/s1/shell", None, Some(json!({"command": "sleep 100"})), Some("c-1".into())).await
            })
        };
        assert!(j.fake.wait_seen(Duration::from_secs(5), |seen| seen.iter().any(|s| s.path().ends_with("/shell"))));
        j.conn.take();
        run(j.conn.close());
        let e = run(call).unwrap().err().unwrap();
        assert_eq!(e.code, "aborted", "the window's running calls end with it");
        assert!(j.fake.wait_closed(1, Duration::from_secs(5)));
        let retired: Vec<String> = j.fake.seen().iter().filter(|s| s.method == "DELETE").map(Seen::line).collect();
        assert_eq!(retired, [format!("DELETE /v1/clients/{}", j.opened.client_id)]);
        // Nothing runs on: no reconnect when the engine goes, and no calls.
        let ensured = j.finder.runs().len();
        j.fake.end_streams();
        std::thread::sleep(Duration::from_millis(400));
        assert_eq!(j.finder.runs().len(), ensured, "no reconnect after close");
        assert!(j.conn.take().is_empty());
        assert_eq!(run(j.conn.call("GET", "sessions", None, None, None)).err().unwrap().code, "engine_unavailable");
        assert!(run(j.conn.open(std::future::ready(Ok(plan(j.finder.clone()))))).is_err(), "a closed window stays closed");
    }
}
