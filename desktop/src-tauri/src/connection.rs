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
use crate::{fixture, login_env, windows};
use serde_json::{json, Map, Value};
use std::collections::HashMap;
use std::future::Future;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex, MutexGuard};
use std::time::{Duration, Instant};
use crate::host::async_runtime::JoinHandle;

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
    crate::host::async_runtime::spawn_blocking(work)
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
    ("POST", "sessions/{sid}/model"),
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
    // Phases 5 and 6 (desktop-design-5-6 6.5; the G-n routes are G56's).
    ("GET", "commands"),
    ("GET", "mcp/prompts"),
    ("POST", "mcp/get-prompt"),
    ("POST", "agent/sessions/{sid}/summarize"),
    ("GET", "agent/capabilities"),
    ("POST", "config/refresh-oauth"),
    ("POST", "config/import-copilot"),
    ("POST", "agent/init"),
    ("POST", "/v1/providers/refresh"),
    ("POST", "auth/flows"),
    ("POST", "auth/flows/{flow_id}/code"),
    ("POST", "auth/flows/{flow_id}/cancel"),
    ("GET", "saved-sessions"),
    ("POST", "saved-sessions"),
    ("POST", "questions/answer"),
    ("POST", "questions/cancel"),
    ("GET", "questions/pending"),
    ("GET", "secure-forms"),
    ("POST", "secure-forms/{fid}/cancel"),
    ("GET", "skills/installed"),
    ("POST", "skills/manage"),
    ("GET", "skills/search"),
    ("GET", "mcp/disabled"),
    ("POST", "mcp/disabled"),
    ("GET", "mcp/enabled"),
    ("POST", "mcp/config-disabled"),
    ("GET", "mcp/pending-auth"),
    ("GET", "mcp/auth-url"),
    ("POST", "mcp/auth"),
    ("POST", "mcp/docker/enable"),
    ("POST", "mcp/docker/disable"),
    ("GET", "/v1/memory"),
    ("PUT", "/v1/memory/limit"),
    ("POST", "/v1/memory/claim-notice"),
    ("POST", "/v1/cpu-affinity"),
    ("GET", "/v1/themes"),
    ("GET", "/v1/themes/{name}"),
    ("POST", "/v1/themes/resolve"),
    ("PUT", "/v1/themes/{name}"),
    ("POST", "/v1/themes"),
    ("POST", "/v1/themes/{name}/rename"),
    ("DELETE", "/v1/themes/{name}"),
    ("POST", "/v1/themes/{name}/revert"),
    ("GET", "sessions/{sid}/helpers"),
    ("POST", "sessions/{sid}/helpers/{hid}/stop"),
    ("GET", "sessions/{sid}/jobs"),
    ("POST", "sessions/{sid}/jobs/kill"),
    ("POST", "sessions/{sid}/background"),
    ("GET", "servers"),
    ("POST", "remote/start"),
    ("POST", "remote/stop"),
    ("GET", "remote"),
    ("POST", "remote/presence"),
    ("POST", "remote/actions/{aid}/done"),
    ("GET", "/v1/updates/clis"),
    ("POST", "/v1/updates/clis/decline"),
    ("POST", "/v1/updates/clis/install"),
    ("GET", "/v1/updates/crush"),
    ("POST", "/v1/updates/crush/decline"),
    // Accounts and the fallback order (internal/rpc/routes_fallback.go).
    ("GET", "fallback"),
    ("PUT", "fallback/order"),
    ("POST", "accounts"),
    ("PUT", "accounts/{aid}"),
    ("DELETE", "accounts/{aid}"),
    ("POST", "accounts/{aid}/login"),
    ("POST", "/v1/limits/refill"),
    // Another folder's open chat, read from its folder's saved files so it
    // stays drawn without that folder's engine (owner, 2026-10-22).
    ("GET", "saved-sessions/{sid}"),
    ("GET", "saved-sessions/{sid}/messages"),
    // OpenRouter's catalog for the model menu's "+" (internal/rpc/routes_openrouter.go).
    ("GET", "openrouter/catalog"),
];

/// The only routes `api_secret` posts to (6.4): a secure form's answers,
/// a provider key, and G-5's key check. `api` never reaches them, so a
/// secret never travels through it.
const SECRET_ROUTES: &[&str] = &["secure-forms/{fid}/submit", "config/provider-key", "auth/verify-key"];

/// A path the route list allows, split into its parts.
#[derive(Debug)]
struct Route {
    absolute: bool,
    parts: Vec<String>,
}

impl Route {
    fn find(method: &str, path: &str) -> AppResult<Route> {
        let refused = || AppError::new("route_not_allowed", format!("Crush Desktop does not call {method} {path}."));
        Route::matching(method, path, ROUTES).ok_or_else(refused)
    }

    /// `api_secret`'s routes: POST, relative, and only these (6.4).
    fn secret(path: &str) -> AppResult<Route> {
        let patterns: Vec<(&str, &str)> = SECRET_ROUTES.iter().map(|p| ("POST", *p)).collect();
        Route::matching("POST", path, &patterns)
            .ok_or_else(|| AppError::new("secret_route", format!("Crush Desktop sends no secret to {path}.")))
    }

    fn matching(method: &str, path: &str, routes: &[(&str, &str)]) -> Option<Route> {
        let (absolute, rest) = match path.strip_prefix('/') {
            Some(rest) => (true, rest),
            None => (false, path),
        };
        let parts: Vec<&str> = rest.split('/').collect();
        if parts.iter().any(|p| p.is_empty() || *p == "." || *p == "..") {
            return None;
        }
        let listed = routes.iter().any(|(m, pattern)| {
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
        listed.then(|| Route {
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

/// One folder the window keeps joined. Its claim on the engine is its event
/// stream, which the engine gives up 10 s after the last one closes
/// (internal/backend/clients.go:336-377), so the stream stays open for the
/// folder's whole life; only the shown folder's events reach the page.
struct Folder {
    link: Link,
    plan: Arc<Plan>,
    client_id: String,
    opened: OpenResult,
    /// The identity of its stream, so the end of an older one starts nothing.
    generation: u64,
    reader: Option<Owned>,
}

/// A folder the window opened besides the one it joined (`engine_open_folder`).
#[derive(Clone, serde::Serialize)]
pub struct OpenedFolder {
    pub dir: String,
    pub client_id: String,
    pub workspace: Value,
    pub engine: engine_cli::Ensured,
}

/// A folder's path written one way, as the page writes it: no trailing slash
/// ("/" stays itself).
fn folder_key(dir: &str) -> String {
    if dir == "/" {
        return "/".to_string();
    }
    dir.trim_end_matches('/').to_string()
}

struct Inner {
    phase: Phase,
    /// The shown folder's plan, from its join.
    plan: Option<Arc<Plan>>,
    /// The folder the page shows: its stream feeds the batch untagged.
    shown: Option<String>,
    /// Every joined folder, the shown one included.
    folders: HashMap<String, Folder>,
    opened: Option<OpenResult>,
    /// The chat the window last reported (`""` is the landing page); sent again after a rejoin.
    current_session: Option<String>,
    counter: u64,
    /// The join under way, and whether its event stream already ended.
    joining: Option<u64>,
    joining_lost: bool,
    reconnect: Option<JoinHandle<()>>,
}

struct Queue {
    batcher: Batcher,
    /// Events of other folders' engines (watch.rs), by folder: each listed
    /// chat stays drawn and current in the window (owner, 2026-10-22).
    side: HashMap<String, Side>,
    /// A doorbell went out and no take has happened since.
    rung: bool,
    /// Events pushed since the last take; tests wait on it.
    #[cfg(test)]
    pushed: usize,
}

/// One other folder's waiting events; `resync` asks the page to read that
/// folder's chats again (its stream just started, or events overflowed).
struct Side {
    batcher: Batcher,
    resync: bool,
}

/// An entry of another folder's batch, tagged with the folder.
fn side_entry(dir: &str, entry: batch::Entry) -> Value {
    let mut value = fixture::wire_entry(entry);
    if let Some(object) = value.as_object_mut() {
        object.insert("folder".to_string(), json!(dir));
    }
    value
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
                side: HashMap::new(),
                rung: false,
                #[cfg(test)]
                pushed: 0,
            }),
            inner: Mutex::new(Inner {
                phase: Phase::Idle,
                plan: None,
                shown: None,
                folders: HashMap::new(),
                opened: None,
                current_session: None,
                counter: 0,
                joining: None,
                joining_lost: false,
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
        let line = format!("crush-desktop: [{}] {what}", self.label);
        #[cfg(test)]
        tests::LOGGED.lock().unwrap().push(line.clone());
        eprintln!("{line}");
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
        let mut out: Vec<Value> = queue.batcher.take().into_iter().map(fixture::wire_entry).collect();
        for (dir, mut side) in queue.side.drain() {
            if side.resync {
                out.push(side_entry(&dir, batch::Entry::Resync));
            }
            out.extend(side.batcher.take().into_iter().map(|entry| side_entry(&dir, entry)));
        }
        out
    }

    /// The folder this window shows (resolved), once it has one.
    fn folder(&self) -> Option<String> {
        self.inner().shown.clone()
    }

    /// Whether `dir` is the folder the window shows, so its events reach the
    /// page untagged (the others stay live for their chats through watch.rs).
    fn is_shown(&self, dir: &str) -> bool {
        self.inner().shown.as_deref() == Some(dir)
    }

    /// Another folder's events (or, with none, a resync of that folder)
    /// into this window's batch; the folder the window joined hears them
    /// through its own stream.
    fn push_side(&self, dir: &str, events: Vec<Event>, resync: bool) {
        if self.folder().as_deref() == Some(dir) || (events.is_empty() && !resync) {
            return;
        }
        let due = {
            let mut queue = self.queue.lock().unwrap();
            let side = queue.side.entry(dir.to_string()).or_insert_with(|| Side { batcher: Batcher::new(BATCH_MAX), resync: false });
            side.resync |= resync;
            for ev in events {
                side.batcher.push(ev);
            }
            !std::mem::replace(&mut queue.rung, true)
        };
        self.ring_if(due);
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

    /// Opens a folder beside the one the window shows (`engine_open_folder`):
    /// the window keeps it joined, so its chats answer at once without the
    /// folder being shown. A folder already joined answers as it is.
    async fn open_folder(self: &Arc<Self>, plan: impl Future<Output = AppResult<Plan>>) -> AppResult<OpenedFolder> {
        let _one_at_a_time = self.opening.lock().await;
        if self.inner().phase == Phase::Closed {
            return Err(closed());
        }
        let plan = match plan.await {
            Ok(plan) => Arc::new(plan),
            Err(e) => {
                self.log(&format!("could not start: {} ({})", e.message, e.code));
                return Err(e);
            }
        };
        let dir = folder_key(&plan.cwd);
        if let Some(folder) = self.inner().folders.get(&dir) {
            return Ok(OpenedFolder {
                dir: dir.clone(),
                client_id: folder.client_id.clone(),
                workspace: folder.opened.workspace.clone(),
                engine: folder.opened.engine.clone(),
            });
        }
        match self.join_as(&plan, 0, false).await {
            Ok(opened) => Ok(OpenedFolder {
                dir,
                client_id: opened.client_id,
                workspace: opened.workspace,
                engine: opened.engine,
            }),
            Err((e, holder)) => {
                if let Some(http) = holder {
                    // That engine holds a claim for this client: give it back.
                    self.retire(&http, &plan.client_id).await;
                }
                self.log(&format!("could not keep {dir} joined: {} ({})", e.message, e.code));
                Err(e)
            }
        }
    }

    /// Shows `dir` (`engine_show_folder`): opens it first when the window does
    /// not keep it joined, then makes it the folder the page reads. Nothing
    /// leaves: the folder it showed before stays joined (owner, 2026-10-22).
    async fn show(self: &Arc<Self>, dir: &str, plan: impl Future<Output = AppResult<Plan>>) -> AppResult<OpenResult> {
        let dir = folder_key(dir);
        if self.inner().shown.as_deref() == Some(dir.as_str()) {
            if let Some(opened) = self.inner().opened.clone() {
                return Ok(opened);
            }
        }
        if !self.inner().folders.contains_key(&dir) {
            self.open_folder(plan).await?;
        }
        let (opened, from, link, client_id) = {
            let mut inner = self.inner();
            if inner.phase == Phase::Closed {
                return Err(closed());
            }
            let Some(folder) = inner.folders.get(&dir) else {
                return Err(AppError::new(
                    "engine_unavailable",
                    format!("Crush Desktop has not joined {dir}."),
                ));
            };
            let parts = (folder.opened.clone(), folder.link.clone(), folder.client_id.clone());
            inner.plan = Some(folder.plan.clone());
            let from = inner.shown.replace(dir.clone());
            inner.current_session = None;
            inner.opened = Some(parts.0.clone());
            (parts.0, from, parts.1, parts.2)
        };
        let base = format!("/v1/workspaces/{}", encode(&link.workspace_id));
        let client = format!("client_id={}", encode(&client_id));
        // The folder it left reports no chat, so no phone of a share there
        // thinks one of its chats is on this window's screen.
        let left = from.filter(|old| old != &dir).and_then(|old| {
            self.inner()
                .folders
                .get(&old)
                .map(|folder| (folder.link.clone(), folder.client_id.clone()))
        });
        if let Some((old_link, old_client)) = left {
            let old_base = format!("/v1/workspaces/{}", encode(&old_link.workspace_id));
            let old_client = format!("client_id={}", encode(&old_client));
            self.send_current_session(&old_link.http, &old_base, &old_client, "").await;
        }
        // A folder the window only kept joined runs its agent now (it needs
        // one for the chats the page will send here).
        let init = json!({"interactive": true});
        if let Err(e) = link.http.call("POST", &format!("{base}/agent/init?{client}"), Some(&init), Some(self.tuning.call_limit)).await {
            self.log(&format!("agent init failed: {} ({})", e.message, e.code));
        }
        let due = self.enqueue(vec![connection_event(json!({
            "state": "connected",
            "attempt": 0,
            "resync": true,
            "fresh_engine": false,
            "workspace": opened.workspace,
            "engine": opened.engine,
        }))]);
        self.ring_if(due);
        self.log(&format!("showing {dir} (workspace {})", opened.workspace["id"]));
        Ok(opened)
    }

    /// Lets a folder go (`engine_leave_folder`): the engine keeps its running
    /// work going (keep_work), and the page drops it. The shown folder stays
    /// until another one is shown.
    async fn leave_folder(self: &Arc<Self>, dir: &str) -> AppResult<()> {
        let dir = folder_key(dir);
        let folder = {
            let mut inner = self.inner();
            if inner.shown.as_deref() == Some(dir.as_str()) {
                return Err(AppError::new(
                    "folder_shown",
                    "Crush Desktop shows this folder; show another one first.",
                ));
            }
            inner.folders.remove(&dir)
        };
        if let Some(folder) = folder {
            self.log(&format!("letting {dir} go"));
            self.retire_keeping(&folder.link.http, &folder.client_id, true).await;
            drop(folder.reader);
        }
        Ok(())
    }

    /// engine, its version, the workspace, the event stream, agent init and
    /// the current chat. On success the window is connected and its
    /// `connected` event is in the batch. A failure after the create also
    /// answers the engine that holds this client's claim.
    async fn join(self: &Arc<Self>, plan: &Arc<Plan>, attempt: u32) -> Result<OpenResult, (AppError, Option<Arc<EngineHttp>>)> {
        self.join_as(plan, attempt, true).await
    }

    /// join, for the folder the window shows (`shown` true) or for one it
    /// keeps joined beside it: that one runs no agent init and reports no
    /// chat, and its join reaches the page as no connection event.
    async fn join_as(
        self: &Arc<Self>,
        plan: &Arc<Plan>,
        attempt: u32,
        shown: bool,
    ) -> Result<OpenResult, (AppError, Option<Arc<EngineHttp>>)> {
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

        let generation = self.begin_join(shown);
        let stream = match http.stream(&format!("{base}/events?{client}")).await {
            Ok(stream) => stream,
            Err(e) => {
                self.end_join(generation);
                return Err(held(e));
            }
        };
        let reader = Owned(Some(self.start_reader(stream, folder_key(&plan.cwd), generation)));

        // Agent init: a failure is logged, not fatal, as in the terminal
        // (internal/cmd/workspace_setup.go:204-208): a project with no model
        // yet still opens, for onboarding. A folder the window only keeps
        // joined runs no work, so it needs none.
        let mut reported = None;
        if shown {
            let init = json!({"interactive": true});
            if let Err(e) = http.call("POST", &format!("{base}/agent/init?{client}"), Some(&init), limit).await {
                self.log(&format!("agent init failed: {} ({})", e.message, e.code));
            }
            reported = self.inner().current_session.clone();
            if let Some(session_id) = &reported {
                self.send_current_session(&http, &base, &client, session_id).await;
            }
        }

        let opened = OpenResult {
            client_id: plan.client_id.clone(),
            workspace: public_workspace(workspace),
            engine,
            version: plan.version.clone().unwrap_or(version),
            sent_yolo: plan.sent_yolo,
            cwd: plan.cwd.clone(),
        };
        let folder = Folder {
            link: Link {
                http: http.clone(),
                workspace_id,
            },
            plan: plan.clone(),
            client_id: plan.client_id.clone(),
            opened: opened.clone(),
            generation,
            reader: None,
        };
        if !shown {
            // The page learns of it from the command's answer, not an event.
            self.install_folder(folder, reader).await.map_err(|e| (e, None))?;
            return Ok(opened);
        }
        let opened = self.install(generation, reader, folder, opened, attempt).await.map_err(held)?;
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

    fn begin_join(&self, shown: bool) -> u64 {
        let mut inner = self.inner();
        inner.counter += 1;
        let generation = inner.counter;
        if shown {
            inner.joining = Some(generation);
            inner.joining_lost = false;
        }
        generation
    }

    fn end_join(&self, generation: u64) {
        let mut inner = self.inner();
        if inner.joining == Some(generation) {
            inner.joining = None;
            inner.joining_lost = false;
        }
    }

    /// The last join step for the folder the window shows: the window is
    /// connected, and its `connected` event goes into the batch under the same
    /// lock as the state change, so it keeps its order with the reconnect's
    /// events.
    async fn install(&self, generation: u64, reader: Owned, mut folder: Folder, opened: OpenResult, attempt: u32) -> AppResult<OpenResult> {
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
            let key = folder_key(&opened.cwd);
            let fresh_engine = attempt > 0 && inner.folders.get(&key).is_some_and(|old| old.link.workspace_id != folder.link.workspace_id);
            folder.reader = Some(reader);
            inner.folders.insert(key.clone(), folder);
            inner.shown = Some(key);
            inner.phase = Phase::Connected;
            inner.reconnect = None;
            inner.opened = Some(opened.clone());
            let due = self.enqueue(vec![connection_event(json!({
                "state": "connected",
                "attempt": attempt,
                "resync": attempt > 0,
                "fresh_engine": fresh_engine,
                "workspace": opened.workspace,
                "engine": opened.engine,
            }))]);
            due
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

    /// The last join step for a folder the window keeps beside the one it
    /// shows: it joins quietly, reaching the page as no connection event.
    async fn install_folder(&self, mut folder: Folder, reader: Owned) -> AppResult<OpenedFolder> {
        let dir = folder_key(&folder.opened.cwd);
        let opened = OpenedFolder {
            dir: dir.clone(),
            client_id: folder.client_id.clone(),
            workspace: folder.opened.workspace.clone(),
            engine: folder.opened.engine.clone(),
        };
        folder.reader = Some(reader);
        {
            let mut inner = self.inner();
            if inner.phase == Phase::Closed {
                return Err(closed());
            }
            inner.folders.insert(dir.clone(), folder);
        };
        self.log(&format!("kept {dir} joined (workspace {})", opened.workspace["id"]));
        Ok(opened)
    }

    /// The folders the window keeps joined, in order.
    #[cfg_attr(not(test), allow(dead_code))] // the tests read it
    fn folders_joined(&self) -> Vec<String> {
        let mut dirs: Vec<String> = self.inner().folders.keys().cloned().collect();
        dirs.sort();
        dirs
    }

    /// Join step 5's reader: the event stream's lines. The shown folder's go
    /// into the batch; another folder's stay live for their chats through
    /// watch.rs's files and only keep its claim on the engine. Its end starts
    /// a reconnect (1.2.5 step 4), or a quieter revive for another folder.
    fn start_reader(self: &Arc<Self>, mut stream: ByteStream, dir: String, generation: u64) -> JoinHandle<()> {
        let me = self.clone();
        crate::host::async_runtime::spawn(async move {
            let mut lines = LineSplitter::new();
            let mut out = Vec::new();
            while let Some(Ok(chunk)) = stream.next_chunk().await {
                lines.push(&chunk, &mut out);
                let events: Vec<Event> = out.drain(..).filter_map(|line| batch::parse_sse_line(&line)).collect();
                if me.is_shown(&dir) {
                    me.push_all(events);
                }
            }
            drop(stream);
            me.lost(&dir, generation, "the event stream ended");
        })
    }

    /// Gives a client's claim back (a failed open, or a closing window).
    async fn retire(&self, http: &EngineHttp, client_id: &str) {
        self.retire_keeping(http, client_id, false).await;
    }

    /// keep_work: the engine keeps this folder's running work going after
    /// this client leaves it (the window moves to another folder; owner,
    /// 2026-10-21). The engine closes the folder once that work ends or no
    /// app is open.
    async fn retire_keeping(&self, http: &EngineHttp, client_id: &str, keep_work: bool) {
        let keep = if keep_work { "?keep_work=1" } else { "" };
        let path = format!("/v1/clients/{}{keep}", encode(client_id));
        if let Err(e) = http.call("DELETE", &path, None, Some(self.tuning.close_limit)).await {
            self.log(&format!("could not leave the engine: {} ({})", e.message, e.code));
        }
    }

    // Losing the engine (1.2.6).

    /// Starts one reconnect; a second trigger while one runs, or a trigger
    /// from an older stream, is ignored.
    fn lost(self: &Arc<Self>, dir: &str, generation: u64, why: &str) {
        let (shown, current, joining) = {
            let inner = self.inner();
            (
                inner.shown.as_deref() == Some(dir),
                inner.folders.get(dir).map(|folder| folder.generation),
                inner.joining,
            )
        };
        if shown && joining == Some(generation) {
            self.inner().joining_lost = true;
            return;
        }
        if current != Some(generation) {
            return;
        }
        if shown {
            let mut inner = self.inner();
            if inner.phase != Phase::Connected {
                return;
            }
            self.log(&format!("lost the engine ({why}); reconnecting"));
            if let Some(reader) = inner.folders.get_mut(dir).and_then(|folder| folder.reader.take()) {
                drop(reader);
            }
            self.start_reconnect(&mut inner);
            return;
        }
        // A folder the window keeps beside the shown one: take its stream up
        // again, since that stream is what holds its claim on the engine.
        self.log(&format!("{dir}'s stream ended ({why}); taking it up again"));
        self.revive_stream(dir.to_string());
    }

    /// Opens a folder's event stream again, quietly, with the same backoff the
    /// reconnect uses; the folder keeps its client id and its workspace.
    fn revive_stream(self: &Arc<Self>, dir: String) {
        let me = self.clone();
        crate::host::async_runtime::spawn(async move {
            let mut backoff = Backoff::new(me.tuning.first_wait, me.tuning.max_wait);
            for _ in 0..me.tuning.max_failures {
                tokio::time::sleep(backoff.next()).await;
                let Some((link, client_id, generation)) = me.stream_next(&dir) else { return };
                let base = format!("/v1/workspaces/{}", encode(&link.workspace_id));
                let client = format!("client_id={}", encode(&client_id));
                let stream = match link.http.stream(&format!("{base}/events?{client}")).await {
                    Ok(stream) => stream,
                    Err(e) => {
                        me.log(&format!("could not open {dir}'s stream again: {} ({})", e.message, e.code));
                        continue;
                    }
                };
                let reader = Owned(Some(me.start_reader(stream, dir.clone(), generation)));
                me.keep_reader(&dir, generation, reader);
                return;
            }
            me.log(&format!("{dir}'s stream stays closed"));
        });
    }

    /// The link of a joined folder with a new stream generation, or None once
    /// it has left the window.
    fn stream_next(&self, dir: &str) -> Option<(Link, String, u64)> {
        let mut inner = self.inner();
        inner.counter += 1;
        let generation = inner.counter;
        let folder = inner.folders.get_mut(dir)?;
        folder.generation = generation;
        Some((folder.link.clone(), folder.client_id.clone(), generation))
    }

    fn keep_reader(&self, dir: &str, generation: u64, reader: Owned) {
        let mut inner = self.inner();
        if let Some(folder) = inner.folders.get_mut(dir).filter(|folder| folder.generation == generation) {
            let old = folder.reader.replace(reader);
            drop(old);
        }
    }

    fn start_reconnect(self: &Arc<Self>, inner: &mut Inner) {
        inner.phase = Phase::Reconnecting;
        let me = self.clone();
        inner.reconnect = Some(crate::host::async_runtime::spawn(async move { me.reconnect().await }));
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
                // A claim this try made is kept, not retired: the next try
                // re-arms it under the same client id, the engine drops it after
                // its create grace otherwise, and a retired id could never rejoin
                // this engine (internal/backend/clients.go:70-99). The terminal
                // too retires only when it exits (internal/workspace/client_events.go:319-324).
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

    /// The link for `dir` (the shown folder with none): its client id, its
    /// stream's generation and its key. `engine_unavailable` at once
    /// otherwise, so nothing is sent.
    fn ready_at(&self, dir: Option<&str>) -> AppResult<(Link, String, u64, String)> {
        let inner = self.inner();
        if inner.phase == Phase::Closed || inner.phase == Phase::Idle {
            return Err(not_open());
        }
        if inner.phase == Phase::Reconnecting {
            return Err(AppError::new(
                "engine_unavailable",
                "Crush Desktop is reconnecting to the Crush engine; nothing was sent.",
            ));
        }
        if inner.phase == Phase::Failed {
            return Err(AppError::new(
                "engine_unavailable",
                "Crush Desktop lost its connection to the Crush engine; nothing was sent.",
            ));
        }
        let key = match dir.map(folder_key) {
            Some(key) if !key.is_empty() => key,
            _ => inner.shown.clone().ok_or_else(not_open)?,
        };
        let folder = inner.folders.get(&key).ok_or_else(|| {
            AppError::new(
                "engine_unavailable",
                format!("Crush Desktop has not joined {key} yet; nothing was sent."),
            )
        })?;
        Ok((folder.link.clone(), folder.client_id.clone(), folder.generation, key))
    }

    #[cfg_attr(not(test), allow(dead_code))] // the tests' calls go through it
    async fn call(
        self: &Arc<Self>,
        method: &str,
        path: &str,
        query: Option<Map<String, Value>>,
        body: Option<Value>,
        call_id: Option<String>,
    ) -> AppResult<Value> {
        self.call_in(None, method, path, query, body, call_id).await
    }

    /// call, in `dir`'s folder (the shown folder with None): a chat of another
    /// joined folder reaches its own folder's engine (owner, 2026-10-22).
    async fn call_in(
        self: &Arc<Self>,
        dir: Option<&str>,
        method: &str,
        path: &str,
        query: Option<Map<String, Value>>,
        body: Option<Value>,
        call_id: Option<String>,
    ) -> AppResult<Value> {
        let method = method.to_ascii_uppercase();
        let route = Route::find(&method, path)?;
        let (link, client_id, generation, dir) = self.ready_at(dir)?;
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
                    self.lost(&dir, generation, &e.code);
                }
            }
        }
        result.map(|reply| reply.body)
    }

    /// `api_secret` (6.4): a POST to one of the secret routes with `body`, the
    /// JSON bytes, sent with its `Content-Length` through the same
    /// `EngineHttp::call`. The bytes and the copy made for the call are
    /// overwritten with zeros when it ends, either way. Nothing of the body
    /// reaches a log line or an error: an engine error keeps the engine's
    /// code and message only.
    async fn call_secret(self: &Arc<Self>, path: &str, body: &mut [u8]) -> AppResult<Value> {
        let result = self.send_secret(path, body).await;
        wipe(body);
        result
    }

    async fn send_secret(self: &Arc<Self>, path: &str, body: &[u8]) -> AppResult<Value> {
        let route = Route::secret(path)?;
        let (link, client_id, generation, dir) = self.ready_at(None)?;
        let target = route.target(&link.workspace_id, None, &client_id);
        let mut value: Value = serde_json::from_slice(body)
            .map_err(|_| AppError::new("bad_request", "The secret's body is not JSON."))?;
        let started = Instant::now();
        let result = link.http.call("POST", &target, Some(&value), Some(self.tuning.call_limit)).await;
        wipe_value(&mut value);
        let took = started.elapsed().as_millis();
        match &result {
            Ok(reply) => self.debug_log(&format!("POST {path}: {} in {took} ms", reply.status)),
            Err(e) => {
                self.debug_log(&format!("POST {path}: {} in {took} ms", e.code));
                if lost_engine(e) {
                    self.lost(&dir, generation, &e.code);
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
            let task = crate::host::async_runtime::spawn(async move { http.call(&method, &target, body.as_ref(), None).await });
            let abort = task.abort_handle();
            if calls.insert(id.clone(), Running { serial, abort }).is_some() {
                self.log("a call took the id of a call still running; abort now stops the new one");
            }
            task
        };
        let _listed = Listed {
            calls: &self.calls,
            id,
            serial,
            abort: task.abort_handle(),
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
        let (link, client_id, generation, dir) = {
            let mut inner = self.inner();
            if inner.phase == Phase::Closed || inner.phase == Phase::Idle {
                return Err(not_open());
            }
            inner.current_session = Some(session_id.to_string());
            match inner.phase {
                Phase::Connected => {}
                // Remembered: the rejoin sends it (1.2.6 step 3).
                _ => return Ok(()),
            }
            let Some(dir) = inner.shown.clone() else { return Ok(()) };
            let Some(folder) = inner.folders.get(&dir) else { return Ok(()) };
            (folder.link.clone(), folder.client_id.clone(), folder.generation, dir)
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
                    self.lost(&dir, generation, &e.code);
                }
                Err(e)
            }
        }
    }

    /// The window is closing: stop its calls, leave the engine, stop reading.
    async fn close(&self) {
        self.end(false).await;
    }

    /// As close, for a window moving to another folder: the engine keeps
    /// the folders' running work going.
    async fn end(&self, keep_work: bool) {
        let running: Vec<Running> = self.calls.lock().unwrap().drain().map(|(_, r)| r).collect();
        for call in running {
            call.abort.abort();
        }
        let (folders, reconnect) = {
            let mut inner = self.inner();
            inner.phase = Phase::Closed;
            inner.shown = None;
            (std::mem::take(&mut inner.folders), inner.reconnect.take())
        };
        if let Some(task) = reconnect {
            task.abort();
        }
        for (_, folder) in folders {
            self.retire_keeping(&folder.link.http, &folder.client_id, keep_work).await;
            drop(folder.reader);
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

/// Overwrites a secret's bytes with zeros. `black_box` keeps the writes from
/// being dropped as dead stores before the memory is freed.
pub fn wipe(bytes: &mut [u8]) {
    bytes.fill(0);
    std::hint::black_box(&*bytes);
}

/// Overwrites every string in a JSON value with zeros, in place (the key
/// names stay: they name fields, never hold a secret).
pub fn wipe_value(value: &mut Value) {
    match value {
        Value::String(text) => {
            let mut bytes = std::mem::take(text).into_bytes();
            wipe(&mut bytes);
        }
        Value::Array(items) => items.iter_mut().for_each(wipe_value),
        Value::Object(map) => map.values_mut().for_each(wipe_value),
        _ => {}
    }
}

/// `api`'s route check, for the fixture player too, so a recorded run
/// refuses what a real one would: `route_not_allowed` for an unlisted route.
pub fn check_route(method: &str, path: &str) -> AppResult<()> {
    Route::find(method, path).map(|_| ())
}

/// `api_secret`'s route check, for the fixture player too: `secret_route` for any other path.
pub fn check_secret_route(path: &str) -> AppResult<()> {
    Route::secret(path).map(|_| ())
}

// The commands' side (1.3): one connection per window label in AppState.

fn connection(app: &crate::host::AppHandle, label: &str) -> Option<Arc<WindowConn>> {
    app.state::<AppState>().windows.lock().unwrap().get(label).cloned()
}

/// Version check, ensure, join steps 1-4, stream start. Called once per window, and again by
/// "Try again" after a failure. Idempotent: a second call while connected returns the same result.
pub async fn open(app: &crate::host::AppHandle, label: &str, cwd: &str) -> AppResult<OpenResult> {
    let state = app.state::<AppState>();
    let mut launch = state.launch.clone();
    // A window other than the first joins the folder it was opened for
    // (windows.rs); `-D` names the first window's project only, so it applies
    // while the first window still shows its start folder.
    let window = windows::launch_for(app, label);
    let cwd = match &window {
        Some(w) if label != windows::FIRST => w.cwd.to_string_lossy().into_owned(),
        _ => cwd.to_string(),
    };
    let first_at_start = label == windows::FIRST && window.as_ref().is_none_or(|w| w.cwd == launch.cwd);
    if !first_at_start {
        launch.data_dir = None;
    }
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
    let plan = async move { plan_for(&launch, settings_yolo, &cwd, env, crush_check(crush_path)).await };
    conn.open(plan).await
}

pub async fn retry(app: &crate::host::AppHandle, label: &str) -> AppResult<()> {
    connection(app, label).ok_or_else(not_open)?.retry()
}

/// Generic call for the window (section 1.3, `api`): adds client_id, checks the route list.
/// With `call_id`: no time limit, and the call is registered for `abort` until it ends (1.2.4).
/// `dir`: the folder of the chat the call is about (the shown folder with none).
pub async fn call(
    app: &crate::host::AppHandle,
    label: &str,
    method: &str,
    path: &str,
    query: Option<serde_json::Map<String, serde_json::Value>>,
    body: Option<serde_json::Value>,
    call_id: Option<String>,
    dir: Option<&str>,
) -> AppResult<serde_json::Value> {
    let Some(conn) = connection(app, label) else {
        Route::find(&method.to_ascii_uppercase(), path)?;
        return Err(not_open());
    };
    conn.call_in(dir, method, path, query, body, call_id).await
}

/// Opens a folder beside the one the window shows (`engine_open_folder`); the
/// window keeps it joined, so its chats answer at once.
pub async fn open_folder(app: &crate::host::AppHandle, label: &str, cwd: &str) -> AppResult<OpenedFolder> {
    let (conn, plan) = plan_for_window(app, label, cwd)?;
    conn.open_folder(plan).await
}

/// Shows a folder (`engine_show_folder`), opening it first when the window does
/// not keep it joined; nothing leaves, so a switch costs no engine restart.
pub async fn show_folder(app: &crate::host::AppHandle, label: &str, cwd: &str) -> AppResult<OpenResult> {
    let (conn, plan) = plan_for_window(app, label, cwd)?;
    conn.show(cwd, plan).await
}

/// Lets a folder go (`engine_leave_folder`); its running work finishes
/// (keep_work), as it did when a window moved away before.
pub async fn leave_folder(app: &crate::host::AppHandle, label: &str, cwd: &str) -> AppResult<()> {
    connection(app, label).ok_or_else(not_open)?.leave_folder(cwd).await
}

/// The window's connection and the plan for `cwd`, as open builds them; the
/// window must have joined before it can take another folder.
fn plan_for_window(
    app: &crate::host::AppHandle,
    label: &str,
    cwd: &str,
) -> AppResult<(Arc<WindowConn>, impl Future<Output = AppResult<Plan>>)> {
    let conn = connection(app, label).ok_or_else(not_open)?;
    let state = app.state::<AppState>();
    let launch = state.launch.clone();
    let cwd = cwd.to_string();
    let (crush_path, settings_yolo) = {
        let settings = state.settings.lock().unwrap();
        (settings.crush_path.clone(), settings.yolo)
    };
    let env: EnvSource = Arc::new(login_env::login_env);
    let plan = async move { plan_for(&launch, settings_yolo, &cwd, env, crush_check(crush_path)).await };
    Ok((conn, plan))
}

/// `api_abort` (1.3): aborts that window's running call with this id; false when none is
/// running. The aborted call fails with code "aborted" (1.2.2).
pub fn abort(app: &crate::host::AppHandle, label: &str, call_id: &str) -> bool {
    connection(app, label).is_some_and(|conn| conn.abort_call(call_id))
}

pub async fn set_current_session(
    app: &crate::host::AppHandle,
    label: &str,
    session_id: &str,
) -> AppResult<()> {
    connection(app, label).ok_or_else(not_open)?.set_current_session(session_id).await
}

/// Events of another folder's engine for every window that did not join
/// that folder (watch.rs); `resync` alone asks them to read its chats again.
pub fn push_folder(app: &crate::host::AppHandle, dir: &str, events: Vec<Event>, resync: bool) {
    let conns: Vec<Arc<WindowConn>> = app.state::<AppState>().windows.lock().unwrap().values().cloned().collect();
    for conn in conns {
        conn.push_side(dir, events.clone(), resync);
    }
}

/// Whether a window joined some other folder than `dir`, so `dir`'s events
/// are wanted by push_folder.
pub fn heard_elsewhere(app: &crate::host::AppHandle, dir: &str) -> bool {
    let conns: Vec<Arc<WindowConn>> = app.state::<AppState>().windows.lock().unwrap().values().cloned().collect();
    conns.iter().any(|conn| conn.folder().as_deref() != Some(dir))
}

/// The wire form of 1.4.2.
pub fn take(app: &crate::host::AppHandle, label: &str) -> Vec<serde_json::Value> {
    connection(app, label).map(|conn| conn.take()).unwrap_or_default()
}

/// `api_secret` (6.4): `body` is the JSON bytes; they are overwritten with zeros
/// when the call ends, either way, and appear in no log line or error.
pub async fn call_secret(app: &crate::host::AppHandle, label: &str, path: &str, mut body: Vec<u8>) -> AppResult<Value> {
    let result = match connection(app, label) {
        Some(conn) => conn.call_secret(path, &mut body).await,
        None => check_secret_route(path).and(Err(not_open())),
    };
    wipe(&mut body);
    result
}

/// The engine this window joined (its `data_dir` names the project's log
/// folder, 2.28); None before the first join.
#[allow(dead_code)] // SYS's logs.rs reads it
pub fn ensured(app: &crate::host::AppHandle, label: &str) -> Option<Ensured> {
    connection(app, label)?.inner().opened.as_ref().map(|opened| opened.engine.clone())
}

/// Window closing: abort every running call of that window (so its `!` commands end with it,
/// 1.2.4), then DELETE /v1/clients/{client_id} (1 s limit, errors only logged), stop the reader.
pub async fn close(app: &crate::host::AppHandle, label: &str) {
    let conn = app.state::<AppState>().windows.lock().unwrap().remove(label);
    if let Some(conn) = conn {
        conn.close().await;
    }
}

/// As close, for a window moving to another folder: the folder it leaves
/// keeps its running work (WindowConn::end).
pub async fn leave(app: &crate::host::AppHandle, label: &str) {
    let conn = app.state::<AppState>().windows.lock().unwrap().remove(label);
    if let Some(conn) = conn {
        conn.end(true).await;
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
        crate::host::async_runtime::block_on(f)
    }

    /// Every line `WindowConn::log` wrote during the tests.
    pub(super) static LOGGED: Mutex<Vec<String>> = Mutex::new(Vec::new());

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
        plan_at(finder, "/tmp/proj")
    }

    /// A plan for `cwd`, as `plan_for` makes one for another folder.
    fn plan_at(finder: Arc<dyn Finder>, cwd: &str) -> Plan {
        let client_id = uuid::Uuid::new_v4().to_string();
        Plan {
            cwd: cwd.into(),
            body: create_body(cwd, None, &client_id, true, false, &[]),
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

    /// Another folder the window opened, its fake engine kept alive by the test.
    struct Extra {
        fake: FakeEngine,
        opened: OpenedFolder,
    }

    /// A window joined to a new fake engine (workspace `ws-1`); it closes when dropped.
    struct Joined {
        name: String,
        fake: FakeEngine,
        finder: Arc<FakeFinder>,
        conn: Arc<WindowConn>,
        rings: Arc<AtomicUsize>,
        opened: OpenResult,
        extras: Mutex<Vec<Extra>>,
    }

    fn joined(name: &str, tuning: Tuning) -> Joined {
        let fake = FakeEngine::start(name, engine(WS));
        let finder = FakeFinder::new(&fake);
        let (conn, rings) = window(tuning, false);
        let opened = run(conn.open(std::future::ready(Ok(plan(finder.clone()))))).unwrap();
        Joined {
            name: name.to_string(),
            fake,
            finder,
            conn,
            rings,
            opened,
            extras: Mutex::default(),
        }
    }

    impl Joined {
        /// Opens `dir` as another folder of the window (engine_open_folder),
        /// on a fake engine whose workspace is `workspace`.
        fn folder(&self, dir: &str, workspace: &'static str) -> OpenedFolder {
            let n = self.extras.lock().unwrap().len();
            let fake = FakeEngine::start(&format!("{}-folder-{n}", self.name), engine(workspace));
            let finder = FakeFinder::new(&fake);
            let opened = run(self.conn.open_folder(std::future::ready(Ok(plan_at(finder, dir))))).expect("the folder joined");
            self.extras.lock().unwrap().push(Extra { fake, opened: opened.clone() });
            opened
        }

        fn extra(&self, dir: &str) -> Option<()> {
            self.extras.lock().unwrap().iter().find(|x| x.opened.dir == dir).map(|_| ())
        }

        /// Every DELETE the window's folders heard, by folder ("" is the one
        /// it joined first), in the order they were heard.
        fn deletes(&self) -> Vec<(String, String)> {
            let mut out = Vec::new();
            let mut collect = |dir: &str, fake: &FakeEngine| {
                for seen in fake.seen().iter().filter(|s| s.method == "DELETE") {
                    out.push((dir.to_string(), seen.target.clone()));
                }
            };
            collect("", &self.fake);
            for extra in self.extras.lock().unwrap().iter() {
                collect(&extra.opened.dir, &extra.fake);
            }
            out.sort();
            out
        }

        /// The event stream requests `dir`'s engine heard.
        fn extra_streams(&self, dir: &str) -> usize {
            match self.extras.lock().unwrap().iter().find(|x| x.opened.dir == dir) {
                Some(extra) => extra.fake.seen().iter().filter(|s| s.path().ends_with("/events")).count(),
                None => 0,
            }
        }

        /// Shows `dir` (engine_show_folder); a folder the window keeps joined
        /// needs no plan, and an unjoined one joins on this window's engine.
        fn show(&self, dir: &str) -> OpenResult {
            let finder: Arc<dyn Finder> = self.finder.clone();
            let plan = std::future::ready(Ok(plan_at(finder, dir)));
            run(self.conn.show(dir, plan)).expect("the folder shows")
        }

        fn end_extra_streams(&self, dir: &str) {
            if let Some(extra) = self.extras.lock().unwrap().iter().find(|x| x.opened.dir == dir) {
                extra.fake.end_streams();
            }
        }

        fn wait_extra_streams(&self, dir: &str, n: usize) -> bool {
            let end = Instant::now() + Duration::from_secs(5);
            while Instant::now() < end {
                if self.extra_streams(dir) >= n {
                    return true;
                }
                std::thread::sleep(Duration::from_millis(5));
            }
            false
        }
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
        // A 2 s first wait keeps the window reconnecting, never failed, until
        // well after the calls below, even on a loaded machine.
        let j = joined("conn-unavailable", Tuning { first_wait: Duration::from_secs(2), ..TEST_TUNING });
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
        assert!(started.elapsed() < Duration::from_secs(1), "at once, not after the rejoin 2 s later");
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
    fn a_failed_rejoin_keeps_its_claim_for_the_next_try() {
        let j = joined("conn-keep-claim", TEST_TUNING);
        j.conn.take();
        // An engine that bars a retired client id from creating again, as the
        // real one does, and whose first rejoin's stream fails after the create.
        let retired = Arc::new(Mutex::new(Vec::<String>::new()));
        let streams = Arc::new(AtomicUsize::new(0));
        let (barred, opened) = (retired.clone(), streams.clone());
        j.fake.set_handler(engine_with(WS, move |seen| {
            if let Some(id) = seen.path().strip_prefix("/v1/clients/") {
                barred.lock().unwrap().push(id.to_string());
                return Some(Answer::Status(204));
            }
            let id = seen.json()["client_id"].as_str().unwrap_or_default().to_string();
            if seen.line() == "POST /v1/workspaces" && barred.lock().unwrap().contains(&id) {
                return Some(Answer::Json(409, json!({"message": "client retired", "code": "client_retired"})));
            }
            let first = seen.path().ends_with("/events") && opened.fetch_add(1, SeqCst) == 0;
            first.then(|| Answer::Json(404, json!({"message": "workspace not found"})))
        }));
        j.fake.forget();
        j.fake.end_streams();
        let taken = take_until(&j.conn, "connected");
        assert_eq!(states(&taken), pairs(&[("reconnecting", 1), ("reconnecting", 2), ("connected", 2)]));
        assert!(retired.lock().unwrap().is_empty(), "no DELETE /v1/clients: the window's id must stay able to rejoin");
        let creates: Vec<Value> =
            j.fake.seen().iter().filter(|s| s.line() == "POST /v1/workspaces").map(|s| s.json()["client_id"].clone()).collect();
        assert_eq!(creates, [json!(j.opened.client_id), json!(j.opened.client_id)], "both tries claim with the same id");
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
        let generation = j.conn.inner().folders["/tmp/proj"].generation;
        j.conn.lost("/tmp/proj", generation, "first trigger");
        j.conn.lost("/tmp/proj", generation, "second trigger");
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
        design.extend(DESIGN_5_6_ROUTES.iter().flat_map(|(m, patterns)| patterns.iter().map(move |p| (*m, *p))));
        design.extend(FALLBACK_ROUTES.iter().flat_map(|(m, patterns)| patterns.iter().map(move |p| (*m, *p))));
        design.extend(OTHER_FOLDER_ROUTES.iter().map(|p| ("GET", *p)));
        design.extend(OPENROUTER_ROUTES.iter().map(|p| ("GET", *p)));
        design.sort();
        assert_eq!(listed, design, "the route list is design 1.3's table, 5-6's 6.5, the fallback, other-folder and OpenRouter routes, no more and no less");
        for (method, pattern) in ROUTES {
            let path = pattern
                .replace("{sid}", "s-1")
                .replace("{mid}", "m-1")
                .replace("{kind}", "claude")
                .replace("{lsp}", "gopls")
            .replace("{flow_id}", "f-1")
            .replace("{fid}", "form-1")
            .replace("{name}", "scholars-edition")
            .replace("{hid}", "h-1")
            .replace("{aid}", "a-1");
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
        ("POST", &["sessions/{sid}/kept-skills", "sessions/{sid}/model"]),
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

    /// desktop-design-5-6 6.5's routes (the functions not marked P3.3), row by row.
    const DESIGN_5_6_ROUTES: &[(&str, &[&str])] = &[
        ("GET", &["commands", "mcp/prompts", "agent/capabilities", "saved-sessions", "questions/pending", "secure-forms",
            "skills/installed", "skills/search", "mcp/disabled", "mcp/enabled", "mcp/pending-auth", "mcp/auth-url",
            "sessions/{sid}/helpers", "sessions/{sid}/jobs", "servers", "remote"]),
        ("POST", &["mcp/get-prompt", "agent/sessions/{sid}/summarize", "config/refresh-oauth", "config/import-copilot", "agent/init",
            "auth/flows", "auth/flows/{flow_id}/code", "auth/flows/{flow_id}/cancel", "saved-sessions", "questions/answer",
            "questions/cancel", "secure-forms/{fid}/cancel", "skills/manage", "mcp/disabled", "mcp/config-disabled", "mcp/auth",
            "mcp/docker/enable", "mcp/docker/disable", "sessions/{sid}/helpers/{hid}/stop", "sessions/{sid}/jobs/kill",
            "sessions/{sid}/background", "remote/start", "remote/stop", "remote/presence", "remote/actions/{aid}/done"]),
        ("GET", &["/v1/memory", "/v1/themes", "/v1/themes/{name}", "/v1/updates/clis", "/v1/updates/crush"]),
        ("POST", &["/v1/providers/refresh", "/v1/memory/claim-notice", "/v1/cpu-affinity", "/v1/themes/resolve", "/v1/themes",
            "/v1/themes/{name}/rename", "/v1/themes/{name}/revert", "/v1/updates/clis/decline", "/v1/updates/clis/install",
            "/v1/updates/crush/decline"]),
        ("PUT", &["/v1/memory/limit", "/v1/themes/{name}"]),
        ("DELETE", &["/v1/themes/{name}"]),
    ];

    /// Other folders' open chats, read from their saved files (TimelineStack).
    const OTHER_FOLDER_ROUTES: &[&str] = &["saved-sessions/{sid}", "saved-sessions/{sid}/messages"];

    /// OpenRouter's catalog for the model menu's "+" (internal/rpc/routes_openrouter.go).
    const OPENROUTER_ROUTES: &[&str] = &["openrouter/catalog"];

    /// Accounts and the fallback order (internal/rpc/routes_fallback.go), row by row.
    const FALLBACK_ROUTES: &[(&str, &[&str])] = &[
        ("GET", &["fallback"]),
        ("PUT", &["fallback/order", "accounts/{aid}"]),
        ("POST", &["accounts", "accounts/{aid}/login", "/v1/limits/refill"]),
        ("DELETE", &["accounts/{aid}"]),
    ];

    #[test]
    fn chat_model_save_reaches_engine() {
        let j = joined("conn-chat-model", TEST_TUNING);
        j.fake.forget();
        let body = json!({"model": {"provider": "codex-cli", "model": "gpt-6-sol", "reasoning_effort": "high"}});
        run(j.conn.call("POST", "sessions/s-1/model", None, Some(body.clone()), None)).unwrap();
        let seen = j.fake.seen();
        let request = seen.iter().find(|s| s.line() == "POST /v1/workspaces/ws-1/sessions/s-1/model")
            .expect("chat model save reached the engine");
        assert_eq!(request.json(), body);
        assert_eq!(request.query("client_id").as_deref(), Some(j.opened.client_id.as_str()));
        assert!(check_route("DELETE", "sessions/s-1/model").is_err());
    }

    #[test]
    fn route_list_has_fallback_routes() {
        for (method, path) in [("GET", "fallback"), ("PUT", "fallback/order"), ("POST", "accounts"), ("PUT", "accounts/claude-2"),
            ("DELETE", "accounts/claude-2"), ("POST", "accounts/claude-2/login"), ("POST", "/v1/limits/refill")] {
            assert!(check_route(method, path).is_ok(), "{method} {path} is refused");
        }
        assert_eq!(check_route("DELETE", "fallback").err().map(|e| e.code), Some("route_not_allowed".to_string()));
    }

    #[test]
    fn route_list_has_5_6_routes() {
        for (method, patterns) in DESIGN_5_6_ROUTES {
            for pattern in *patterns {
                assert!(ROUTES.contains(&(*method, *pattern)), "{method} {pattern} is missing from the route list");
            }
        }
        // They reach the engine through a joined window, with this window's client_id.
        let j = joined("conn-routes-56", TEST_TUNING);
        j.fake.forget();
        run(j.conn.call("GET", "skills/search", Some(Map::from_iter([("q".to_string(), json!("go"))])), None, None)).unwrap();
        run(j.conn.call("PUT", "/v1/themes/my-theme", None, Some(json!({"base": "scholars-edition", "palette": {}})), None)).unwrap();
        run(j.conn.call("POST", "sessions/s-1/helpers/h-1/stop", None, None, None)).unwrap();
        let lines = lines(&j.fake.seen());
        assert!(lines.contains(&"GET /v1/workspaces/ws-1/skills/search".to_string()), "{lines:?}");
        assert!(lines.contains(&"PUT /v1/themes/my-theme".to_string()), "{lines:?}");
        assert!(lines.contains(&"POST /v1/workspaces/ws-1/sessions/s-1/helpers/h-1/stop".to_string()), "{lines:?}");
        // The secret routes are not among them.
        for path in ["secure-forms/form-1/submit", "config/provider-key", "auth/verify-key"] {
            assert_eq!(Route::find("POST", path).err().map(|e| e.code), Some("route_not_allowed".to_string()), "{path}");
        }
    }

    const MARKER: &str = "sk-s5-marker-7f3a";

    fn secret_body() -> Vec<u8> {
        serde_json::to_vec(&json!({"provider_id": "openai", "api_key": MARKER, "nested": {"values": [MARKER]}})).unwrap()
    }

    #[test]
    fn call_secret_never_logs() {
        let j = {
            let fake = FakeEngine::start("conn-secret-logs", engine(WS));
            let finder = FakeFinder::new(&fake);
            let (conn, rings) = window(TEST_TUNING, true);
            let opened = run(conn.open(std::future::ready(Ok(plan(finder.clone()))))).unwrap();
            Joined {
                name: "conn-secret-logs".to_string(),
                fake,
                finder,
                conn,
                rings,
                opened,
                extras: Mutex::default(),
            }
        };
        LOGGED.lock().unwrap().clear();
        // A refused key: the engine's error keeps its code and message only.
        j.fake.set_handler(engine_with(WS, |seen| {
            seen.path().ends_with("/auth/verify-key").then(|| Answer::Json(400, json!({"message": "invalid key", "code": "invalid_key"})))
        }));
        let mut body = secret_body();
        let e = run(j.conn.call_secret("auth/verify-key", &mut body)).err().unwrap();
        assert_eq!((e.code.as_str(), e.message.as_str()), ("invalid_key", "invalid key"));
        // An accepted one, a refused route and a body that is not JSON.
        let mut body = secret_body();
        run(j.conn.call_secret("config/provider-key", &mut body)).unwrap();
        let mut body = secret_body();
        let refused = run(j.conn.call_secret("config/set", &mut body)).err().unwrap();
        let mut broken = format!("{{\"api_key\": \"{MARKER}").into_bytes();
        let unparsed = run(j.conn.call_secret("config/provider-key", &mut broken)).err().unwrap();
        let logged = LOGGED.lock().unwrap().clone();
        assert!(logged.iter().any(|line| line.contains("POST auth/verify-key")), "the call was logged by route: {logged:?}");
        for text in logged.iter().chain([&e.message, &refused.message, &unparsed.message, &format!("{e:?}{refused:?}{unparsed:?}")]) {
            assert!(!text.contains(MARKER), "the secret leaked: {text}");
        }
        // The fixture player records the route and the key names, never a value.
        let dir = scratch("conn-secret-fixture");
        std::fs::write(dir.join("fixture.json"), "{}").unwrap();
        let fixture = fixture::Fixture::load(&dir.join("fixture.json")).unwrap();
        let launch = Launch { probe: Some(dir.join("probe.json")), ..Default::default() };
        let body: Value = serde_json::from_slice(&secret_body()).unwrap();
        assert_eq!(fixture.api_secret(&launch, "config/provider-key", &body).unwrap(), Value::Null);
        assert_eq!(fixture.api_secret(&launch, "config/set", &body).err().map(|e| e.code), Some("secret_route".into()));
        let recorded = std::fs::read_to_string(dir.join("probe.json.api.jsonl")).unwrap();
        let line: Value = serde_json::from_str(recorded.trim()).unwrap();
        assert_eq!(line["secret"], "config/provider-key");
        assert_eq!(line["keys"], json!(["api_key", "nested", "provider_id"]));
        assert!(!recorded.contains(MARKER), "{recorded}");
    }

    #[test]
    fn call_secret_zeroes_body() {
        let j = joined("conn-secret-zero", TEST_TUNING);
        let mut body = secret_body();
        run(j.conn.call_secret("config/provider-key", &mut body)).unwrap();
        assert!(!body.is_empty() && body.iter().all(|&b| b == 0), "the body was overwritten with zeros");
        // Also when the call fails, and when the route is refused before anything is sent.
        j.fake.set_handler(engine_with(WS, |seen| seen.path().ends_with("/provider-key").then_some(Answer::Status(500))));
        let mut body = secret_body();
        assert!(run(j.conn.call_secret("config/provider-key", &mut body)).is_err());
        assert!(body.iter().all(|&b| b == 0));
        let mut body = secret_body();
        assert!(run(j.conn.call_secret("sessions", &mut body)).is_err());
        assert!(body.iter().all(|&b| b == 0));
        // The parsed copy is zeroed in place too.
        let mut value = json!({"api_key": MARKER, "list": [MARKER, 3]});
        wipe_value(&mut value);
        assert_eq!(value, json!({"api_key": "", "list": ["", 3]}));
    }

    #[test]
    fn call_secret_sends_content_length() {
        let j = joined("conn-secret-length", TEST_TUNING);
        j.fake.forget();
        let sent: Value = serde_json::from_slice(&secret_body()).unwrap();
        let mut body = secret_body();
        run(j.conn.call_secret("secure-forms/form-1/submit", &mut body)).unwrap();
        let seen = j.fake.seen();
        let submit = seen.iter().find(|s| s.path().ends_with("/secure-forms/form-1/submit")).expect("the submit reached the engine");
        assert_eq!(submit.method, "POST");
        let length = serde_json::to_vec(&sent).unwrap().len().to_string();
        assert_eq!(submit.header("content-length"), Some(length.as_str()));
        assert!(!submit.chunked && submit.header("transfer-encoding").is_none());
        assert_eq!(submit.json(), sent);
        assert_eq!(submit.query("client_id").as_deref(), Some(j.opened.client_id.as_str()));
    }

    #[test]
    fn secret_route_refused() {
        let j = joined("conn-secret-route", TEST_TUNING);
        j.fake.forget();
        for path in ["config/set", "sessions", "/v1/memory", "secure-forms/form-1/cancel", "secure-forms/../config/provider-key", "auth/verify-key/x", "/config/provider-key"] {
            let mut body = secret_body();
            let e = run(j.conn.call_secret(path, &mut body)).err().unwrap_or_else(|| panic!("{path} was allowed"));
            assert_eq!(e.code, "secret_route", "{path}");
        }
        assert!(j.fake.seen().is_empty(), "nothing was sent");
        for path in ["secure-forms/form-1/submit", "config/provider-key", "auth/verify-key"] {
            assert!(check_secret_route(path).is_ok(), "{path}");
        }
    }

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
            crate::host::async_runtime::spawn(async move {
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
    fn opening_more_than_four_folders_keeps_every_folder_connected() {
        let j = joined("conn-cap", TEST_TUNING);
        let one = j.folder("/tmp/one", "ws-one");
        let two = j.folder("/tmp/two", "ws-two");
        let three = j.folder("/tmp/three", "ws-three");
        assert_eq!(j.conn.folders_joined(), ["/tmp/one", "/tmp/proj", "/tmp/three", "/tmp/two"], "four folders fit");
        assert!(j.deletes().is_empty(), "nothing was given up");
        // Switching folders must not make an older folder disposable.
        j.show("/tmp/two");
        let four = j.folder("/tmp/four", "ws-four");
        let five = j.folder("/tmp/five", "ws-five");
        assert!(j.deletes().is_empty(), "opening more folders never disconnects older ones");
        assert_eq!(j.conn.folders_joined(), ["/tmp/five", "/tmp/four", "/tmp/one", "/tmp/proj", "/tmp/three", "/tmp/two"]);
        let answer = run(j.conn.call_in(Some("/tmp/one"), "GET", "sessions", None, None, None)).unwrap();
        let target = answer["echo"].as_str().unwrap();
        assert!(target.starts_with("/v1/workspaces/ws-one/sessions"));
        assert!(target.contains(&format!("client_id={}", one.client_id)));
        assert!(j.extra("/tmp/three").is_some() && j.extra("/tmp/two").is_some() && j.extra("/tmp/four").is_some());
        assert_eq!(three.dir, "/tmp/three");
        assert_eq!(two.dir, "/tmp/two");
        assert_eq!(four.dir, "/tmp/four");
        assert_eq!(five.dir, "/tmp/five");
    }

    #[test]
    fn a_call_with_a_folder_reaches_that_folder() {
        let j = joined("conn-route", TEST_TUNING);
        let other = j.folder("/tmp/other", "ws-other");
        j.fake.forget();
        let echoed = run(j.conn.call_in(Some("/tmp/other"), "GET", "sessions", None, None, None)).unwrap();
        let target = echoed["echo"].as_str().unwrap_or_default();
        assert!(target.starts_with("/v1/workspaces/ws-other/sessions"), "{target}");
        assert!(target.contains(&format!("client_id={}", other.client_id)), "{target}");
        assert!(j.fake.seen().is_empty(), "the shown folder hears nothing of it");
        // A folder the window has not joined is refused, and nothing is sent.
        let e = run(j.conn.call_in(Some("/tmp/nowhere"), "GET", "sessions", None, None, None)).err().unwrap();
        assert_eq!(e.code, "engine_unavailable");
        // A call without a folder goes to the folder the window shows.
        let echoed = run(j.conn.call("GET", "sessions", None, None, None)).unwrap();
        assert!(echoed["echo"].as_str().unwrap_or_default().starts_with("/v1/workspaces/ws-1/"));
    }

    #[test]
    fn showing_another_folder_keeps_every_folder_claimed() {
        let j = joined("conn-show", TEST_TUNING);
        let other = j.folder("/tmp/other", "ws-other");
        run(j.conn.set_current_session("s1"));
        j.fake.forget();
        j.conn.take();
        let opened = j.show("/tmp/other");
        assert_eq!(opened.cwd, "/tmp/other");
        assert_eq!(opened.workspace["id"], "ws-other");
        assert_eq!(opened.client_id, other.client_id);
        // The folder it left no longer reports a shown chat, so no phone thinks
        // that chat is on its screen.
        let cleared: Vec<(String, Value)> = j
            .fake
            .seen()
            .iter()
            .filter(|s| s.path().ends_with("/current-session"))
            .map(|s| (s.target.clone(), s.json()))
            .collect();
        assert_eq!(cleared, [(format!("/v1/workspaces/ws-1/current-session?client_id={}", j.opened.client_id), json!({"session_id": ""}))]);
        assert!(j.deletes().is_empty(), "showing a folder gives none up");
        assert!(!j.fake.wait_closed(1, Duration::from_millis(300)), "the folder it left keeps its stream");
        let taken = j.conn.take();
        let connected = payload(&taken, "connected");
        assert_eq!(connected["resync"], true, "the page reloads what it shows");
        assert_eq!(connected["workspace"]["id"], "ws-other");
        // Calls without a folder now go to the folder it shows.
        let echoed = run(j.conn.call("GET", "sessions", None, None, None)).unwrap();
        assert!(echoed["echo"].as_str().unwrap_or_default().starts_with("/v1/workspaces/ws-other/"));
        // Showing a folder it has not joined opens it.
        let opened = j.show("/tmp/proj");
        assert_eq!(opened.workspace["id"], "ws-1");
        assert!(j.deletes().is_empty(), "going back gives none up either");
    }

    #[test]
    fn an_other_folders_lost_stream_is_taken_up_again_without_a_reconnect() {
        let j = joined("conn-extra-lost", TEST_TUNING);
        j.folder("/tmp/other", "ws-other");
        let ensured = j.finder.runs().len();
        j.conn.take();
        j.end_extra_streams("/tmp/other");
        assert!(j.wait_extra_streams("/tmp/other", 2), "the folder's claim is taken up again");
        std::thread::sleep(Duration::from_millis(200));
        assert!(j.conn.take().is_empty(), "the page heard nothing of it");
        assert_eq!(j.finder.runs().len(), ensured, "the window did not rejoin");
    }

    #[test]
    fn closing_the_window_retires_every_folder() {
        let j = joined("conn-close-all", TEST_TUNING);
        let other = j.folder("/tmp/other", "ws-other");
        j.conn.take();
        run(j.conn.close());
        assert_eq!(
            j.deletes(),
            [
                ("".to_string(), format!("/v1/clients/{}", j.opened.client_id)),
                ("/tmp/other".to_string(), format!("/v1/clients/{}", other.client_id)),
            ],
            "every folder hears the window left"
        );
    }

    #[test]
    fn leaving_for_another_folder_asks_the_engine_to_keep_its_work() {
        let j = joined("conn-leave", TEST_TUNING);
        j.conn.take();
        run(j.conn.end(true));
        let retired: Vec<String> = j.fake.seen().iter().filter(|s| s.method == "DELETE").map(|s| s.target.clone()).collect();
        assert_eq!(retired, [format!("/v1/clients/{}?keep_work=1", j.opened.client_id)]);
    }

    #[test]
    fn close_aborts_calls_retires_the_client_and_stops() {
        let j = joined("conn-close", TEST_TUNING);
        j.fake.set_handler(engine_with(WS, |seen| seen.path().ends_with("/shell").then_some(Answer::Hang)));
        let call = {
            let conn = j.conn.clone();
            crate::host::async_runtime::spawn(async move {
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
