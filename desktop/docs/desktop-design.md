# Crush Desktop: app design for phases 3 and 4

Author: t31 (design only; nothing here is built yet). Written against branch `desktop` at `cfd85eee` (2026-10-18); engine pieces A-D were still being built, so their code was not read and `engine-design.md` is the contract for them.
Inputs: `desktop/PLAN.md` (approved), `desktop/PRODUCT.md`, the brief, `desktop/CHECKLIST.md`, `desktop/docs/engine-design.md` (the engine contract), research notes t1, t2, t3, t6, t8 and t10, the desktop code, the engine wire code, the terminal UI code, the approved picture (`desktop/.impeccable/mocks/decision/edition.png`) and the my-ui-design rules.

## 0. How to read this

- **What this pins.** Every name that two builders share: Rust module functions, Tauri command names with their arguments and results, window events, store files and their exported functions, JS wire types, file ownership per build piece, test names and the commands that run them. A builder may choose anything that is not pinned here, inside the files their piece owns.
- **Citations.** `path:line` points into the worktree `~/dev/crush-desktop` unless another root is named. `CHECKLIST.md:N` is the checklist row on that line. "Engine design §5.x" is `desktop/docs/engine-design.md`.
- **"Inferred"** marks anything taken from reasoning rather than read in code or docs. The builder confirms it at the place named.
- **Sizes.** Every size is in picture pixels, written `calc(N * var(--px))` as in `src/app.css:3-5`. "1px" means one real pixel (hairlines stay one pixel at every zoom). Type roles are the `--t-*` variables of `src/app.css:53-71`.
- **Phases.** Phase 3 owns the CHAT, TYPE and SIDE rows (PLAN.md:65, "the chat, typing box and side-info rows"). Phase 4 owns the branching behaviors of PLAN.md §6 (lines 96-112) and the done-line at PLAN.md:66. PLAN.md:292 also gives this design the START rows, the permission prompts (DLG-119 to DLG-140), the tree and file-restore dialogs (DLG-089 to DLG-112) and the branch rows of the left list; the phases 5-6 design only lists them. This design adds the Changes sheet (DLG-294 to DLG-311, opened by CHAT-084) and five LIST rows (LIST-012 in part, LIST-033 to LIST-037) because phase 3 rows cannot pass without them. Section 2.10 gives each pulled row's reason.
- **Order of the parts.** 1 architecture; 2 phase 3 rows; 3 phase 4; 4 look decisions; 5 build pieces and the scaffold; 6 tests and harness; 7 risks; 8 decisions made; 9 open questions; 10 where the code contradicts the plan; 11 not in scope.

---

## 1. Architecture

### 1.1 The shape

```
 crush (on PATH, or the app setting, or CRUSH_DESKTOP_CRUSH)
   │  `crush engine version`, `crush engine ensure --cwd DIR`  (child processes, JSON out)
   ▼
 Rust side (desktop/src-tauri/src)
   crush_bin ─ engine_cli ─ connection ──► uds_http (HTTP/1.1 over the engine's unix socket)
                               │   └────► sse (one long GET …/events) ─► batch::Batcher (piece D)
                               │                                            │ doorbell: window event "events-ready"
   settings · clipboard · opener · files                                    │
   commands.rs (every #[tauri::command])  ◄─────── invoke ───────────┐      ▼
                                                                     │  window: events.js
 Screen side (desktop/src)                                            │   one requestAnimationFrame → invoke("events_take")
   lib/engine/{wire,commands,api,events}.js                          │   → stores apply entries in order
   lib/stores/*.svelte.js (runes)  ──► components/*.svelte ──────────┘
   lib/render/* (Markdown, source map, math, Mermaid, code, ANSI, diff)
```

One window = one project = one engine connection = one `client_id` (engine design "What the desktop app calls" §2, `engine-design.md:2805`). Phases 5 and 6 add more windows through the same registry (section 1.2.9).

### 1.2 Rust side

Today the crate is `main.rs` (51 lines: `Launch{probe}`, `app_info`, `probe_write`, `--version`, `--probe`; `src-tauri/src/main.rs:10-51`) and the two-line `batch.rs` stub that engine piece D fills (`main.rs:5`; engine design §5.12, `engine-design.md:2378`). The dependencies are tauri, serde and serde_json (`src-tauri/Cargo.toml:10-13`).

**New direct dependencies** (scaffold S3 adds them). Every crate below is already in `src-tauri/Cargo.lock` as a dependency of Tauri, at the version given (the line is the crate's `name =` line). Turning on a feature the lock does not use yet may pull an optional crate that is not there; that is inferred not to happen for these features, and S3's `cargo check` settles it:

| Crate | Version (lock line) | Features | Used by |
|---|---|---|---|
| `tokio` | 1.53.3 (`Cargo.lock:3357`) | `net`, `io-util`, `time`, `sync`, `rt`, `fs` | sockets, timers |
| `hyper` | 1.12.0 (`:1265`) | `client`, `http1` | HTTP over the socket |
| `hyper-util` | 0.1.21 (`:1284`) | `tokio` | `TokioIo` |
| `http-body-util` | 0.1.5 (`:1246`) | — | `Full`, `BodyExt` |
| `bytes` | 1.12.1 (`:200`) | — | bodies |
| `uuid` | 1.28.0 (`:3627`) | `v4` | `client_id` |
| `dirs` | 7.0.0 (`:590`) | — | settings folder |
| `base64` | 0.22.1 (`:92`; the lock also has 0.21.7 and 0.23.1) | — | file reads |
| `gtk` (Linux only, `[target.'cfg(target_os = "linux")'.dependencies]`) | 0.18.2 (`:1134`) | — | clipboard |

Not used on purpose: tokio's `macros` and `process` features. They pull `tokio-macros` and `signal-hook-registry`, which are not in the lock. Child processes run with `std::process::Command` inside `tauri::async_runtime::spawn_blocking`; no `select!` is needed (section 1.2.5).

#### 1.2.1 Files and owners

| File | Owner piece (section 5) | Purpose |
|---|---|---|
| `main.rs` | S3 (frozen after) | flags, managed state, the handler list, window close hook |
| `error.rs` | S3 (frozen after) | `AppError` and its codes |
| `commands.rs` | S3 (frozen after) | every `#[tauri::command]`, each a short delegate into a module below |
| `state.rs` | S3 (frozen after) | `AppState`: the per-window connection registry and the settings handle |
| `fixture.rs` | S3 (frozen after) | the test-only fixture player (6.3) |
| `batch.rs` | engine piece D (not a desktop file) | the batcher |
| `crush_bin.rs`, `engine_cli.rs`, `uds_http.rs`, `sse.rs`, `connection.rs`, `login_env.rs` | P3.1 | finding crush, version check, ensure, HTTP, stream, reconnect, environment |
| `settings.rs`, `clipboard.rs`, `opener.rs`, `files.rs` | P3.2 | the desktop settings file (closed chats live here), clipboard, links, file reads |

S3 creates every file in the table with the exact public signatures below and bodies that return `Err(AppError::not_built("<function>"))`, so all pieces compile from the first day. Each piece then replaces only its own bodies.

#### 1.2.2 `error.rs` (S3)

```rust
#[derive(Debug, Clone, serde::Serialize)]
pub struct AppError {
    pub code: String,            // a stable code; the screens branch on it, never on message
    pub message: String,         // plain words, shown as is
    #[serde(skip_serializing_if = "Option::is_none")]
    pub field: Option<String>,   // proto.Error.field (secure_field_error), passed through
    #[serde(skip_serializing_if = "Option::is_none")]
    pub status: Option<u16>,     // the HTTP status when the error came from the engine
}
impl AppError {
    pub fn new(code: &str, message: impl Into<String>) -> Self;
    pub fn not_built(what: &str) -> Self;              // code "not_built"
    pub fn from_engine(status: u16, body: &[u8]) -> Self; // decodes proto.Error {message, code, field}
}
pub type AppResult<T> = Result<T, AppError>;
```

Engine errors keep the engine's `code` (`proto.Error`, `internal/proto/proto.go:37-41`; codes in engine design §0.1 "Errors", `engine-design.md:42-96`). When the body has no `code`, `from_engine` fills it by status the same way the engine does for plain errors: 400 `bad_request`, 404 `not_found`, 409 `conflict`, 500 `internal`, 503 `shutting_down`. App-side codes:

| Code | When |
|---|---|
| `crush_not_found` | no `crush` at the configured place or on PATH |
| `crush_too_old` | `crush engine version` fails or reports another protocol |
| `ensure_failed` | `crush engine ensure` printed no usable JSON |
| `protocol_mismatch`, `project_locked`, `start_failed`, `unsafe_socket_dir` | `crush engine ensure` error JSON (exit 3, 4, 5, 6; `engine-design.md:2798`), passed through |
| `engine_unavailable` | a call made while the window is reconnecting or failed; nothing was sent |
| `transport` | the socket broke during a call; the engine may or may not have acted (the composer's "uncertain" case, TYPE-048) |
| `timeout` | a call took longer than its limit |
| `route_not_allowed` | the `api` command was asked for a route outside the list in section 1.3 |
| `bad_path`, `too_big`, `not_a_file`, `is_a_directory` | `files.rs` |
| `link_refused` | `opener.rs` refused a link (its message is the terminal's text, CHAT-102) |
| `not_built` | the S3 placeholder |

#### 1.2.3 Finding `crush` and the version check (`crush_bin.rs`, `engine_cli.rs`, P3.1)

PLAN.md:51: PATH by default; a setting and `CRUSH_DESKTOP_CRUSH` can point elsewhere; a version check at start and a plain "too old" message.

```rust
// crush_bin.rs
pub struct CrushBin { pub path: std::path::PathBuf, pub source: BinSource }
pub enum BinSource { Env, Setting, Path }
/// Order: CRUSH_DESKTOP_CRUSH (tests), then settings.crush_path, then `crush` found on PATH
/// (the PATH of the login-shell environment, section 1.2.7, else the app's own PATH).
pub fn resolve(setting: Option<&str>, login_path: Option<&str>) -> AppResult<CrushBin>;

// engine_cli.rs
#[derive(serde::Deserialize, serde::Serialize, Clone)]
pub struct EngineVersion { pub version: String, pub protocol: i64 }
#[derive(serde::Deserialize, serde::Serialize, Clone)]
pub struct Ensured {
    pub address: String, pub socket: String, pub protocol: i64, pub version: String,
    pub pid: i64, pub data_dir: String, pub started: bool,
}
pub const APP_PROTOCOL: i64 = 1; // proto.ProtocolVersion, internal/proto/protocol.go:6
/// Runs `<bin> engine version` (5 s limit). Ok only when it printed {"version","protocol"} and
/// protocol == APP_PROTOCOL; any other outcome is Err(crush_too_old) with what crush said.
pub fn version(bin: &CrushBin) -> AppResult<EngineVersion>;
/// Runs `<bin> engine ensure --cwd DIR [--data-dir D] --timeout 15s` (20 s limit) with `env`
/// as the child's whole environment. Parses the success JSON or the error JSON
/// {"error":{"code","message"}} (exit 3/4/5/6/1, engine design §5.2).
pub fn ensure(bin: &CrushBin, cwd: &str, data_dir: Option<&str>, env: &[(String, String)]) -> AppResult<Ensured>;
```

- **The "too old" message** (shown by the screens, section 4.12) uses the error's message, which `version` builds as one of:
  - `The crush at <path> is too old for Crush Desktop: it has no engine command.` when the command exits non-zero (an older crush says "unknown command").
  - `The crush at <path> speaks engine protocol <n>; Crush Desktop needs protocol 1.` when the protocol differs.
  - `The crush at <path> did not answer in time.` after 5 s.
- `crush_not_found`: `Crush Desktop could not find crush on your PATH.` (or `… at <path>` for the setting and the variable).
- Both are checked once at start and again on "Try again".

#### 1.2.4 HTTP over the socket (`uds_http.rs`, P3.1)

The engine speaks HTTP/1.1 with any `Host` (`engine-design.md:2805`). One new connection per call (a unix connect costs microseconds; no pool to go stale across an engine restart).

```rust
pub struct EngineHttp { socket: std::path::PathBuf }
pub struct Reply { pub status: u16, pub body: serde_json::Value } // body Null when empty
impl EngineHttp {
    pub fn new(socket: &str) -> Self;
    /// `path_and_query` starts with "/v1/". Body is sent as JSON when Some.
    /// 2xx → Ok(Reply). Other statuses → Err(AppError::from_engine). Connect refused or a missing
    /// socket → Err(code "engine_unavailable"). A break after the request was written → Err("transport").
    pub async fn call(&self, method: &str, path_and_query: &str, body: Option<&serde_json::Value>,
                      limit: std::time::Duration) -> AppResult<Reply>;
    /// Opens a GET and returns the response body as a stream of raw byte chunks. Err on non-200.
    pub async fn stream(&self, path_and_query: &str) -> AppResult<ByteStream>;
}
pub struct ByteStream { /* hyper::body::Incoming */ }
impl ByteStream { pub async fn next_chunk(&mut self) -> Option<AppResult<bytes::Bytes>>; }
```

- Implementation (inferred to fit hyper 1): `tokio::net::UnixStream::connect`, `hyper_util::rt::TokioIo`, `hyper::client::conn::http1::handshake`, the connection future spawned, `Request::builder()` with `host: crush` and `content-type: application/json`, `http_body_util::Full<Bytes>`.
- **Limits:** 30 s for ordinary calls; the stream has none. The engine sends no keep-alive comments on the stream (`internal/server/handlers_workspace.go:171-208`), so a quiet stream is normal; a dead engine closes the socket, which ends the stream.

#### 1.2.5 The event stream and the batcher (`sse.rs`, `connection.rs`, P3.1)

The engine writes each event as `data: <json>\n\n` (`internal/server/handlers_workspace.go:206`); the envelope is `{"type": <payload type>, "payload": {"type": "created"|"updated"|"deleted", "payload": {…}}}` (`pubsub.Payload` and `pubsub.Event`, `internal/pubsub/events.go:49-68`, built by `envelope`, `internal/server/events.go:186`).

```rust
// sse.rs
/// Splits a byte stream into lines (LF, an optional CR before it dropped). Holds a partial
/// line across chunks; a line longer than 64 MiB is dropped with a log line.
pub struct LineSplitter { /* buffer */ }
impl LineSplitter {
    pub fn new() -> Self;
    pub fn push(&mut self, chunk: &[u8], out: &mut Vec<String>);
}
```

The reader task, per window:

1. `stream("/v1/workspaces/{id}/events?client_id=…")`.
2. For each chunk: split lines; for each line `batch::parse_sse_line(&line)` (piece D, `engine-design.md:2399-2400`); `Some(ev)` → push.
3. **Push and doorbell.** The window's `Batcher::new(5_000)` sits in a `std::sync::Mutex` with a flag `rung: bool`. After `push`, if `rung` is false, set it and emit the window event `events-ready` (no payload) with `app.emit_to(label, "events-ready", ())`. `events_take` (section 1.3) clears `rung` inside the same lock as `take()`. So the window gets exactly one doorbell per batch, and nothing at all while the engine is quiet.
4. On the stream's end or error: start a reconnect (1.2.6).

**One flush per frame** (engine design §5.12 "How take and the screens work", `engine-design.md:2443-2446`). The window's frame loop (section 1.5.4) answers a doorbell by asking for one animation frame; inside that frame it calls `events_take` once and applies the result. A doorbell that comes while a frame is already booked books nothing more. When the window is hidden, WebKit runs no frames, the batch grows, and at 5,000 entries the batcher turns the mergeable entries into one `Resync` (`engine-design.md:2454-2458`).

**The app's own connection events** go through the same batcher, so they keep their place among engine events. They use the envelope type `connection`, which the batcher treats as a barrier because it is not a mergeable type (`engine-design.md:2419`, "Unknown types are barriers"):

```json
{"type":"connection","payload":{"type":"updated","payload":{
  "state":"connecting|connected|reconnecting|failed",
  "attempt":0, "resync":false, "fresh_engine":false,
  "error":{"code":"…","message":"…"},
  "workspace":{ /* proto.Workspace, only with "connected" */ },
  "engine":{ /* Ensured, only with "connected" */ } }}}
```

`resync: true` on a `connected` event after a reconnect is the "push one Resync" of engine design §5.14 step 5 (`engine-design.md:2855`): the batcher's API takes only events (`push(ev: Event)`), so the client signals the resync as this barrier event. The screens treat it exactly like a `Resync` entry (section 1.5.3).

#### 1.2.6 Join, stay joined, reconnect (`connection.rs`, P3.1)

```rust
pub struct WindowConn { /* label, client_id, http, workspace, last current-session, state, batcher */ }
pub struct OpenResult {
    pub client_id: String,
    pub workspace: serde_json::Value,   // proto.Workspace as the engine answered
    pub engine: engine_cli::Ensured,
    pub version: engine_cli::EngineVersion,
    pub sent_yolo: bool,                // what the app asked for; compare with workspace.yolo
    pub cwd: String,                    // what the app asked for; compare with workspace.path
}
/// Version check, ensure, join steps 1-4, stream start. Called once per window, and again by
/// "Try again" after a failure. Idempotent: a second call while connected returns the same result.
pub async fn open(app: &tauri::AppHandle, label: &str, cwd: &str) -> AppResult<OpenResult>;
pub async fn retry(app: &tauri::AppHandle, label: &str) -> AppResult<()>;
/// Generic call for the window (section 1.3, `api`): adds client_id, checks the route list.
pub async fn call(app: &tauri::AppHandle, label: &str, method: &str, path: &str,
                  query: Option<serde_json::Map<String, serde_json::Value>>,
                  body: Option<serde_json::Value>) -> AppResult<serde_json::Value>;
pub async fn set_current_session(app: &tauri::AppHandle, label: &str, session_id: &str) -> AppResult<()>;
pub fn take(app: &tauri::AppHandle, label: &str) -> Vec<serde_json::Value>; // the wire form of 1.4.2
/// Window closing: DELETE /v1/clients/{client_id} (1 s limit, errors only logged), stop the reader.
pub async fn close(app: &tauri::AppHandle, label: &str);
```

**Join** (engine design "Join and stay joined", `engine-design.md:2807-2818`), in order:

1. `crush_bin::resolve`, then `engine_cli::version` (start check; failures end here with `crush_not_found` or `crush_too_old`).
2. `engine_cli::ensure(cwd)` with the login-shell environment (1.2.7).
3. `GET /v1/version`: `protocol` must be 1 (`proto.VersionInfo.Protocol`, `internal/proto/version.go:4-11`).
4. `POST /v1/workspaces` with `{"path": cwd, "data_dir": "", "version": "<app version>", "protocol": 1, "client_id": <uuid>, "yolo": settings.yolo (default true), "env": ["K=V", …]}` (`proto.Workspace`, `internal/proto/proto.go:15-34`). On 503 `shutting_down`: run ensure again and create once more; a second 503 is an error.
5. Start the reader task on `GET /v1/workspaces/{id}/events?client_id=`.
6. `POST /v1/workspaces/{id}/agent/init?client_id=` with `{"interactive": true}` (`internal/proto/requests.go:112-115`).
7. `current-session` is sent when the screens report the shown chat (`set_current_session`), and again after every rejoin with the last value.
8. Push `connection{state:"connected"}` and return `OpenResult`.

**Losing the engine** (engine design §5.14, `engine-design.md:2847-2855`). Any of these starts one reconnect (a second trigger while one runs is ignored): the stream ends or errors; a call fails with `engine_unavailable`; a call answers 503 `shutting_down`.

1. Push `connection{state:"reconnecting", attempt:n}`.
2. Run ensure. Between failed tries wait 250 ms, then double, capped at 10 s. After 3 failures in a row, push `connection{state:"failed", error}` and stop until `retry`.
3. Rejoin steps 3-6 with the same `client_id` (so the engine's 10 s grace of 5.5 keeps this window's work), then `current-session` with the last value.
4. Push `connection{state:"connected", resync:true, fresh_engine: <the workspace id changed>}`.

While reconnecting or failed, `call` returns `engine_unavailable` at once. The screens treat that as "not sent" (TYPE-048).

#### 1.2.7 The login-shell environment (`login_env.rs`, P3.1)

The first app's environment wins in a workspace (`engine-design.md:586`), and a desktop app started from the menu may lack the terminal's PATH. So the app sends its user's login-shell environment.

```rust
/// Runs `$SHELL -l -i -c 'printf "\0__CRUSH_ENV__\0"; env -0'` (stdin /dev/null, 5 s limit) and
/// parses the NUL-separated pairs after the marker, so anything the shell's startup prints first
/// is skipped. On any failure (no SHELL, timeout, no marker) it returns the app's own environment.
/// Always removes: CRUSH_DESKTOP_CRUSH, DESKTOP_STARTUP_ID, XDG_ACTIVATION_TOKEN, INVOCATION_ID.
/// Cached for the app's life; computed on a blocking thread at the first `engine_open`.
pub fn login_env() -> Vec<(String, String)>;
```

- Inferred: `-i` is needed because many users set PATH in `.zshrc`, which only interactive shells read. The marker makes the parse safe against prompts and greetings.
- In the test sandbox `SHELL` is unset (`tests/sandbox.sh:29-37` passes no `SHELL`), so tests always use the app's own environment.

#### 1.2.8 Desktop services (`settings.rs`, `clipboard.rs`, `opener.rs`, `files.rs`, P3.2)

**Settings file.** `dirs::config_dir()/crush-desktop/settings.json` (Linux `~/.config/crush-desktop/settings.json`; Mac `~/Library/Application Support/crush-desktop/settings.json`). Written atomically (temp file, then rename), read again before every change, guarded by one in-process mutex. Every change emits the window event `settings-changed` to all windows. Shape (phases 5 and 6 add keys; unknown keys are kept on write):

```json
{
  "version": 1,
  "crush_path": null,
  "yolo": true,
  "closed_chats": { "<engine data_dir>": ["<session id>", "…"] }
}
```

```rust
#[derive(serde::Serialize, serde::Deserialize, Clone)]
pub struct Settings { pub version: u32, pub crush_path: Option<String>, pub yolo: bool,
                      pub closed_chats: std::collections::BTreeMap<String, Vec<String>>,
                      #[serde(flatten)] pub other: serde_json::Map<String, serde_json::Value> }
pub fn load() -> Settings;                                   // defaults when missing or broken
pub fn set_key(app: &tauri::AppHandle, key: &str, value: serde_json::Value) -> AppResult<Settings>; // known keys only: crush_path, yolo
pub fn set_closed(app: &tauri::AppHandle, project: &str, session_id: &str, closed: bool) -> AppResult<Settings>;
```

The closed set is keyed by the engine's `data_dir` (from `Ensured.data_dir`), the project's one stable name (engine design §5.2: one engine per data folder). PLAN.md:103: "saved in the desktop app's own settings file, so every desktop window agrees and it survives a restart".

**Clipboard.** Writing goes through Rust so it works the same from keys, menus and auto-copy, without the web view's clipboard permission rules:

```rust
pub fn write_text(app: &tauri::AppHandle, text: &str) -> AppResult<()>; // Linux: gtk::Clipboard (CLIPBOARD selection) on the main thread via app.run_on_main_thread; Mac: NSPasteboard (inferred, objc2-app-kit is in the lock)
pub fn read_text(app: &tauri::AppHandle) -> AppResult<Option<String>>;  // Ctrl+Shift+V (TYPE-010)
```

Pasting with Ctrl+V into the typing box stays the web view's own `paste` event (text and images, TYPE-014).

**Links** (CHAT-102, PLAN.md:129 "web links open in your browser; file links open in your editor"):

```rust
pub enum LinkKind { Web, File(std::path::PathBuf) }
/// Same rules and texts as the terminal (CHECKLIST.md:211): http, https and mailto are web links;
/// file links are relative to `cwd`, may start with `~`, and a trailing `:line:col` is dropped
/// when no file has that exact name. Refusals: "cannot open links of type <scheme>",
/// "cannot open a file on another computer", "link does not name a local file".
pub fn classify(url: &str, cwd: &str) -> AppResult<LinkKind>;
/// Linux: the first of xdg-open, x-www-browser, www-browser that exists, detached; Mac: `open`.
/// File links use the same opener, which opens the file in the program the desktop assigns to
/// it (inferred reading of "your editor"; a terminal-tab editor is Ctrl+O, phase 6).
/// When the app runs with --probe, nothing is started: the request is appended as one JSON line
/// to "<probe file>.opened.jsonl" (so a test never shows a browser).
pub fn open(app: &tauri::AppHandle, url: &str, cwd: &str) -> AppResult<()>;
```

**Files** (attachments, TYPE-011 to TYPE-014, TYPE-029):

```rust
#[derive(serde::Serialize)] pub struct FileInfo { pub exists: bool, pub is_dir: bool, pub size: u64, pub mime: String }
pub fn info(path: &str, cwd: &str) -> AppResult<FileInfo>;                   // mime from the infer crate, then the extension
pub fn read(path: &str, cwd: &str, max_bytes: u64) -> AppResult<(String, String)>; // (mime, base64); too_big over max_bytes
#[derive(serde::Serialize)] pub struct FileEntry { pub path: String, pub is_dir: bool } // path relative to cwd, "/" separators
/// The `@` popup's file list (TYPE-026). Inside a Git work tree: `git ls-files --cached --others
/// --exclude-standard` (2 s limit), which applies the ignore files; elsewhere a walk that skips
/// hidden folders. Both stop at max_depth folders deep and max_items entries. Inferred to match
/// the terminal's walker closely enough; the builder compares with `internal/fsext` and notes
/// any difference (for example `.crushignore`) in the piece's report.
pub fn list(cwd: &str, max_depth: u32, max_items: u32) -> AppResult<Vec<FileEntry>>;
```

#### 1.2.9 Windows, flags and state (`main.rs`, `state.rs`, S3)

- **Flags** (PLAN.md:134 and :269; the terminal's flags, `internal/cmd/root.go:37-48`): `--version`, `--probe FILE` (kept, `main.rs:41-45`), `--fixture FILE` (test only, 6.3), `-c`/`--cwd DIR` (relative paths resolve against the process's folder; default: the process's folder), `-s`/`--session ID`, `-C`/`--continue`, `-D`/`--data-dir DIR`, `-d`/`--debug`, `-H`/`--host ADDR`, `-y`/`--yolo[=false]`, `--channels server:name` (repeatable). Their effects are rows START-006 to START-013 and START-021 (section 2.5). A missing folder shows `failed to change directory: <reason>` in the start-up notice (the terminal's text, `internal/cmd/root.go:323`).
- **State.** `AppState { windows: Mutex<HashMap<String /* window label */, Arc<connection::WindowConn>>>, settings: Mutex<settings::Settings>, launch: Launch }`, with `Launch { probe: Option<PathBuf>, fixture: Option<PathBuf>, cwd: PathBuf, session: Option<String>, continue_last: bool, data_dir: Option<String>, debug: bool, host: Option<String>, yolo: Option<bool>, channels: Vec<String>, ui_debug: bool }`. `ui_debug` is `CRUSH_UI_DEBUG == "true"` (PLAN.md:144).
- **Fixture mode.** With `--fixture`, `commands.rs` sends `engine_open`, `engine_retry`, `engine_set_current_session`, `api` and `events_take` to `fixture.rs` instead of `connection.rs` (section 6.3).
- **Closing.** `on_window_event(CloseRequested)` calls `connection::close` for that label. Q2 (what keeps running) is the engine's job (engine design §5.5).
- Phases 5 and 6 open more windows (`crush-desktop --project DIR`, PLAN.md:143). Every command already takes the calling window, so a second window gets its own `WindowConn`, `client_id` and batcher without new commands.

### 1.3 Tauri commands (exact)

All commands are registered in `main.rs` by S3. Arguments are the JS names (Tauri maps camelCase JS keys to snake_case Rust names; the JS wrappers in `src/lib/engine/commands.js` pass them as written here). Every failure is an `AppError` (1.2.2).

| Command | Arguments | Result | Notes |
|---|---|---|---|
| `app_info` | — | `{version, probe, fixture, cwd, session, continue_last, ui_debug, platform}` | `fixture` is a boolean; `platform`: `"linux"` or `"macos"`. Extends today's `{version, probe}` (`main.rs:15-24`) |
| `probe_write` | `json: string` | `null` | unchanged (`main.rs:26-33`) |
| `engine_open` | `cwd: string` | `OpenResult` (1.2.6) | joins; starts the stream |
| `engine_retry` | — | `null` | the "Try again" button of the failed state |
| `engine_set_current_session` | `sessionId: string` | `null` | `""` clears (landing). Remembered for rejoins |
| `events_take` | — | `Entry[]` (1.4.2) | once per animation frame |
| `api` | `method: "GET"\|"POST"\|"PUT"\|"DELETE"`, `path: string`, `query?: object`, `body?: any` | the response JSON (`null` when empty) | see the route list below |
| `settings_get` | — | `Settings` | |
| `settings_set` | `key: "crush_path"\|"yolo"`, `value: any` | `Settings` | |
| `closed_set` | `project: string`, `sessionId: string`, `closed: boolean` | `Settings` | phase 4 |
| `clipboard_write` | `text: string` | `null` | |
| `clipboard_read` | — | `string \| null` | |
| `open_link` | `url: string`, `cwd: string` | `null` | after a click only (section 1.7) |
| `file_info` | `path: string`, `cwd: string` | `FileInfo` | |
| `file_read` | `path: string`, `cwd: string`, `maxBytes: number` | `{mime, base64}` | |
| `files_list` | `cwd: string`, `maxDepth: number`, `maxItems: number` | `FileEntry[]` | the `@` popup; cached per window for 5 s |

**`api` paths.** `path` is either relative to the window's workspace (`"sessions"` means `/v1/workspaces/{id}/sessions`) or absolute (`"/v1/limits"`). Rust always adds `client_id=<this window's id>` to the query: routes that do not use it ignore it, and the ones that do (send, agent init, current session, shell) need it. **The route list** is fixed in `connection.rs`; anything else gives `route_not_allowed`. Phase 3 and 4 routes (method, pattern after `/v1/workspaces/{id}/` unless absolute; `rpc/routes.go` line):

| Method | Pattern | Source |
|---|---|---|
| GET | `/v1/version`, `/v1/engine`, `/v1/limits` | `routes.go:26`; engine design 5.2, 5.9 |
| POST | `/v1/limits/{kind}/refresh` | 5.9 |
| GET | `config`, `providers`, `agent`, `agent/default-small-model`, `git/branch`, `lsps`, `mcp/states`, `skills`, `messages/user` | `routes.go:41,42,95,112,47,78,140,128,46` |
| GET, POST | `sessions` | `routes.go:57-58` |
| GET, PUT, DELETE | `sessions/{sid}` | `routes.go:59-61` |
| GET | `sessions/{sid}/messages`, `sessions/{sid}/messages/user`, `sessions/{sid}/messages/{mid}/review`, `sessions/{sid}/history`, `sessions/{sid}/filetracker/files` | `routes.go:63,65,64,62,71` |
| POST | `sessions/{sid}/kept-skills` | `routes.go:66` |
| POST | `agent` (send, 202), `agent/main` | `routes.go:96,99` |
| GET | `agent/sessions/{sid}`, `agent/sessions/{sid}/prompts/queued`, `agent/sessions/{sid}/prompts/list` (with `details=true`) | `routes.go:100,102,106` |
| POST | `agent/sessions/{sid}/cancel`, `…/interrupt`, `…/prompts/clear`, `…/prompts/recall`, `…/shell` (with `request_id`) | `routes.go:101,109,107,108,111`; 5.9 |
| POST | `config/set`, `config/remove`, `config/model`, `config/compact` | `routes.go:117,118,119,120` |
| GET, POST | `permissions/skip` | `routes.go:86-87` |
| POST | `permissions/grant` (`{permission, action}` → `{resolved}`, `internal/proto/proto.go:208-220`) | `routes.go:88` |
| GET | `project/needs-init` (`{needs_init}`), `project/init-prompt` (`{prompt}`) | `routes.go:48,50`; `internal/proto/requests.go:96-103` |
| POST | `project/init` | `routes.go:49` |
| POST | `skills/read` (`{skill_id}` → `{content, result}`), `mcp/read-resource` (`{name, uri}` → contents) | `routes.go:129,137`; `internal/proto/proto.go:103-111`, `requests.go:161-164` |
| GET | `filetracker/lastread` | `routes.go:73` |
| POST | `sessions/{sid}/branches` (phase 4) | 5.11 |
| GET | `chat-links` (phase 4) | 5.11 |
| GET | `sessions/{sid}/tree`, `sessions/{sid}/tree/messages/{mid}` (phase 4) | 5.6, `engine-design.md:1236-1246` |
| POST | `sessions/{sid}/tree/label`, `…/tree/switch`, `…/tree/navigate`, `…/tree/copy`, `…/files/preview`, `…/files/undo-preview`, `…/files/undo` (phase 4) | same |

The list holds the phase 4 routes from the start, so phase 4 pieces never edit `connection.rs` or `api.js`. Phases 5 and 6 extend the list in `connection.rs` (their own piece edits it then; nothing else changes).

### 1.4 What the window receives

#### 1.4.1 Window events (Tauri `listen`)

| Event | Payload | Sent when |
|---|---|---|
| `events-ready` | none | the batcher went from empty to not empty (1.2.5) |
| `settings-changed` | `Settings` | any settings write, to every window |

Nothing else. Engine data never travels as a window event; it is always pulled with `events_take`, so the frame budget stays in the screen's hands.

#### 1.4.2 Batch entries (`events_take` result)

```js
// one entry per element, in order
{ kind: "event", type: "message", event: "updated", payload: { /* proto.Message */ } }
{ kind: "resync" }
```

`type` is the envelope type (`internal/pubsub/events.go:19-46`, plus `connection`); `event` is `"created" | "updated" | "deleted"`; `payload` is the inner object. This is the serde form of `batch::Entry` that `connection::take` produces: `Entry::Event(e)` → `{kind:"event", type: e.ty, event: <kind lower-case>, payload: e.payload}`, `Entry::Resync` → `{kind:"resync"}`.

### 1.5 Screen side

#### 1.5.1 Files

```
src/main.js                    S3   startZoom(); import "./lib/plugins.js"; mount(App); startProbe(); startEvents()
src/lib/plugins.js             S3   import.meta.glob("./plugins/*.js", { eager: true }): loads every registration file (1.10)
src/App.svelte                 S3   the three columns, the overlay hosts, the engine gate
src/app.css                    S3   adds the tokens of section 4.0 (nothing else edits it)
src/lib/probe.js               S3   probe registry (1.9)
src/lib/keys.js                S3   key dispatcher and the shortcut table (1.8)
src/lib/engine/wire.js         S3   JSDoc wire types (1.5.5), no code
src/lib/engine/commands.js     S3   one async function per Tauri command (names of 1.3)
src/lib/engine/events.js       S3   frame loop, handler registry, resync registry (1.5.4)
src/lib/engine/api.js          P3.3 one function per route (1.5.2)
src/lib/stores/ui.svelte.js    S3   focus, selection, menus, overlays, notices (complete in S3)
src/lib/stores/{connection,sessions,messages,workspace,limits,prompts}.svelte.js   P3.3
src/lib/stores/{composer,queue,todos}.svelte.js                                    P3.7
src/lib/stores/visuals.svelte.js                                                   P3.8
src/lib/stores/{links,closed,tree}.svelte.js                                       P4.1
src/lib/timeline.js            P3.5 pure: messages → timeline items, groups, keys, footers
src/lib/agents.js              P3.3 provider type → agent name ("claude-code" → "Claude", …)
src/lib/render/*               P3.4 Markdown, source map, copy, incremental, math, Mermaid, code, ANSI, diff, sanitize
src/lib/branch.js              P4.1 branch rules (title, quote, selection checks)
src/components/…               owners in section 5
```

#### 1.5.2 `api.js` (P3.3)

One function per route, all returning promises of the engine's JSON. Names are pinned because every UI piece calls them:

```js
export const api = {
  version(), engine(), limits(), refreshLimits(kind),
  config(), providers(), agentInfo(), defaultSmallModel(), gitBranch(), lsps(), mcpStates(), skills(), allUserMessages(),
  listSessions(), createSession(session), getSession(sid), saveSession(session), deleteSession(sid),
  messages(sid), userMessages(sid), review(sid, mid), history(sid), trackedFiles(sid),
  setKeptSkill(sid, {id, name, on, toggleable}),
  send({session_id, prompt, attachments, submission_id, run_id, hidden_user_message, channel}), setMainAgent(agent_id),
  agentSession(sid), queuedCount(sid), queuedSummaries(sid), cancel(sid), interrupt(sid), clearQueue(sid), recallQueued(sid),
  runShell(sid, {command, term_width, is_first_message}, requestId),
  setConfig(scope, key, value), removeConfig(scope, key), setModel(scope, model_type, model), setPermissionsSkip(skip),
  setCompact(enabled), permissionsSkip(), grantPermission(permission, action),   // action: "allow" | "allow_session" | "deny"
  needsInit(), initPrompt(), initProject(), readSkill(skill_id), readMcpResource(name, uri), lastRead(path),
  // phase 4 (built by P3.3 so phase 4 pieces never edit this file):
  createBranch(sid, req), chatLinks(), tree(sid), treeMessage(sid, mid),
  labelTree(sid, req), switchTree(sid, req), navigateTree(sid, req), copyTree(sid, req),
  previewFiles(sid, req), previewUndoFiles(sid), undoFiles(sid, plan_id),
};
export function setCurrentSession(sid) // engine_set_current_session
```

`send` adds a fresh `run_id` (a UUID made in JS with the app's own `uuid()` helper in `api.js`, since `crypto.randomUUID` needs a secure context; inferred that tauri:// counts as one, so the helper falls back to `crypto.getRandomValues`). The 202 answer means accepted; completion comes from `run_complete` with that `run_id` (`proto.RunComplete`, `internal/proto/proto.go:82-90`).

#### 1.5.3 Stores (Svelte 5 runes) and how events apply

Each store is a `.svelte.js` module exporting one `$state` object (read by components) and plain functions (the only way to change it). Pinned exports:

```js
// connection.svelte.js (P3.3)
export const conn = $state({ state: "idle", attempt: 0, error: null, workspace: null, engine: null,
  clientId: "", project: { cwd: "", path: "", dataDir: "", name: "" }, notices: [] });
export async function start();            // app_info → engine_open(cwd) → seeds the other stores
export async function retry();

// sessions.svelte.js (P3.3)
export const sessions = $state({ byId: {}, order: [], currentId: "", loaded: false });
export function upsertSession(s, { fromEvent });   // fromEvent keeps is_busy, attached_clients, has_unfinished_work, pending_prompt
export function removeSession(id);
export async function openChat(id);                 // current-session, load messages, restore model (LIST-035)
export async function newChat();                    // LIST-033: shows the empty chat; the record is made on first send (TYPE-041)
export function busy(id);                           // derived busy (below)

// messages.svelte.js (P3.3)
export const messages = $state({ bySession: {} }); // sid → { ids: [], byId: {}, status: "idle"|"loading"|"ready"|"error", error }
export async function load(sid);                    // GET messages; buffers events meanwhile; replays (LIST-036)
export function applyMessage(event, m);

// workspace.svelte.js (P3.3)
export const ws = $state({ config: null, agent: null, providers: [], lsps: {}, mcps: {}, skills: [], files: {}, branch: "" });

// limits.svelte.js (P3.3)
export const limits = $state({ byKind: {} });       // kind → CLILimits with the newest read_at (engine design 5.9)

// prompts.svelte.js (P3.3; P3.7 draws permission prompts, phase 5 draws question forms)
export const prompts = $state({ permission: null, question: null, pendingByRoot: {} });

// ui.svelte.js (S3, complete)
export const ui = $state({ focus: "composer", selectedItem: null, menu: null, viewer: null, sheet: null,
  help: false, notice: null, panelHidden: false, follow: true });
export function notify(kind, text, ttlMs = 5000);   // SIDE-028: kinds info, success, warn, error, update
export function openMenu(menu); export function closeMenu();
export function openViewer(image); export function openSheet(sheet); export function closeOverlay();
export function setFocus(target); export function selectItem(key);
```

**Applying a batch.** `events.js` hands each entry, in order, to the handlers registered for its `type` (`onEvent(type, fn)`), and `resync` entries (and `connection` events with `resync: true`) to every function registered with `onResync(fn)`. Registration happens at module load of each store, so no store edits another's file. Rules:

- **Order** is the batch order; handlers run synchronously; Svelte batches the DOM update into the same frame.
- **Duplicates.** Every handler is an upsert or delete by key, so applying an event twice gives the same state (engine design §5.12 guarantee 3). A `created` for a known key is an update; a `deleted` for an unknown key does nothing.
- **Protected session fields.** `session` events never carry `is_busy`, `attached_clients`, `has_unfinished_work` or `pending_prompt` (engine design 5.1(a), `engine-design.md:193`). `upsertSession(s, {fromEvent: true})` keeps the stored values of those four. `saveSession` sends back the whole record as last read, as the engine requires (`engine-design.md:2820`).
- **Busy, live.** `busy(id)` is true when the last read said `is_busy`, or this window sent and has no `run_complete` for that `run_id` yet, or the chat has an assistant message without a `finish` part (`IsFinished`, `internal/proto/message.go:303-310`). A `run_complete` for the chat clears the first two; the queue (TYPE-044) can start the next turn, whose messages make it busy again.
- **Loading a chat** (LIST-034, LIST-036): `load(sid)` sets `status: "loading"`, buffers that chat's `message` events, calls `GET sessions/{sid}/messages`, stores the list in its order, then replays the buffer on top with one guard: a buffered event never replaces a message that the snapshot already shows finished. (A finished message gets no later text, so such an event can only be older than the snapshot.)
- **Messages of chats not loaded** are dropped, except that an `updated_at` newer than the session record's bumps the chat's place in the left list.
- **`tree_changed`** for the shown chat reloads its messages (the path may differ, 5.6; this is also what hides and shows branch links, PLAN.md:104).
- **Resync** (`resync` entry or `connection{resync:true}`): every registered resync function runs; while they run, events for the affected stores are buffered and replayed as in "Loading a chat". Phase 3 registers: chat list, the shown chat's messages, queue, limits, config and agent, LSP, MCP, skills, modified files, prompts (`GET agent/sessions/{sid}`). Phase 4 adds chat links. Phase 6 adds helpers, jobs, secure forms, remote and memory through the same `onResync`.
- **Unknown types** are ignored (engine design §0.1 "Apps must ignore unknown fields and event types").

| Event type | Store | Effect |
|---|---|---|
| `connection` | connection | state, notices (SIDE-030); `resync` |
| `session` | sessions | upsert or remove; a removed current chat follows LIST-037 |
| `message` | messages | upsert or remove; busy |
| `file` | workspace | modified files (SIDE-008) |
| `lsp_event`, `mcp_event` | workspace | LSP and MCP rows (SIDE-009, SIDE-010) |
| `skills_event` | workspace | skills (SIDE-011), `$` catalog (TYPE-031) |
| `config_changed` | workspace | re-read config and agent info |
| `agent_event` | messages, ui | `error` → error notice; `summarize` → compaction state (CHAT-029) |
| `run_complete` | sessions, queue | busy off, queue refresh, send settled (TYPE-048) |
| `permission_request`, `permission_notification`, `question_batch_request`, `question_batch_notification`, `prompt_pending` | prompts | stored; permission prompts drawn in the composer slot (P3.7, section 2.7); question forms by phase 5 in the same slot |
| `limits` | limits | newest `read_at` wins per kind |
| `shell_output` | composer | live `!` output (TYPE-020) |
| `tree_changed` | messages | reload the shown chat |
| `chat_link` | links (phase 4) | upsert |

#### 1.5.4 `events.js` (S3)

```js
export function onEvent(type, fn);       // fn(eventKind, payload)
export function onResync(fn);            // fn(): Promise<void>
export function startEvents();           // listen("events-ready") → book one requestAnimationFrame →
                                         // events_take → apply in order → if the batch was not empty, book one more frame
export function withHeld(types, fn);     // runs fn() while events of these types are buffered, then replays them
```

#### 1.5.5 Wire types (`wire.js`, S3; JSON names exactly as the engine sends them)

```js
/** @typedef {{id:string, parent_session_id:string, title:string, message_count:number,
 *   prompt_tokens:number, completion_tokens:number, summary_message_id:string, cost:number,
 *   todos?:Todo[], channel?:string, kept_skills?:KeptSkill[], created_at:number, updated_at:number,
 *   is_busy:boolean, attached_clients:number,
 *   context_budget?:{provider:string, model:string, tokens:number, estimated:boolean},
 *   estimated_usage:boolean, has_unfinished_work:boolean, pending_prompt:""|"permission"|"question"}} Session
 *   (internal/proto/session.go:16-32; the last four lines are engine design 5.1(a)) */
/** @typedef {{content:string, status:string, active_form:string}} Todo        (session.go:52-56) */
/** @typedef {{id:string, name:string, by_agent?:boolean}} KeptSkill           (session.go:35-40) */
/** @typedef {{id:string, role:"assistant"|"user"|"system"|"tool", session_id:string, parts:Part[],
 *   model:string, provider:string, created_at:number, updated_at:number,
 *   activity_at?:number, activity?:string, activity_detail?:string,
 *   prism_model_id?:string, prism_model_name?:string, prism_hypercredit_savings?:number,
 *   prism_dollar_savings?:number, is_summary_message?:boolean, is_compacting?:boolean}} Message
 *   (internal/proto/message.go:24-42; times are Unix seconds) */
/** @typedef {{type:"reasoning", data:{thinking:string, signature:string, started_at?:number, finished_at?:number}}
 *  | {type:"text", data:{text:string, submission_id?:string, hidden?:boolean}}
 *  | {type:"image_url", data:{url:string, detail?:string}}
 *  | {type:"binary", data:{Path:string, MIMEType:string, Data:string}}
 *  | {type:"tool_call", data:{id:string, name:string, input:string, type?:string, finished?:boolean}}
 *  | {type:"tool_result", data:{tool_call_id:string, name:string, content:string, data?:string,
 *        mime_type?:string, metadata:string, is_error:boolean, review?:Review}}
 *  | {type:"finish", data:{reason:"end_turn"|"max_tokens"|"tool_use"|"canceled"|"error"|"content_filter"|"unknown",
 *        time:number, message?:string, details?:string}}
 *  | {type:"shell_command", data:{command:string, output:string, exit_code:number}}} Part
 *   (message.go:95-196 and 519-571). Note: binary parts have no JSON tags, so their keys are
 *   capitalised and Data is base64 (message.go:137-141). */
/** @typedef {{root:string, deletions?:boolean, id?:string, session_id?:string, changes?:Change[],
 *   summary?:{files:number, deletions?:boolean, paths?:string[], copied?:number, checkouts?:number, moved?:number,
 *   generated?:number, removed?:number, no_lines?:boolean, adds?:number, dels?:number, counted?:boolean}}} Review
 *   (internal/filechange/snapshot.go:65-93) */
/** @typedef {{path:string, order?:number, before?:FileState, after?:FileState, transfer?:object}} Change
 *  @typedef {{content?:string, digest?:string, size:number, mode:number, omitted?:string}} FileState (snapshot.go:39-60) */
/** @typedef {{id:string, path:string, yolo?:boolean, debug?:boolean, data_dir?:string, version?:string,
 *   client_id?:string, config?:object, env?:string[], channels?:string[], skills?:object[], protocol?:number}} Workspace
 *   (internal/proto/proto.go:15-34) */
/** @typedef {{is_busy:boolean, is_ready:boolean, model:{id:string, name:string, context_window:number,
 *   can_reason:boolean, reasoning_levels?:string[], default_reasoning_effort?:string, supports_attachments:boolean},
 *   model_cfg:{model:string, provider:string, reasoning_effort?:string, service_tier?:string, ultracode?:boolean, think?:boolean}}} AgentInfo
 *   (proto.go:123-128; catwalk Model fields: catwalk@v0.52.49/pkg/catwalk/provider.go:91-104, where
 *   SupportsImages has the JSON name supports_attachments; SelectedModel: internal/config/config.go:50-69) */
/** @typedef {{session_id:string, prompt:string, submission_id?:string, hidden_user_message?:boolean,
 *   run_id?:string, channel?:string, attachments?:{file_path:string, file_name:string, mime_type:string, content:string}[]}} AgentMessage
 *   (proto.go:149-157; attachment content is base64, message.go:651-656) */
/** @typedef {{submission_id?:string, session_id:string, run_id?:string, message_id:string,
 *   text?:string, error?:string, cancelled?:boolean}} RunComplete (proto.go:82-90) */
/** @typedef {{type:"error"|"response"|"summarize", message:Message, error?:string, run_id?:string,
 *   session_id?:string, session_title?:string, progress?:string, done?:boolean}} AgentEvent (internal/proto/agent.go:29-55) */
/** @typedef {{id:string, session_id:string, tool_call_id:string, tool_name:string, description:string,
 *   action:string, params:any, path:string}} PermissionRequest (internal/proto/permission.go:16-25) */
/** @typedef {{session_id:string, tool_call_id:string, granted:boolean, denied:boolean}} PermissionNotification (permission.go:8-13) */
/** @typedef {{request_id:string, session_id:string, seq:number, chunk:string}} ShellOutput (engine design 5.9) */
/** @typedef {{kind:string, state:"checking"|"ready"|"error", read_at?:string, error?:string,
 *   windows:{name:string, used:number, resets_at?:string, model?:string}[]}} CLILimits (engine design 5.9, engine-design.md:1841-1856) */
/** @typedef {{session_id:string, kind:"permission"|"question", pending:boolean}} PromptPending (engine design 5.1(e)) */
/** @typedef {{message_id:string, highlight_text:string, highlight_start:number, highlight_end:number, title:string}} BranchRequest
 *  @typedef {{id:string, session_id:string, parent_session_id:string, source_message_id:string, highlight_text:string,
 *   highlight_start:number, highlight_end:number, origin:boolean, created_at:number}} ChatLink
 *  @typedef {{session:Session, link:ChatLink, copied_through:string, copied:number}} Branch
 *   (engine design 5.11, engine-design.md:2273-2296) */
/** @typedef {{message:string, code?:string, field?:string}} EngineError (proto.go:37-41) */
```

### 1.6 Long chats: the speed design

The speed bar (PLAN.md:156): a scripted two-minute streamed reply with tool calls, 3 runs; 95 % of frames within 20 ms; typing stays responsive; a frame over 50 ms fails a run if it happens more than once in it, or at the same moment in two runs.

1. **One flush per frame** (1.2.5): the batcher merges a streaming message's many updates into one entry per frame.
2. **Virtual list.** `components/timeline/VirtualList.svelte` (P3.5) renders only the items inside the viewport plus one viewport above and below. Heights: measured with one `ResizeObserver`, cached per item key, content version and column width; unmeasured items use an estimate by kind (user 80, reply 33 per 70 characters plus 50, tool group 44, picture px). An anchor (the first visible item and its offset) keeps the view still when items above change height. **Follow mode** (CHAT-099) keeps the bottom in view while it is on; scrolling up turns it off; reaching the bottom turns it on.
3. **Rendered-item cache.** Each item's rendered DOM is kept by key and content version (an LRU of 300 items), so scrolling back never re-renders Markdown.
4. **Incremental Markdown** (CHAT-018, desktop version PLAN.md:126). `lib/render/incremental.js` keeps a stable prefix per streaming message: it finds the last safe block boundary with the terminal's rules (`internal/ui/chat/markdown_boundary.go:7-39`, including its relaxed cut for long replies without blank lines), keeps the rendered blocks before it, and re-lexes only the tail on each update. A retry that rewrites earlier text (the new text no longer starts with the cached prefix) drops the cache.
5. **Heavy work off the frame.** Code highlighting runs only on closed fences; Mermaid and display math render only when their fence or block is closed, one figure at a time, in idle time (`requestIdleCallback` when present, else `setTimeout(0)` slices of at most 4 ms of work each); a figure being drawn shows its source in a plain code block until then.
6. **Measured, not hoped.** The probe (1.9) records every frame time and every key-to-frame time in test runs; `p3_speed.py` reads them (section 6).

### 1.7 Safe rendering (PLAN.md:273)

The rule: AI replies become page content, and a crafted reply must not be able to press the app's buttons. The content rule (CSP) stays exactly as it is (`src-tauri/tauri.conf.json:21-34`): only the app's own scripts, no inline scripts or handlers, no plug-ins, frames or form posts, pictures only from the app, `asset:`, `data:` and `blob:`.

1. **Markdown never becomes HTML text.** `lib/render/markdown.js` runs `marked`'s lexer (tokens only) and builds DOM nodes itself with `document.createElement` and `textContent`. It never sets `innerHTML` from a reply. Allowed elements: `p h1-h6 ul ol li blockquote pre code a em strong del br hr table thead tbody tr th td span img(from data:/blob: only) input(type=checkbox, disabled, for task lists)`. Attributes it sets: `class`, `href` (checked below), `src` (checked), `alt`, `colspan`, `align` as a class, `data-*` for its own bookkeeping.
2. **Raw HTML in a reply** (`html` tokens, block or inline) is shown as text with every tag removed: DOMPurify `sanitize(html, {ALLOWED_TAGS: [], KEEP_CONTENT: true})`, then inserted with `textContent`. That matches the terminal, whose Markdown renderer runs raw HTML through a strict sanitizer and prints what is left (`glamour/v2@v2.0.1/ansi/elements.go:415-430` in the Go module cache).
3. **Links.** `href` is kept only for `http:`, `https:`, `mailto:` and file-like targets (no scheme, `~/`, `/`, `./`, `../`). Everything else (`javascript:`, `data:`, `vbscript:`, any other scheme) becomes plain text. A link never navigates the window: one click handler on the timeline calls `preventDefault()` and then `open_link` (1.3) for that `href`, and only for a real click (`event.isTrusted`, button 0, no drag, PLAN.md:273 "only after a click"). Keyboard Enter on a focused link counts as a click.
4. **Pictures.** `img` only for `data:image/*` and `blob:` sources (the CSP blocks web pictures anyway). A web picture in a reply becomes a link chip with its address (section 4.4).
5. **KaTeX** with `{trust: false, throwOnError: false, strict: "ignore", maxExpand: 1000, output: "htmlAndMathml"}` renders to a string; that string goes through DOMPurify with its default HTML profile (MathML kept) before it is inserted. `\href`, `\url` and `\includegraphics` stay off because `trust` is false.
6. **Mermaid** with `{startOnLoad: false, securityLevel: "strict", htmlLabels: false, theme: "base", themeVariables: <section 4.3>}`. Its SVG goes through DOMPurify (`USE_PROFILES: {svg: true, svgFilters: true}`) before insertion. If Mermaid needs something the CSP blocks (for example `eval`; inferred not to, but unproved), the figure shows its source in a code block with the line `This diagram could not be drawn here.` and the safety check (section 6) fails, so the problem is caught rather than hidden.
7. **Code highlighting** (`highlight.js`) produces escaped HTML; it is still passed through DOMPurify (`ALLOWED_TAGS: ["span"], ALLOWED_ATTR: ["class"]`) before insertion.
8. **ANSI output** (CHAT-045) is converted by our own `lib/render/ansi.js` into text nodes inside `span` elements with classes; no HTML strings at all.
9. **Every `innerHTML` in the codebase** is inside `lib/render/sanitize.js`, which is the only module allowed to import DOMPurify. A unit test greps the source tree and fails if `innerHTML` or `outerHTML` or `insertAdjacentHTML` appears in any other file.

**Libraries, pinned exactly** (scaffold S3 writes them into `package.json` `dependencies`; all are bundled by Vite into `dist/`, so they work offline):

| Package | Version | Evidence |
|---|---|---|
| `marked` | 18.1.1 | the demo's lock, `~/dev/crush-gui-demos/package-lock.json:2134-2135` |
| `dompurify` | 3.4.17 | same lock, `:1689-1690` |
| `katex` | 0.19.0 | same lock, `:1828-1829` |
| `mermaid` | 12.2.0 | same lock, `:2146-2147` |
| `highlight.js` | 11.11.1 | inferred: no input names a version; the scaffold confirms it installs and builds, and records the version it got in PLAN.md's progress record |

KaTeX's fonts and CSS are imported from `katex/dist/katex.min.css` so Vite copies the fonts into `dist/` (font-src `'self'` allows them, `tauri.conf.json:27`). Mermaid is loaded with a dynamic `import()` at the first diagram, so the first screen does not pay for it.

### 1.8 Keyboard and focus

`lib/keys.js` (S3) has one `keydown` listener on `window` (capture phase), a table of bindings by scope, and `bindKey(scope, combo, handler)` for pieces to attach handlers. Scopes, from most to least specific: `menu`, `overlay` (viewer, sheet, help), `popup` (the `@`/`$` list), `slot` (an inline prompt), `composer`, `timeline`, `panel`, `list`, `global`. The first scope that has the combo and whose handler returns `true` wins. The zoom keys keep their own listener (`src/lib/zoom.js:60-79`), which already runs first.

**The shortcut map** (terminal keys kept where sensible, `internal/ui/model/keys.go:93-341`; "→ phase n" means the key is reserved now and its action comes then):

| Key | Scope | Action | Row |
|---|---|---|---|
| Enter | composer | send | TYPE-004 |
| Shift+Enter, Ctrl+J | composer | new line | TYPE-005 (`keys.go:141-147`) |
| Esc | composer | close popup or delete mode, leave history, else stop the run (busy) or the `!` command | TYPE-046, TYPE-021 |
| Ctrl+Enter | composer, timeline | interrupt and send queued | TYPE-047 (`keys.go:211-214`) |
| Up / Down | composer | history; Up recalls queued prompts | TYPE-017, TYPE-019 |
| Tab | composer | focus the timeline's last item (not with composer_focus_only) | TYPE-022 |
| Shift+Tab | composer, timeline | mode cycle | TYPE-023 |
| Ctrl+Y | global | YOLO toggle | TYPE-024 |
| Ctrl+R | composer | attachment delete mode | TYPE-038 |
| Ctrl+A / Home, Ctrl+E / End | composer | line start / line end (terminal behavior) | TYPE-009 |
| Ctrl+Shift+A, Ctrl+Shift+C, Ctrl+Shift+X | composer | select all, copy, cut | TYPE-009 |
| Ctrl+V | composer | paste (web view paste event: text, image) | TYPE-014 |
| Ctrl+Shift+V | composer | paste text from the clipboard | TYPE-010 |
| Ctrl+G | global | full help | SIDE-027 |
| Ctrl+T, Ctrl+Space | global | show or hide to-dos | SIDE-032 |
| Ctrl+D | global | session details (when the side panel is hidden) | SIDE-003 |
| Ctrl+N | global | new chat | LIST-033 |
| Ctrl+K | global | focus the chat-list search (Sessions search; its results are phase 5) | PLAN.md:103 |
| Ctrl+End | timeline, composer | jump to the bottom and follow | CHAT-098 |
| ↑ ↓, k j | timeline | one line; selection follows | CHAT-096 |
| Shift+↑ ↓, K J | timeline | one item | CHAT-096 |
| u d, b f, PgUp PgDn, g G, Home End | timeline | half page, page, top, bottom | CHAT-096 |
| Space | timeline | expand or collapse the selected item | CHAT-097 |
| c y C Y | timeline | copy the selected item | CHAT-100 (capitals fixed, PLAN.md:194) |
| Esc | timeline | clear the selection (busy: stop the run, as today) | CHAT-100 (hint fixed, PLAN.md:194) |
| Shift+← →, H L | timeline | side-scroll the selected shell or diff | CHAT-043 |
| → / l, ← / h | timeline, panel | focus the side panel / back to the chat | SIDE-012 |
| ← → | tray (to-do and queue open) | switch section, from the composer and the timeline alike | SIDE-033 (fixed, PLAN.md:195) |
| Ctrl+C | global | copy when text is selected; otherwise reserved for quit → phase 5 | DLG-290s |
| Ctrl+P, Ctrl+L / Ctrl+M, Ctrl+S, Ctrl+O, Ctrl+F, Ctrl+X, Ctrl+B, Ctrl+Z | global | → phase 5 (commands, models, sessions, attach) and phase 6 (editor, helpers, background); `keys.go:103-118,137-139,203-206,227-234` | — |
| Ctrl + / Ctrl - / Ctrl 0 | global | zoom (unchanged, `zoom.js:44-58`) | PLAN.md:243 |

Two terminal keys change meaning, both by the plan: **Ctrl+K** is Sessions search (PLAN.md:103), so in the timeline it no longer moves up a line (`keys.go:248-251`; ↑ and k still do) and in the typing box it no longer deletes to the line's end. **Ctrl+C** copies when a selection exists (a desktop app's copy key), and only otherwise reaches phase 5's quit.

**Focus order.** Tab inside the typing box follows the terminal (TYPE-022), so the regions are reached as in the terminal plus the desktop's own ways: the chat-list search (Ctrl+K), its rows (↑ ↓ and Enter inside the list, Esc back to the typing box), the timeline (Tab from the typing box; Tab again back), the side panel (→ from the timeline, Tab back to the typing box, SIDE-012), and every control by mouse. Inside a region, buttons are ordinary tab stops in reading order. Focus is always visible (`src/app.css:126-130`). With `composer_focus_only` (SET-003) clicks and Tab never move focus out of the typing box (PLAN.md:142); clicks in the timeline still select and expand items.

### 1.9 The probe (test runs only)

`lib/probe.js` (S3) keeps today's snapshot (`src/lib/probe.js:24-39`) and adds a registry: `probeSection(name, fn)` where `fn()` returns JSON for that part of the screen. Sections are computed only when the app runs with `--probe`, at the existing 200 ms rhythm (`probe.js:50-52`). Pinned section names and fields (each piece fills its own):

| Section | Owner | Fields |
|---|---|---|
| `connection` | P3.3 | `state, attempt, error, workspace_id, client_id, engine_version, fresh_engine, notices[]` |
| `list` | P3.9 | `rows[{id, title, depth, selected, closed, state, rect}], closed_count, search_hint` |
| `chat` | P3.5 | `id, title, path, busy, follow, status, scroll_top, items[{key, kind, id, rect, expanded, selected, letter}], messages[{id, role, finished, text}], words[{mid, i, text, rect}] (first 400 visible words), selection{text, items[]}, links[{session_id, mid, text, rects[]}], animations` (the count from `document.getAnimations()`) |
| `composer` | P3.7 | `value, focused, mode, placeholder, selection[start,end], attachments[{name, kind}], tray[{kind, text}], slot{kind, buttons[{label, selected, rect}]}` |
| `panel` | P3.8 | `hidden, meters[{id, label, text, warn}], visuals[{id, kind, title, rect}], sections[]` |
| `menu` | P3.5 | `open, items[{label, disabled, hint, rect}]` |
| `overlay` | S3 | `viewer, sheet, help, notice{kind, text}` |
| `frames` | S3 | `count, p95_ms, max_ms, over50_at[]` (ms since `frames_mark`), `input_over50_at[]`, `input_count` |
| `safety` | P3.4 | `inline_blocked, csp_violations, handlers_found, scripts_found, bad_hrefs_found` |

Rects are viewport pixels (`getBoundingClientRect`). The frame meter runs only in probe mode: one `requestAnimationFrame` loop records each frame's time since the last; a `keydown` listener records the time from each key to the next frame. A driver starts a measured window by sending the key `F12` with Ctrl+Alt (`ctrl+alt+F12`), which sets `frames_mark` and clears the counters (probe mode only; no effect otherwise).

Two more probe fields exist for checks that cannot read the screen another way: `clipboard.last` (the last text the app wrote through `clipboard_write`, with its time) and `opened` (the count of lines in `<probe>.opened.jsonl`). The sandbox has no `xclip` or `xsel`, but Python's GTK 3 bindings are in `/usr` (checked: `gi` loads GTK 3.24), so `tests/driver/clip.py` reads and sets the hidden screen's real clipboard (`get-text`, `set-text TEXT`, `set-image FILE`; a setter keeps owning the clipboard for up to 10 s). Copy checks compare both `clipboard.last` and `clip.py get-text`.

### 1.10 Registries that later phases plug into (S3)

Phases 4, 5 and 6 add surfaces without editing phase 3 files. S3 creates these registries with their final exports; each is a plain module with a Svelte `$state` list.

| Module | Exports | Phase 3 users | Later users |
|---|---|---|---|
| `lib/actions.js` | `registerAction({id, title, keys?, enabled(), run()})`, `runAction(id)`, `actions` (list) | new chat, YOLO, to-dos, help, details, copy keys | phase 5's command palette lists `actions`; phase 4 registers Session Tree, Fork from Prompt, Undo Last File Restore |
| `lib/dialogs.js` | `registerDialog(name, component)`, `openDialog(name, props)`, `closeDialog()`, `dialog` (the open one) | `quit` (TYPE-007), `models` (model button), `sessions` (START-021) are opened but registered by phase 5; an unregistered name logs and does nothing | phase 5 dialogs; phase 4 tree and restore dialogs |
| `lib/menu.js` | `registerMenuItem({id, label, order, enabled(ctx), hint(ctx), run(ctx)})`, `menuItems(ctx)` | Copy (order 20), Quote in reply (30), Copy whole message (40) | phase 4: Branch from here (order 10) |
| `lib/decorations.js` | `registerDecorationSource(fn(messageId) → [{kind, start, end, data}])`, `registerDecorationHandler(kind, fn)` | none (empty) | phase 4: branch links |
| `ui.svelte.js` slot | `openSlot({kind, component, props})`, `closeSlot(kind)`, `ui.slot` | permission prompt, plan handoff, Customize Composer | phase 5: question forms, secure entry |
| `lib/stores/{links,closed,tree}.svelte.js` | S3 writes them as stubs with the final exports (empty data) | the list model and the renderer read them | P4.1 fills them |
| `lib/plugins/*.js` | each file registers actions, menu items, dialogs, decoration sources and key handlers when imported; `lib/plugins.js` loads them all | `timeline.js` (P3.5: Copy, Quote in reply, Copy whole message), `composer.js` (P3.7: YOLO, mode cycle, to-dos), `panel.js` (P3.8: help, details), `list.js` (P3.9: new chat) | `branch.js` (P4.1), `tree.js` (P4.4), phase 5 and 6 files; none of them edits `main.js` |

---

## 2. Phase 3, row by row

### 2.0 Reading the tables

Each row gives the desktop behavior, where it lives (component · store; owners in section 5) and its check.

- **Check kinds.** `U` a Vitest unit test (`src/**/*.test.js`, run by `pnpm test`); `R` a Rust test (`cargo test`); `W` a step of a window driver in `desktop/tests/driver/` that drives the real window with xdotool and reads the probe; `T` a step of the two-app driver (the terminal app in a hidden tmux on the same chat); `G` a Go test. Section 6 gives the exact commands.
- **Step names.** Every `W` and `T` check is the driver step named after its row (for example `p3_chat.py` step `CHAT-014`). Each step saves `tests/out/<driver>/<ROW>.png` as the row's proof (PLAN.md:10, "the test name plus a screenshot") and fails the run on its first failed assertion.
- **"Fails when"** names the observation that makes the check fail. Wording checks compare the exact strings given in the row; those strings are the terminal's (cited) unless the row says "desktop text".
- **Scripts.** `script X` means the fake AI server plays `tests/fakes/scripts/X.json` (section 6.2).
- **Ports of terminal logic.** "Port of `file:lines`" means the JS module copies that Go logic rule for rule, and its unit test copies the Go test's cases where one exists. Builders read the cited Go code; the row does not restate every branch.

### 2.1 What phase 3 puts on the screen

```
┌ left column (P3.9) ─────┬ middle column: Conversation.svelte (S3 shell) ──────────────┬ right column (P3.8) ────┐
│ Wordmark                │ ChatHead (P3.9): title; path line                            │ PROJECT + name          │
│ New chat                │ ─ rule ─                                                     │ meters: CONTEXT 5-HOUR  │
│ search (Ctrl K)         │ Timeline (P3.5) on the rail, virtual list:                   │         WEEK            │
│ ─ rule ─                │   UserItem · ReplyItem (P3.4 Markdown) · ToolGroup/ToolItem   │ ─ rule ─                │
│ TODAY                   │   /ShellItem (P3.6) · NoticeItem · BannerItem · FooterItem    │ scrolling body:         │
│ chat rows with state    │   · PendingItem · StatusLine                                  │  VISUALS (figures)      │
│ (branches: phase 4)     │ NoticeLine (P3.8) over the timeline's bottom edge             │  FILES  LSP  MCP        │
│                         │ Tray (P3.7): to-dos, queue (phase 6: helpers, jobs)           │  SKILLS                 │
│ footer (phase 4:        │ ─ rule ─                                                     │                         │
│  "N closed · …")        │ ChipRow · Field [ModeMark | textarea | ModelButton] · Send   │                         │
│                         │   or the Slot (permission, plan handoff, forms)              │                         │
│                         │ MeterStrip (P3.8), only when the right column is hidden      │                         │
└─────────────────────────┴──────────────────────────────────────────────────────────────┴─────────────────────────┘
 Overlays (S3 host): ContextMenu (P3.5), ImageViewer (P3.8), ChangesSheet (P3.6), HelpSheet (P3.8), DetailsOverlay (P3.8), dialogs (phase 5)
 Before a chat or when the engine is not ready: EngineGate, Onboarding, InitPrompt, Landing, Opening (P3.9) take the middle column.
```

S3 splits today's `Conversation.svelte` (319 lines) into `ChatHead.svelte`, `timeline/Timeline.svelte`, `tools/ToolGroup.svelte` and `composer/Composer.svelte` by moving its markup and CSS unchanged, so the phase 2 look scores the same before any piece starts (section 5.1).

### 2.2 CHAT: messages, Markdown, tool rows, diffs, selection, copy, scrolling

| ID | Desktop behavior | Where | Check · fails when |
|---|---|---|---|
| CHAT-001 | Landing and init screens center their content in the middle column (max width `calc(720 * var(--px))`, at least `calc(60 * var(--px))` side padding). A chat's timeline keeps the picture's padding (`Conversation.svelte:133`) | Landing, InitPrompt (P3.9) · Timeline (P3.5) | W `p3_chat`: rects of the landing block and the first chat item · fails when the chat's left inset differs from 66 picture px × zoom by more than 1 px, or the landing block touches the column edge |
| CHAT-002 | Your message sits in the picture's box (1px `--rule-faint`, `Conversation.svelte:177-180`), the desktop version of the band; it spans the timeline width and wraps; no horizontal scroll | UserItem (P3.5) | U `timeline.test.js` "user item"; W script `basic` with a 3,000-character prompt · fails when the item's `scrollWidth` exceeds its `clientWidth` or its width differs from the timeline's |
| CHAT-003 | The selected item gets the selection bar: `calc(2 * var(--px))` of `--ink` at `calc(56 * var(--px))` from the column's left, the item's full height (desktop version of "›"; never vermilion, which the rail uses). One item at most | ItemFrame (P3.5) · `ui.selectedItem` | W click a user message · fails when `chat.items[k].selected` is not true for exactly that item, or the screenshot column at that x has no ink pixels over the item |
| CHAT-004 | Your text as typed: text nodes only, `white-space: pre-wrap`, no Markdown; ANSI codes removed, `\r\n` → `\n`, tabs → 4 spaces, with an offset map back to the stored text (phase 4 branches need it) | UserItem · `render/usertext.js` (P3.4) | U `usertext.test.js` with `<tag>`, `*x*`, `` `x` ``, `\r\n`, tab, `\x1b[31m`; W send `<b>x</b> *y* `` `z` ``⇥t` · fails when any mark is drawn as formatting or the text differs |
| CHAT-005 | Hidden text parts (`hidden: true`) and user messages with only hidden parts are not shown | `timeline.js` (P3.5) | U "hidden" · fails when a hidden part makes an item. W in TYPE-052 (hidden "Implement the plan.") |
| CHAT-006 | Binary and `image_url` parts of a sent message show as chips under its text: icon by kind, name cut to 15 characters with "…", no ✕ | UserItem · `render/Chip.svelte` (P3.4) | W attach two files, send · fails when the sent item lacks two chips or any chip has a remove control |
| CHAT-007 | A click (press and release in place, no drag) on an image chip or a picture opens the image viewer (section 4.5); a web picture's link chip opens the browser after the click | UserItem, ReplyItem → `ui.openViewer` · ImageViewer (P3.8) | W click a chip, then drag from a chip · fails when the click leaves `overlay.viewer` empty or the drag opens it |
| CHAT-008 | A `<loaded_skill>` block shows one line "Loaded Skill → name · description"; broken XML falls back to plain text | `timeline.js` · NoticeItem (P3.5); port of `chat/user.go:87-93,111-119` | U valid and broken XML · fails when broken XML loses text |
| CHAT-009 | Task-result notices "✓ Task NAME finished", "× … failed", "… stopped", "● Still running: NAME" with drawn icons, one line with "…", aligned with replies, no box | NoticeItem; port of `chat/user.go:79-85,138-160,194-200,247-254` | U every state and the cut · fails when a word or icon differs. W comes with phase 6 jobs |
| CHAT-010 | Question-answer notice "✓ Your answers to the questions: answered", or the cancelled icon | NoticeItem; port of `chat/user.go:140-146` | U answered and cancelled. W with phase 5 question forms |
| CHAT-011 | System notices "◐ teach turned on by the agent" / "○ … turned off …", skill name in ink at weight 600; text without " turned " is a plain line; one line | NoticeItem; port of `chat/user.go:162-175` | U cases from `system_notice_test.go:14-29` · fails when a case differs |
| CHAT-012 | `c` or `y` on a selected user message copies its text; notice "Message copied to clipboard". The menu's "Copy whole message" does the same | `lib/copy.js` (P3.5) · `clipboard_write` | W select, press `c` · fails when `clipboard.last` differs from the prompt or the notice text differs |
| CHAT-013 | `shell_command` parts of a user message show as shell items after it, also after reopening | `timeline.js` · ShellItem (P3.6) | W run `!printf 'a\nb'`, restart the app, reopen · fails when the shell item is missing or its output differs |
| CHAT-014 | Replies as rich Markdown through the safe renderer (1.7): headings, bullet and numbered lists, task boxes, links, quotes with a left hairline, highlighted code blocks; tables, display math, Mermaid and pictures become visuals chips (brief 14, section 4.3). Prose width `calc(540 * var(--px))` as built (`Conversation.svelte:175`); code, diffs and tool rows use the full timeline width | ReplyItem · `render/markdown.js` (P3.4) | U `markdown.test.js` DOM shape per block type; W script `markdown` · fails when any block shows as raw Markdown or raw HTML, or a paragraph is wider than 540 picture px |
| CHAT-015 | The byline (author in `--t-author`, time in `--t-stamp`, `Conversation.svelte:153-168`) heads each reply: the desktop version of the reply bullet. Not on plan cards' inner text | ReplyItem | W script `basic` · fails when a reply lacks its byline or a plan card's body repeats it |
| CHAT-016 | A selected reply shows the selection bar of CHAT-003 | ItemFrame | W click a reply · same as CHAT-003 |
| CHAT-017 | Inline code: `--face-mono` at 0.94 em in `--code-inline`, no backticks, no box or padding. Copy brings the backticks back (CHAT-088) | `render/markdown.js` | U inline code; U `copy.test.js` with the cases of `inlinecode_copy_test.go:16-45` · fails when backticks show or copy loses them |
| CHAT-018 | Incremental Markdown with a stable prefix (1.6 item 4); a retry that rewrites earlier text drops the cache | `render/incremental.js` (P3.4) | U retry rewrite, a 2 KB reply with no blank line, a closed then reopened fence; W `p3_speed` · fails when the output differs from a full render of the same text |
| CHAT-019 | Under an unfinished reply: the activity mark and "Thinking", "Responding", "Writing command", "Waiting for the model" or "Compacting conversation"; "Responding" only within 5 s of activity; gone once tools start or a later step takes over | StatusLine, ActivityMark · `timeline.js`; port of `chat/assistant.go:217-226,609-615`, `assistant_render.go:176-196` | U each word with a fake clock; W script `basic` with `delay_ms` · fails when the probe's status text order differs from the script's phases |
| CHAT-020 | Retry words "Rate limited — waiting to retry", "Waiting to retry"; gone after the activity changes or a cancel | StatusLine; port of `assistant_render.go:198-207` | U the cases of `retry_status_test.go:12-38` · fails when a case differs. W script `errors` step `rate_limit` (fake server answers 429 once) |
| CHAT-021 | Elapsed time after the status ("12s", "3m 4s", "1h 2m"), hidden under 10 s, reset when the activity changes; heartbeats don't reset it. One shared 1 s clock for all visible timers | `lib/timefmt.js`, StatusLine; port of `chat/activity_timer.go:8-37` | U 9 s, 10 s, 61 s, 3,661 s, heartbeat · fails when a text differs |
| CHAT-022 | Thought peek: " · " plus the newest thinking line in italic slate after "Thinking"; Markdown marks removed; `activity_detail` used first; empty once finished; off when `options.tui.thought_line` is false | `lib/thought.js`, StatusLine; port of `chat/thought_line.go:13-63` | U marks, detail first, finished; W with the setting off · fails when text shows while the setting is off |
| CHAT-023 | Thinking text is never drawn; no "Thought for" footer; empty thinking-only or tool-only steps make no item; a step reappears when it later gets reply text | `timeline.js`; port of `chat/assistant.go:147-149,366-369,696-699,753-760`, `messages.go:514-521` | U the cases of `assistant_reasoning_visibility_test.go:19-61`; W script `basic` (thinking deltas) · fails when any reasoning text reaches the DOM (probe word search) |
| CHAT-024 | The unused thinking box is not built: no surface shows reasoning (the terminal never shows it either) | — | U `timeline.test.js` asserts no item kind carries reasoning text · fails when one does |
| CHAT-025 | "Canceled" in italic slate after a stopped reply; not when the step called tools; shown on this window's own Esc before the engine confirms | BannerItem (P3.5) | W script `basic` long reply, Esc mid-stream · fails when the banner is missing within 2 frames of the key, or appears under a tool step |
| CHAT-026 | On interrupt, this window marks the run's unfinished tools cancelled and stops its live status at once; later updates for that run reopen nothing unless they carry a `finish` part; interrupted tool-only steps are hidden | `messages.svelte.js` (P3.3) · `timeline.js`; port of `chat.go:568-594`, `immediate_cancel.go:1-24` | U late chunks after interrupt; W Ctrl+Enter mid-tool · fails when a cancelled tool shows pending again |
| CHAT-027 | Error banner: small-caps "ERROR" in `--error-ink`, a one-line title in ink ending in "…" when cut, wrapped details in slate | BannerItem; port of `assistant_render.go:233-252` | W script `errors` (fake server answers 500) · fails when the title wraps, the tag is missing, or details are cut |
| CHAT-028 | Refusal banner: "REFUSED", "Model refused to continue" and the fixed advice (`chat/assistant.go:32-41`) when the provider sends none; provider text wins | BannerItem | W script `errors` step `content_filter` · fails when a text differs |
| CHAT-029 | Compaction: "Compacting conversation…" with the activity mark and timer, then "Conversation compacted", "Compaction canceled" or an error banner; an earlier compacting step makes the live step and group say "Compacting" | BannerItem, StatusLine; port of `chat/assistant.go:228-242,516-529`, `assistant_render.go:215-231` | W the driver posts `agent/sessions/{sid}/summarize` with `curl --unix-socket` · fails when the probe never shows the three texts in order |
| CHAT-030 | `c`/`y` on a selected reply copies the visible text: no Markdown marks, code verbatim, plan markers and reasoning left out, HTML blocks dropped, links copy their label. "Copy whole message" in the menu copies the same text | `lib/copy.js copyVisible`; port of `chat/copy_source.go:15-85` | U the Go cases; W · fails when `clipboard.last` differs |
| CHAT-031 | The final plan in a square box (rounded cards are banned): 1px `--rule-control` with + corner marks, a small-caps "PLAN" label set into the top rule, padding `calc(17 * var(--px)) calc(20 * var(--px))`, no fill | ReplyItem plan variant (P3.5); markers by `render/plan.js` (P3.4) | W script `plan` · fails when the box is missing, rounded (any `border-radius`), or filled |
| CHAT-032 | While a plan streams in plan mode, the same box is drawn without its bottom rule until the ready marker; replies without the start marker get no box; old finished messages unchanged | ReplyItem | W script `plan` with `delay_ms` · fails when the open box shows a bottom rule before the marker |
| CHAT-033 | Plan markers never show; a fence that only wraps the marker is removed; a real fence just above it stays (offset map kept) | `render/plan.js`; port of `common/common.go:65-118` | U the cases of the Go code · fails when a marker is visible |
| CHAT-034 | Plan headings: H1 and H2 plain (no "#"), H3-H5 indented `calc(16 * var(--px))`, inline code at weight 600; a narrow box drops its padding to 0 | ReplyItem plan CSS | U DOM classes; W narrow window · fails when indentation or padding differs |
| CHAT-035 | Footer "◇ Model via Provider in 12.3s" (diamond drawn as a line icon) in `--t-small` slate under the turn's final reply; every turn for Prism-routed models; "Unknown Model" when not in config; duration from your last message; removed when a retry resets the stream | FooterItem · `timeline.js`; port of `chat/messages.go:287-426` | U cases; W script `basic` · fails when the text differs from the port's output for the engine's messages |
| CHAT-036 | Prism: "Model → RoutedModel" and "◆ 12 Saved" or "• $0.42 Saved"; no decimals at 1 or more, one below | FooterItem; port of `chat/messages.go:292-303,400-458` | U the rounding cases · fails when a case differs |
| CHAT-037 | No footer where it would label nothing (after a prompt, another footer or a compaction marker) | `timeline.js`; port of `chat/messages.go:305-316` | U · fails when such a footer is built |
| CHAT-038 | Shell item: 1px ink left rule, header "$ command" in mono at weight 500, output in mono slate; multi-line commands flattened to one line; selection bar when selected | ShellItem (P3.6) | W `!echo hi` · fails when the header or output text differs from the engine's part |
| CHAT-039 | Running shell: "Running…" after the header, output streaming from `shell_output` events (5.9) in `seq` order, the activity mark under the latest line; no output yet shows only the mark | ShellItem · `composer.svelte.js` shell state (P3.7) | W `!for i in 1 2 3; do echo $i; sleep 0.3; done` · fails when the probe never shows a partial output before the end |
| CHAT-040 | Exit marks: "(Canceled)" for 130 in slate, "(exit N)" in `--error-ink` for other non-zero; Esc while pending sets 130 | ShellItem; port of `chat/shell.go:105-109,233-237` | W `!exit 3`, then `!sleep 5` + Esc · fails when the marks differ |
| CHAT-041 | Long output: first 10 lines and "… N more lines" when done; last 10 and "… N earlier lines" while streaming; trailing resets and spaces trimmed first; exactly 10 lines shows all | `lib/tools/shell.js`; port of `chat/shell.go:22-25,264-318` | U 10, 11, 25 lines, streaming; W `!seq 1 25` · fails when a count or text differs |
| CHAT-042 | Click (CHAT-101 rule) or Space toggles the full output | ShellItem | W · fails when the line count does not toggle between 10 and 25 |
| CHAT-043 | Long lines never wrap; Shift+←/H and Shift+→/L scroll 5 characters (`5ch`), horizontal wheel too; a "…" mark at the left edge while scrolled; clamped to the longest line | ShellItem | W `!printf '%0300d\n' 0`, Shift+→ twice · fails when `scrollLeft` is not 10 `ch` or the mark is missing |
| CHAT-044 | `c`/`y` copies "$ cmd\n<output>" without colors; notice "Shell output copied to clipboard" | `lib/tools/copy.js` (P3.6) | W · fails when `clipboard.last` differs or carries escape codes |
| CHAT-045 | Program colors through `render/ansi.js`: the 16 colors map to `--ansi-0`…`--ansi-15` classes; 256 and true colors set through the element's style object (CSSOM, allowed by the CSP); cursor moves and clears removed; `\r` redraws collapsed | `render/ansi.js` (P3.4); port of `common/ansi16.go:11-207` (RemapANSI16 :19, StripCursorControl :126, simulateCarriageReturns :199) | U git and npm progress bars, trailing `\x1b[0m`, 256 and true color; W script `tools` (bash prints colors) · fails when a class or the collapsed text differs |
| CHAT-046 | Tool header: status mark, display name at weight 500, main parameter in mono, other `key=value` pairs in slate brackets, shown only when 30 or more characters of width remain; cut with "…" | ToolHeader (P3.6) · `lib/tools/registry.js`; port of `chat/tool_header.go:84-176` | U names and parameters per tool; W at a narrow window · fails when pairs show without 30 characters of room |
| CHAT-047 | Status marks: pending = the small activity mark (ink; the terminal's soft green is not used, section 4.11); done = check icon; error = cross in `--error-ink`; cancelled = dot in slate; a result's error or success overrides the stored status | ToolHeader · `lib/tools/status.js`; port of `chat/tool_header.go:69-82`, `tools.go:356-365` | U status table; W script `tools` · fails when a mark differs from the table |
| CHAT-048 | Pending tool: name plus the activity mark; cancelled stops it | ToolHeader | W · fails when a cancelled tool still animates (`document.getAnimations()` count in the probe) |
| CHAT-049 | Early state lines in slate: "Requesting permission...", "Waiting for tool response...", "Canceled." | ToolBody; port of `chat/tool_header.go:31-48` | U each; W in `p3_permissions` · fails when a text differs |
| CHAT-050 | Error line: "ERROR" in `--error-ink` plus the message; "User denied permission" and "User cancelled" get "WARN" in `--amber-ink`; long errors cut with "…" | ToolBody; port of `chat/tool_header.go:50-67` | U; W deny in `p3_permissions` · fails when the tag or color differs |
| CHAT-051 | "Invalid parameters" or "Invalid tool name" when the input does not decode; lenient tools (LSP, diagnostics) draw anyway | `lib/tools/registry.js`; port of `chat/tool_standard.go:33-36`, `mcp.go:35-38` | U bad JSON per tool · fails when a strict tool draws |
| CHAT-052 | Body collapsed to 10 lines plus "… (N lines hidden) [click or space to expand]"; never hides one line (11 lines shows all) | ToolBody; port of `chat/tools.go:17-31`, `tool_output.go:16-49` | U 10, 11, 12 lines · fails when a count or text differs |
| CHAT-053 | Expanded: the full body; long header parameters wrap under the first parameter (hanging indent), highlight kept | ToolHeader, ToolBody | W Space on a tool with a long command · fails when wrapped lines start left of the first parameter |
| CHAT-054 | A selected tool shows the selection bar; tools nested in a group or an agent tree have no inner padding | ItemFrame, ToolItem | W · fails when nested tools carry padding |
| CHAT-055 | Hook lines above a tool: "Hook name [matcher] → OK / Rewrote Output / Denied reason", aligned, long paths cut on the left ("…/x.sh"), deny colors the label, newlines as "¶"; hidden in nested tools | HookLines (P3.6); port of `chat/tool_hooks.go:15-179` | U samples; W with a PreToolUse hook in the test project's config (format from `internal/hooks`) · fails when a line differs |
| CHAT-056 | `c`/`y` on a tool copies Markdown: "## Name Tool Call", "### Parameters:", "### Result:" / "### Error:" / "### Status: Pending..." / "Cancelled", with the per-tool formats; notice "Tool content copied to clipboard" | `lib/tools/copy.js`; port of `chat/tool_copy.go:16-543`, `tool_registry.go:44-163` | U one case per tool kind; W · fails when `clipboard.last` differs |
| CHAT-057 | Code bodies (View, Write, Fetch, JSON and Markdown results, Definition, Symbols) highlighted with a line-number gutter (mono, slate, right-aligned, width from the last number); long lines cut with "…"; copy gives full lines | ToolBody code · `render/code.js` (P3.4) | U offsets and gutter width; W View with `offset` · fails when numbers don't start at offset + 1 |
| CHAT-058 | Picture results: "Loaded Image → image/png 12.3 KB" (size estimated from the base64 length) plus a thumbnail at most `calc(96 * var(--px))` tall that opens the viewer | ToolBody image | U size; W View of a PNG · fails when the size text differs or the click opens nothing |
| CHAT-059 | Generic results: JSON re-indented, unified diffs drawn as diffs, Markdown-looking text as Markdown, else plain; JSON that fails to re-indent stays as is | `lib/tools/sniff.js`; port of `chat/tool_result_content.go:23-59`, `unified_diff.go:18-188` | U cases · fails when a case picks another kind |
| CHAT-060 | Bash: highlighted command, a leading `cd <project> && ` hidden, full wrap when not nested, output body; "(No output)" hides the body; unparsable input "failed to parse command" | bodies/Bash (P3.6); port of `chat/bash.go:45-110`, `common/bash.go:8-14` | U; W script `tools` · fails when the prefix shows |
| CHAT-061 | "background=true" in the header when the command ran in or moved to the background | bodies/Bash; port of `chat/bash.go:78-81,112-125` | U. W with phase 6 (Ctrl+B) |
| CHAT-062 | Job rows "● Job (Start/Output/Kill) PID id description"; description hidden under 10 characters; Start shows "Command: …" and output | bodies/Job; port of `chat/bash.go:63-69,131-277` | U. W with phase 6 jobs |
| CHAT-063 | View: short path, limit and offset, highlighted file with real line numbers; skill files "Loaded Skill → …"; pictures "Loaded Image"; an empty file shows the header only | bodies/File; port of `chat/file.go:39-98` | U; W script `tools` · fails when a variant differs |
| CHAT-064 | Write: path and highlighted content; denied: the error and a diff of the proposed change; an error without diff data shows only the error | bodies/File; port of `chat/file.go:125-167` | U; W deny in `p3_permissions` · fails when the denied diff is missing |
| CHAT-065 | Edit: path and diff at the full timeline width; error above the diff; no empty diff when nothing changed | bodies/Edit, DiffView | W script `tools` · fails when the diff box is narrower than the timeline or an empty diff is drawn |
| CHAT-066 | Multi-Edit: "edits=N", the diff, "Note: X of Y edits succeeded" on partial failure, the cut message above the diff | bodies/Edit; port of `chat/file.go:269-323`, `tool_output.go:212-251` | W script `tools` step `multiedit_partial` · fails when the note differs |
| CHAT-067 | Download: URL, file_path, timeout, plain body | bodies/Download; port of `chat/file.go:350-368` | W download from the fake server (`http://127.0.0.1:<port>/file.txt`) · fails when a field is missing |
| CHAT-068 | Glob, Grep (include, literal=true), List (path, "." default), Sourcegraph (count, context), plain results | bodies/Search; port of `chat/search.go:39-186` | U all four; W glob, grep, ls (Sourcegraph needs the web: unit only) · fails when parameters differ |
| CHAT-069 | Fetch: URL, format, timeout; body highlighted as txt, html or md | bodies/Fetch; port of `chat/fetch.go:38-68` | W fetch from the fake server · fails when the format is ignored |
| CHAT-070 | Web Fetch ("Fetch") and Web Search ("Search") bodies as quiet Markdown | bodies/Fetch; port of `chat/fetch.go:99-150`, `tool_output.go:301-342` | U (needs the web) |
| CHAT-071 | To-dos: "2/5 · current task"; "created N todos[, starting first]"; "completed N[, starting next]" / "completed all" / "starting task"; list done → in progress → pending (✓, •) | bodies/Todos; port of `chat/todos.go:40-192` | U; W script `tools` step `todos` · fails when a text differs |
| CHAT-072 | Question tool row: header = first question (cut at 60, or 40 plus "(+N more)"); body "✓ question answer" with "╰ note" lines; Yes, No, Skipped, joined choices, collapsed free text | bodies/Question; port of `chat/question.go:36-269` | U. W with phase 5 question forms |
| CHAT-073 | MCP tool: "Server → Tool", JSON parameters, sniffed result; a name without 3 parts → "Invalid tool name" | bodies/Mcp; port of `chat/mcp.go:33-78` | W with `tests/fakes/fake_mcp.py` tool `echo` · fails when the header differs |
| CHAT-074 | Docker MCP: "Docker MCP → Find/Add/Remove/Exec/Config Set/Code Mode", Add in `--diff-add-ink`, Remove in `--error-ink`, Find as a server table (10 max, "... and N more", "No MCP servers found.") | bodies/DockerMcp; port of `chat/docker_mcp.go:38-271` | U (no Docker in the sandbox) |
| CHAT-075 | Diagnostics: "project" or the file; plain body | bodies/Diagnostics; port of `chat/diagnostics.go:40-52` | U |
| CHAT-076 | LSP tools: Find References, Find Definition, Rename Symbol "a → b", Replace Symbol (diff, plain without metadata), Call Hierarchy, List Symbols, Restart LSP | bodies/Lsp; ports of `chat/references.go`, `definition.go`, `rename.go`, `replace_symbol.go`, `call_hierarchy.go`, `symbols.go`, `lsp_restart.go` | U |
| CHAT-077 | Unknown tools: "my_tool" → "My tool", JSON parameters, sniffed result, pictures | bodies/Generic; port of `chat/generic.go:32-73` | U |
| CHAT-078 | Agent: "Agent" header, small-caps "TASK" tag and the prompt (one line until expanded), nested tool lines on a hairline tree (`--figure`, like the list's branch tree), loaded from the helper chat 8 at a time and recursively, the activity mark until a result, then the Markdown result; live helper events update the nested tools | bodies/Agent, AgentTree (P3.6) · `messages.svelte.js` loads child chats; port of `chat/agent.go:25-206`, `model/session_load.go:401-476` | W script `agent` · fails when nested tools are missing or the tree shows before results stream |
| CHAT-079 | Agentic Fetch: like Agent with a "PROMPT" tag and optional URL | bodies/Agent; port of `chat/agent.go:212-374` | W script `agent` step `agentic_fetch` (local URL) · fails when the tag differs |
| CHAT-080 | Runs of tool calls (and thinking-only steps) fold into one group row "Ran N tools · …" (PLAN.md:147 wording) that keeps its open state by group ID (its first step's message ID) for the window's life; Question, Agent and Agentic Fetch never fold; a run of only thinking does not group | `timeline.js` groups (P3.5) · ToolGroup (P3.6); port of `model/chat.go:311-455`, `chat/toolgroup.go:17-130` | U grouping cases; W script `tools` · fails when a group splits, merges across a reply, or forgets its open state after new events |
| CHAT-081 | Group status: the activity mark while working; a cross in `--error-ink` when the last tool failed; a gear when it went to the background; no mark when done (the picture shows none) | ToolGroup; port of `toolgroup_header.go:85-105,260-261` | U; W script `tools` step `last_fails` · fails when an earlier failure with a good last tool shows the cross |
| CHAT-082 | Extras " · N backgrounded", " · N failed" (the count in `--error-ink`) | ToolGroup; port of `toolgroup_header.go:42-47` | U |
| CHAT-083 | After the dot: the changed-files summary ("edited a.go +3 −1", "moved 3 files", "4 files changed", "removed …", "checked out / copied / generated", "Some files were deleted"; names shortened in the middle to 24 characters; only +/− while working); when no file changed, past-tense verbs from the tools ("read session.go, ran a command"; section 4.6 lists the verbs) | `lib/tools/summary.js` (P3.6); port of `toolgroup_header.go:107-258`, `chat/changes.go:14-118`, `review_inputs.go:34-101` | U saved summaries, expired diffs, ignored paths, `delete_file`; W · fails when the summary differs from the port's output |
| CHAT-084 | The group row opens on click (brief: "opens on click"); a "Changes" text button at the row's right end, shown only when the group changed files, opens the Changes sheet (DLG-294). The sheet's loading title "… · Loading…" and the expired text "This saved diff has expired. Full diffs are kept for the latest five prompts; the chat is still here." are the terminal's | ToolGroup · ChangesSheet (P3.6) · `api.review` | W click the row, then "Changes" · fails when the row click opens the sheet, or "Changes" shows on a group without file changes |
| CHAT-085 | While the last group works, the live status replaces the summary: the activity mark, "Running a command", "Reading a file", "Editing a file", "Searching", "Searching the web", "Reading a web page", "Updating the plan", "Checking a command", "Checking code", "Using <server>", "Waiting for subagent", "Waiting for approval", "Thinking", "Writing command", retry words, "Compacting conversation", "Working", the timer and the thought peek; the timer restarts per tool call | ToolGroup · `lib/tools/status.js`; port of `toolgroup.go:293-370,460-473`, `toolgroup_header.go:155-171,296-306` | U each word; W script `tools` with `delay_ms` · fails when a word differs |
| CHAT-086 | An open group lists its last 5 tools and "… N earlier" (a text button that shows the rest); each tool is compact until opened; clicking a tool opens that tool | ToolGroup | W a group of 7 · fails when 5 rows and "… 2 earlier" are not shown first |
| CHAT-087 | Native text selection across items; WebKit scrolls the timeline while a drag passes its top or bottom edge; the virtual list keeps every item between the selection's ends mounted; `::selection` in `--selection`; not while a slot form has focus | Timeline, VirtualList | W press on a word, drag past the bottom edge, release · fails when the selection stops at an item edge or the view does not scroll |
| CHAT-088 | Copying a selection rebuilds the Markdown source through the renderer's source map (backticks, indentation, tabs, real newlines; soft wraps add nothing); whole inline spans bring their marks | `lib/copy.js copySelection` · `render/sourcemap.js` | U the cases of `list/source_copy_test.go:11-47`; W select across a code span and a list, Ctrl+C · fails when `clipboard.last` differs from the expected source |
| CHAT-089 | Not needed: DOM text has no wrapped rows to rejoin; the desktop version is the native selection (PLAN.md:124) | `lib/copy.js` | U a wrapped paragraph copies with no inserted newline · fails when one is inserted |
| CHAT-090 | Copies go through `clipboard_write` (Rust, 1.2.8); success shows the row's notice; a failure shows "Failed to copy to clipboard" | `lib/copy.js` | U with the command failing; W · fails when the failure text differs |
| CHAT-091 | After a mouse selection ends in the timeline, wait 400 ms (the double-click interval), copy it as in CHAT-088 and show "Selected text copied to clipboard". The selection stays (the terminal clears it) so the right-click menu can still act on it (PLAN.md:99) | Timeline · `lib/copy.js` | W drag-select, wait 600 ms · fails when `clipboard.last` is not set or the selection is cleared |
| CHAT-092 | The virtual list scrolls by line, to top and bottom, and to the selected item; an item taller than the view still counts as at the bottom when its end is shown | VirtualList · `lib/vlist.js` | U anchor math; W `p3_long` · fails when End leaves the last item's bottom more than 1 px off the view's bottom |
| CHAT-093 | Native wheel scrolling; a sideways wheel over shell output scrolls it; after a wheel scroll the selection moves to the nearest visible item when it left the view; over a slot form the form scrolls first | Timeline, ShellItem | W `xdotool click 5` ×10 · fails when `scrollTop` does not grow or the selection stays out of view |
| CHAT-094 | A thin styled scrollbar (`::-webkit-scrollbar`, `calc(6 * var(--px))`, thumb `--rule-control`, no track): `default` shows it while scrolling and 2 s after, `always`, `never` (`options.tui.scrollbar`, SET-006) | Timeline CSS · `ws.config` | W each mode · fails when the thumb is visible 3 s after a scroll in `default` |
| CHAT-095 | The scrollbar takes presses even while hidden (an invisible thumb, not `display: none`); a track press jumps; drag scrolls; release anywhere ends; it stays visible while dragging | Timeline CSS | W press at the right edge and drag · fails when `scrollTop` does not change |
| CHAT-096 | Timeline keys: ↑/k and ↓/j scroll one reading line and the selection follows the edge item; Shift+↑/K and Shift+↓/J move one item; u/d half a page; b/PgUp and f/PgDn a page; g/Home top; G/End bottom. Ctrl+K and Ctrl+J are not line keys here (1.8) | `lib/keys.js` timeline scope (S3) · Timeline handlers (P3.5) | W each key · fails when `scrollTop` or `selectedItem` does not change as the table says |
| CHAT-097 | Space opens or closes the selected item and keeps following the bottom | Timeline | W · fails when follow mode turns off |
| CHAT-098 | Ctrl+End jumps to the bottom and follows; only with a chat open | Timeline | W · fails when `chat.follow` is false after the key |
| CHAT-099 | Follow mode: new output keeps the bottom in view; scrolling up stops it; reaching the bottom restarts it; a growing item re-anchors | VirtualList | W script `stream2m`: scroll up 3 notches, wait, End · fails when the view moves while not following, or the bottom drifts while following |
| CHAT-100 | `c`/`y` and also `C`/`Y` copy the selected message, tool or shell (the three notices of the row); Shift+←/→ side-scroll keys; Esc clears the selection; Esc with nothing selected while busy stops the run. Help shows "c/y copy" and "esc clear selection" (both quirks fixed, PLAN.md:194) | `lib/keys.js` · Timeline | W `C` on a reply; Esc with a selection while busy · fails when `C` copies nothing or that Esc stops the run |
| CHAT-101 | A click selects an item; a single click (no drag, no second click within 400 ms) opens or closes an expandable item; double-click selects a word and triple-click a line (native) | Timeline | W one click, double click, triple click · fails when a double click toggles the item |
| CHAT-102 | Links open only after a real click (1.7): `http`, `https`, `mailto` in the browser; file links (relative to the project folder, `~`, `:line:col` dropped when no such file) with the desktop opener; others refused with "cannot open links of type …", "cannot open a file on another computer", "link does not name a local file"; a drag cancels | Timeline click handler · `open_link` → `opener.rs` | R `opener::classify` cases from `links.go:19-93`; W click each kind · fails when `<probe>.opened.jsonl` gains a line without a click, or a refusal text differs |
| CHAT-103 | The activity mark (section 4.10) is a CSS animation: only mounted, visible marks run (the virtual list unmounts the rest); a reloaded idle chat shows no mark; `prefers-reduced-motion` stops it | ActivityMark | W idle chat after reload · fails when `document.getAnimations().length` is above 0 |
| CHAT-104 | Two mark sizes: `line` (replies and tools, `calc(24 * var(--px))` wide) and `small` (groups, `calc(12 * var(--px))`) | ActivityMark | U props · fails when a group uses `line` |
| CHAT-105 | Opening a long chat draws the bottom view in the first frame and measures the rest lazily; the scrollbar shows after the first full estimate | VirtualList | W `p3_long` open a 500-item chat with the frame meter on · fails when a frame over 50 ms happens more than once |
| CHAT-106 | MCP channel pushes appear as turns the engine makes (the terminal skips its own handling when the server routes them, `agent_events.go:67-113`; inferred that the engine routes them in shared mode) | `messages.svelte.js` | W `fake_mcp.py` channel push with `--channels fake:demo` · fails when no turn appears |
| CHAT-107 | Unified diff: two line-number columns, "+"/"−", hunk header "@@ -a,b +c,d @@", context; empty diff draws nothing; CRLF → LF; tabs → 4 spaces | DiffView (P3.6) · `lib/diff.js` (own Myers line diff, no new library) | U against the Go fixtures in `diffview/` tests · fails when a hunk differs |
| CHAT-108 | Side by side when the diff's box is at least 120 mono characters wide (measured with `ch` of the diff font); deletes paired with the next inserts; blank cells for missing lines; an odd width gives the right side the extra pixel | DiffView; port of `diffview/split.go:21-73` | U pairing; W wide and narrow window · fails when the switch happens at another width |
| CHAT-109 | Diff colors: tinted bands `--diff-add-bg`/`--diff-del-bg` with darker gutters `--diff-add-gutter`/`--diff-del-gutter` (section 4.0); themes may set their own (phase 5) | DiffView CSS | W screenshot pixel check on one added line · fails when the gutter is not darker than the code band |
| CHAT-110 | Diff code highlighted by language from the file name (else `highlightAuto` on at most 20 KB); control characters shown as ␀-style symbols | DiffView · `render/code.js` | U NUL and DEL; W · fails when a control byte reaches the DOM raw |
| CHAT-111 | A file-name header row per file in a multi-file diff | DiffView | U · fails when a header is missing |
| CHAT-112 | DiffView options: height cap with a "…" row, line numbers on or off, x and y offsets with a left "…" mark (used by the Changes sheet) | DiffView props | U · fails when the last line exactly at the cap shows "…" |
| CHAT-113 | A diff in the chat starts at 10 lines plus "… (N lines hidden) [click or space to expand]" | DiffView in bodies/Edit | W · fails when more than 10 lines show before expanding |

### 2.3 TYPE: typing box, mentions, skills, attachments, sending, queue, interrupt, held prompts, inline forms

Engine calls for modes: plan and code switch the main agent with `POST agent/main` (`ws.AgentSetMain(config.AgentPlan or AgentCoder)`, `internal/ui/model/input_mode.go:62-80`); YOLO is `POST permissions/skip`. Both are project-wide in the engine, so they change the other app too (engine design, `engine-design.md:1525`; risk R7).

| ID | Desktop behavior | Where | Check · fails when |
|---|---|---|---|
| TYPE-001 | The textarea grows from 1 to 15 lines (`--t-composer`, 26 px lines), then scrolls; the composer area grows upward and a following timeline stays at its bottom; focus shows the vermilion hairline (`Conversation.svelte:283-286`), the desktop version of the band color | Field (P3.7) | W type 20 lines · fails when the field is not exactly 15 lines tall, or the timeline's bottom item moves out of view |
| TYPE-002 | One mode mark at the field's left: none in normal mode (the picture); plan = small-caps "PLAN"; YOLO = the amber warning icon (the meter's icon); bang = "!" in mono at weight 700. Bang beats plan, plan beats YOLO. The text starts `calc(8 * var(--px))` after the mark | ModeMark (P3.7) · `composer.svelte.js` mode | W each mode · fails when `composer.mode` and the mark disagree, or two marks show |
| TYPE-003 | The terminal's placeholders exactly (`model/composer.go:82-95`): bang "Run a shell command"; YOLO and not plan "Go crazy"; busy a random working text; plan "Let's plan"; ready a random ready text; a new random pick after each send. The picture's "Ask Claude…" was sample text (decision D4) | Field · `lib/composer/modes.js` | U the order of the rules; W YOLO on, plan, bang, busy · fails when `composer.placeholder` differs |
| TYPE-004 | Enter sends; empty text with no attachments does nothing, except reopening a pending plan handoff (TYPE-052) | Composer · `composer.svelte.js send()` | W Enter on empty · fails when anything is sent (fake AI log gains a request) |
| TYPE-005 | Shift+Enter and Ctrl+J insert a new line; the help says "shift+enter" | Field keys | W · fails when either sends |
| TYPE-006 | A trailing `\` then Enter replaces the `\` with a new line | Field keys; port of `keypress.go:334-338` | W · fails when it sends or the `\` stays |
| TYPE-007 | `exit` or `quit` (trimmed) then Enter calls `openDialog("quit")`; phase 5 registers the quit dialog | `composer.svelte.js` · `lib/dialogs.js` | U the match; W with phase 5 · fails when the text is sent to the AI |
| TYPE-008 | Enter while a mode switch is in flight shows "Switching input mode, one moment..." and keeps the text | `composer.svelte.js` | U with a slow fake `agent/main` · fails when it sends |
| TYPE-009 | Ctrl+A and Home go to the line start, Ctrl+E and End to the line end (terminal keys); Ctrl+Shift+A selects all; Ctrl+Shift+C copies ("Selection copied to clipboard") and Ctrl+Shift+X cuts ("Selection cut to clipboard"), only with a selection; Ctrl+C with a selection copies too | Field keys | W each key · fails when Ctrl+A selects all or a notice differs |
| TYPE-010 | Ctrl+Shift+V pastes text from `clipboard_read`, through the paste rules below; empty or non-text clipboard: "Clipboard is empty or does not contain text" | Field · `lib/composer/paste.js` | U paste routing; W after an app copy · fails when the text differs |
| TYPE-011 | A paste over 10 lines or 1,000 columns becomes an attachment `paste_N.txt` (N counts up per chat; `\r\n` normalized); over 5 MB: "Paste is too big (>5mb)" | `lib/composer/paste.js`; port of `paste.go:21-25,54-85,122-136,261-276` | U 10 vs 11 lines, 1,000 vs 1,001 columns, 5 MB; W · fails when the chip name or rule differs |
| TYPE-012 | Pasted file paths attach when all exist and are pictures (else they paste as text); a model without pictures: the warning; a folder: "Cannot attach a directory"; over 5 MB: "File is too big (>5mb)". Dropping files on the window follows the same rules (Tauri drag-drop event; GUI version of the row) | `paste.js` · `file_info`, `file_read` | U each case; W paste two PNG paths · fails when a case differs |
| TYPE-013 | A paste starting with `!` (leading spaces allowed) enters bang mode | `paste.js` | U · fails when the mode stays |
| TYPE-014 | Ctrl+V with a picture on the clipboard attaches `paste_N.png` (only when the model takes pictures); clipboard text that is a picture path attaches that file; errors "File too large, max 5MB", "File type is not a supported image format", "Unable to read file: …"; empty clipboard is silent | Field `paste` event (`clipboardData.items`) · `paste.js` | W `clip.py set-image tests/fixtures/dot.png`, Ctrl+V · fails when no `paste_1.png` chip appears; a text-only model shows the warning instead |
| TYPE-015 | Moves to phase 6: "Open in editor" (Ctrl+O) runs `$VISUAL`/`$EDITOR` in a terminal tab (PLAN.md:131), and terminal tabs are phase 6 work. Phase 3 keeps Ctrl+O reserved | — | checked in phase 6 (contradiction C1) |
| TYPE-016 | Native caret placement and drag selection; a selection turns off `$skill` tinting (TYPE-032) | Field | W click and drag · fails when the caret index differs from the click point |
| TYPE-017 | History: ↑ at the start (or empty) goes older (the first ↑ only moves to the first line's start); ↓ at the end goes newer, then back to the draft; Esc restores the draft; this chat's prompts (all chats' when it has none), newest 200; skips hidden and task-result prompts; shell commands as `!cmd` | `lib/composer/history.js` · `api.userMessages`, `api.allUserMessages`; port of `history.go:16-96,183-282` | U the cases; W send 3, ↑ ↑ ↓ ↓ · fails when an entry or the draft differs |
| TYPE-018 | A history entry starting with `!` switches to bang mode | `history.js` | U · fails when the mark doesn't follow |
| TYPE-019 | ↑ at the start while busy or with a queue recalls queued prompts (`prompts/recall`) with their attachments; several join with a blank line; in bang mode "Queued message recovered. Press Up outside shell mode to edit it."; errors shown; 10 s limit | `history.js` · `queue.svelte.js` (P3.7) | W queue two while busy, ↑ · fails when the field differs from the two joined |
| TYPE-020 | Bang mode: `!` at the start enters; Backspace on empty leaves; Enter runs `POST …/shell` with a fresh `request_id` (no AI); the shell item streams `shell_output`; with no chat, the chat is created first; cancelled = exit 130; errors "shell: …" | `composer.svelte.js runShell` · ShellItem | W `!echo hi` on the landing page · fails when no chat is created or the item is missing |
| TYPE-021 | Esc stops a running shell command (it counts as busy) through the same stop route as a reply (TYPE-046) | `composer.svelte.js` | W `!sleep 5`, Esc · fails when the item does not end with "(Canceled)" |
| TYPE-022 | Tab moves focus to the timeline and selects the last item; not on the landing page; not with `composer_focus_only` | Field keys · `ui.setFocus` | W · fails when focus moves with the setting on |
| TYPE-023 | Shift+Tab cycles code → plan → code+YOLO (turns YOLO on) → code (turns YOLO off only if the cycle turned it on); busy: "Agent is busy, please wait before switching input mode..."; a failed switch keeps the old mode; banners per TYPE-025 | `lib/composer/modes.js`; port of `input_mode.go:16-36,62-123` | U the cycle and its flag; W four presses · fails when the mode order or a banner differs |
| TYPE-024 | Ctrl+Y toggles YOLO ("Skip permission prompts. System level commands will be blocked." / "Yolo mode disabled"); in plan mode it switches to YOLO coding; it pins YOLO against the cycle. It changes the engine's setting only, as the terminal's runtime toggle does; the desktop setting `yolo` stays the launch default | `modes.js` · `api.setPermissionsSkip`; port of `input_mode.go:38-54,125-134` | W · fails when a banner differs or the setting file changes |
| TYPE-025 | Banners as notices (SIDE-028): kind plan "Plan with Crush before generating any code.", kind yolo with the YOLO text, info "input mode: code" | NoticeLine (P3.8) | W within TYPE-023 · fails when a kind or text differs |
| TYPE-026 | `@` at the start or after a space opens the popup: project files (Rust `files_list`, depth and count from `options.tui.completions`) plus MCP resources as "server/title" (from `mcp/states`; inferred that the states carry resources, the builder confirms); filter on the word after `@`; ranking exact name > name prefix > path segment > fuzzy; closes on a space, moving before the `@`, or a non-mention word | Popup · `lib/composer/mentions.js`; port of `mention.go:12-140`, `completions.go:228-310,431-456` | U ranking and closing; W type `@rea` · fails when README.md is not first |
| TYPE-027 | Popup: up to 10 rows of `calc(32 * var(--px))`, width between 10 and 100 characters, above the caret (measured with a mirror element), kept inside the window, matched letters at weight 600, the focused row on `--band` | Popup | W at the window's right edge · fails when the popup leaves the window |
| TYPE-028 | Popup keys: ↑/↓ wrap; Enter, Tab and Ctrl+Y pick; Ctrl+N and Ctrl+P insert the next or previous and stay open; Esc closes; while open, Ctrl+N and Ctrl+Y don't reach new chat or YOLO | `lib/keys.js` popup scope | W each key · fails when Ctrl+N opens a new chat while the popup is open |
| TYPE-029 | Picking a file puts its path and a space in place of `@query` and attaches it (`file_read` → base64); skipped when already read and unchanged in this chat (`filetracker/lastread`); before a chat exists, de-duplicated; a picture on a model without pictures: the warning; an unreadable file is left for the AI | `mentions.js`; port of `editor.go:277-368` | U each case; W · fails when the chip is added twice |
| TYPE-030 | Picking an MCP resource inserts its title or URI and attaches its content (`mcp/read-resource`); empty content adds nothing; MIME falls back to `text/plain`; a picture on a model without pictures: the warning | `mentions.js`; port of `editor.go:370-428` | W `fake_mcp.py` resource · fails when the chip's content differs |
| TYPE-031 | `$` at the start or after a space (not in bang mode, at least one skill) opens the popup with skill names; it closes as soon as no name contains the text (`$HOME` closes, so Enter sends); a pick inserts `$name `. The terminal code is uncommitted and not on this branch (contradiction C2); the checklist text is the spec | Popup · `lib/composer/skillwords.js` · `ws.skills` | U the closing rule; W with a test skill · fails when `$HOME` keeps it open |
| TYPE-032 | `$name` words that name a skill get a `--skill-tint` background through a backdrop element behind the transparent-background textarea (a textarea cannot color words); not in bang mode; not while text is selected; trailing `. : / - _` are not part of the name | Field backdrop · `skillwords.js` | U word rules; W type `$demo.` · fails when the dot is tinted |
| TYPE-033 | On send, each `$name` skill not yet attached is attached once, the same way the palette's skill attachment does today (inferred from `dialog_actions.go:101-102`; `skills/read` gives the content); read failures are only logged; `$5` and `$HOME` are ignored | `composer.svelte.js send()` | W send `use $demo` · fails when the AI request (fake AI log) lacks the skill or has it twice |
| TYPE-034 | The skill list reloads on start and on `skills_event`; the popup redraws after the load | `workspace.svelte.js` (P3.3) | W add a skill folder during the run · fails when the popup lacks it after the event |
| TYPE-035 | The chip row sits above the field under the rule: icon (picture, skill, text), name cut to 15 characters, ✕; overflow "N more…"; very narrow: names trimmed to at least 5 characters. Kept skills (SIDE-026) follow the attachments in the same row | ChipRow (P3.7) | W 8 attachments in a narrow window · fails when chips wrap to a second line instead of "N more…" |
| TYPE-036 | Clicking ✕ removes that chip; ignored in delete mode | ChipRow | W · fails when another chip is removed |
| TYPE-037 | A click (no drag) on a picture chip opens the image viewer (PLAN.md:128) | ChipRow → `ui.openViewer` | W · fails when the viewer stays closed |
| TYPE-038 | Ctrl+R: delete mode numbers the chips 0-9; a digit removes that chip; `r` removes all; Esc leaves; other keys are eaten | ChipRow · `lib/keys.js` | W · fails when a letter reaches the field during delete mode |
| TYPE-039 | The help sheet shows the attachment keys while chips exist | HelpSheet (P3.8) · `lib/keys.js` table | W Ctrl+G with and without chips · fails when the entries don't follow |
| TYPE-040 | Pictures attach only when the agent's model has `supports_attachments` | `paste.js`, `mentions.js` · `ws.agent` | U · fails when a picture attaches to a text-only model |
| TYPE-041 | The first send on the landing page creates "New Session" (`POST sessions`), saves skills turned on before the chat (`kept-skills`), records read files, then sends; a create error shows and keeps the text; port of `send.go:33-148` | `composer.svelte.js` · `sessions.svelte.js` | W send on landing · fails when the chat list lacks the new chat or the prompt is lost on error |
| TYPE-042 | The sent prompt shows at once (a pending item keyed by `submission_id`) with a "Working" status; the engine's saved copy replaces it in place without a jump | PendingItem (P3.5) · `messages.svelte.js` | W · fails when the item's top moves by more than 1 px at the swap |
| TYPE-043 | With the agent not ready (`is_ready` false), sending shows the engine's `ErrAgentNotInitialized` text as an error notice and keeps the text | `composer.svelte.js` | U with a fake `agentInfo` · fails when it sends |
| TYPE-044 | A prompt sent while busy is held: it shows only in the tray's queue and joins the chat when its turn starts; an interrupt releases it | Tray QueueSection · `queue.svelte.js` | W send twice during a stream · fails when the second shows in the timeline before its turn |
| TYPE-045 | Queue rows in the tray: slate, one line each; the first "waiting · sends next"; task results relabeled "Result of task X", "Progress of X", "Your answers to the questions"; hidden while a slot form is open; duplicates already in the chat hidden | QueueSection; port of `pills.go:87-130`, `queue_display.go:7-66` | U labels; W · fails when the first row's tag differs |
| TYPE-046 | Esc while busy stops the run the way the terminal does (`send.go:243-316`: interrupt when prompts are queued so the queue runs next, otherwise its stop path), after closing the popup, delete mode or history first; the help says "esc cancel" or "esc interrupt, send queued" | `composer.svelte.js stop()` · `api.interrupt`/`api.cancel` | W with and without a queue · fails when the queue is lost by an Esc with prompts queued |
| TYPE-047 | Ctrl+Enter interrupts and sends the queue; key repeat ignored; works while an interrupt is in flight | `composer.svelte.js` | W hold Ctrl+Enter · fails when more than one interrupt is sent (engine log via the fake AI request count) |
| TYPE-048 | A failed send: cancelled removes the pending item; a definite failure (an engine error answer) puts the text and attachments back in the field with the error; a `transport` error keeps the pending item with "This message may not have been sent" (desktop text) | `composer.svelte.js` · `messages.svelte.js`; port of `update_agent.go:121-155`, `pending_prompt.go:82-148` | U each error code; W stop the engine mid-send (`p3_reconnect`) · fails when the text is lost |
| TYPE-049 | A prompt sent while another chat opens waits for that chat and goes to it; on failure it returns to the field; shell: "Shell command not run: the conversation did not open" | `sessions.svelte.js openChat` · `composer.svelte.js` | U with a slow fake open · fails when it goes to the old chat |
| TYPE-050 | The tray's queue header has a "Clear" text button (`prompts/clear`), the desktop answer to the row's open question (decision D9) | QueueSection | W · fails when the queue is not empty afterwards |
| TYPE-051 | Paste goes to the focused element (native); a paste while the timeline has focus does nothing | Field | W · fails when a paste with the timeline focused changes the field |
| TYPE-052 | When a plan run ends with the ready marker, the slot shows the "ready to code" prompt (default Start coding); confirming switches mode and sends hidden "Implement the plan."; Enter on an empty field reopens it while pending; dismissing keeps it pending; a new prompt or a chat switch clears it; ignored while a secure form is open | PlanHandoff (P3.7) in the slot; port of `input_mode.go:136-219` | W script `plan` · fails when the slot does not open, or the hidden prompt shows in the timeline |
| TYPE-053 | Three buttons: Start coding, Code with YOLO, Revise plan; ←/→/H/L move and wrap; Enter confirms; buttons stack in a narrow column | PlanHandoff; port of `plan_handoff_inline.go:171,252` | W · fails when the order or wrap differs |
| TYPE-054 | Start coding (C, Shift+C, click): code mode, permission skipping turned off if needed, the chat continues | PlanHandoff; port of `input_mode.go:195` | W · fails when YOLO stays on |
| TYPE-055 | Code with YOLO (Y, Shift+Y, click): code mode with permission skipping on | PlanHandoff | W · fails when YOLO is off afterwards |
| TYPE-056 | Revise plan (N, Shift+N): "What should change?" field, 1,000 characters, 3 to 8 visible lines | PlanHandoff | W · fails when the limit or line range differs |
| TYPE-057 | Feedback: Enter sends the trimmed text to the planning agent; Shift+Enter and Ctrl+J add lines; whitespace only stays open | PlanHandoff | W · fails when whitespace sends |
| TYPE-058 | Esc in the feedback returns to the buttons and keeps the draft; Esc on the buttons gives focus back to the field without rejecting the plan | PlanHandoff | W · fails when the draft is lost or the plan is cleared |
| TYPE-059 | Buttons act on one click; drag in the feedback selects and places the caret; the selection clears on a key, paste, resize or blur | PlanHandoff | W · fails when a click needs a second press |
| TYPE-060 | With focus elsewhere the handoff stays shown with inactive buttons (slate); hover styles only while focused | PlanHandoff | W click the timeline · fails when the slot disappears |
| TYPE-061 | The `@`/`$` popup also takes the mouse (GUI version of the row): a click on a row picks it, the wheel scrolls it | Popup | W click the second row · fails when nothing is picked |

### 2.4 SIDE: right panel, meters, status, notices, help, to-dos, context, limits, model display

Q6 (PLAN.md:27) places the terminal's footer and sidebar facts: model and effort in the typing box's model button; context and limit meters at the top of the right panel, or in the meter strip under the typing box while that panel is hidden. The exact terminal words (`composer_footer.go:251-336`) appear in the meters' detail popover.

| ID | Desktop behavior | Where | Check · fails when |
|---|---|---|---|
| SIDE-001 | The three columns as built (list, conversation, panel; `App.svelte:94-105`, `lib/columns.js`); the composer is as wide as the timeline; the tray sits above the composer rule | App (S3), Conversation (S3) | W at 1600 × 1000 · fails when the field's left or right edge differs from the timeline's content edges by more than 1 px |
| SIDE-002 | The right panel hides when keeping it would push the conversation under `middleMin` (460 picture px, `src/lib/columns.js:9`) with both side columns at their minimum, or when the window is under 640 picture px tall (the desktop measure for "<120 columns or <30 rows"; decision D11); `compact_mode` forces it hidden (`POST config/compact` saves it); focus in the panel moves to the field. The chat head stays (it is not the terminal's header) | App · `ws.config` · MeterStrip (P3.8) | W narrow the window with `xdotool windowsize`; set `compact_mode` · fails when the panel stays, the strip is missing, or focus is lost |
| SIDE-003 | Ctrl+D while the panel is hidden opens the details overlay below the chat head: title, model info, version (right), then Modified Files, LSPs, MCPs, Skills side by side; typing in the field closes it; at most 20 rows per section | DetailsOverlay (P3.8) | W hidden panel, Ctrl+D, type a letter · fails when it doesn't open or doesn't close on typing |
| SIDE-004 | No desktop surface: the terminal draws this header only where compact mode is always off (`layout.go:51-54`), so it never shows | — | none needed; recorded as dead terminal code |
| SIDE-005 | The right panel: PROJECT and the project name, the meters, then a scrolling body with VISUALS, FILES, LSP, MCP, SKILLS. The chat title is in the chat head, the folder and branch in its path line, the logo in the left column, the model in the model button (decision D12) | SidePanel (P3.8) | W · fails when a section is missing or the body doesn't scroll with 30 skills |
| SIDE-006 | The model button shows "Model · effort" (picture: "Opus 5.5 · max"); its hover and focus detail shows provider, "Thinking", "Thinking On", "Thinking Off" or "Reasoning <effort>", "· FAST", "· Ultracode", context used and cost (with "~" when estimated); "Unknown" until the agent is ready. Clicking it opens `openDialog("models")` (phase 5) | ModelButton (P3.7) · `ws.agent`, `sessions` | U the label from `AgentInfo`; W hover · fails when a word differs from the port of `sidebar.go:25-86` |
| SIDE-007 | The 5-HOUR and WEEK meters show used percent; the amber warning icon when less than 20% is left; the detail shows "5h N% left · resets 2h41m"; "checking…" until the first `limits` reading (5.9); refreshed on `limits` events and on model change | Meters · `limits.svelte.js` (P3.3) | W with a fake CLI limits reading · fails when the warning shows at 20% left or more |
| SIDE-008 | FILES: relative paths with "+N" / "−N", "…and N more", "None"; unchanged files left out; reloaded on `file` events | FilesSection · `lib/modified.js` (P3.8) · `api.history`; port of `session.go:66-73,151-299` | U; W after an edit tool · fails when the counts differ from the diff |
| SIDE-009 | LSP rows: offline icon when unstarted or stopped, "starting...", ready with error, warning, hint and info counts, "error: …", "disabled", "None" (there is no LSP dialog); refreshed on `lsp_event` plus a 5 s backstop | LspSection · `ws.lsps`; port of `lsp.go:19-201` | W with `tests/fakes/fake_lsp.py` (one error, one warning) · fails when the counts differ |
| SIDE-010 | MCP rows: "starting...", connected with "N tools / N prompts / N resources" and "channel", "error: …", "needs authentication", "disabled", offline; "Docker MCP" named so; a failed read keeps the old rows | McpSection · `ws.mcps`; port of `mcp.go:20-262` | W with `fake_mcp.py` (and one broken server) · fails when a state text differs |
| SIDE-011 | SKILLS: active and built-in skills minus disabled ones, sorted, "…and N more", "None" | SkillsSection · `ws.skills`; port of `skills.go:16-143` | U sort and cut · fails when the order differs |
| SIDE-012 | → or l from the timeline, or a click, focuses the panel body; ↑/↓ scroll 4 lines; g/Home, G/End; ← or h back to the timeline; Tab to the field; the wheel; the scrollbar shows while focused or 2 s after a wheel; with `composer_focus_only` the wheel works on hover | SidePanel · `lib/keys.js` panel scope | W each key · fails when focus or `scrollTop` differs |
| SIDE-013 | The Git branch is read every 5 s while the window is visible (`git/branch`). Hyper credits (60 s) need a route the engine does not have; they wait for phase 5's Hyper sign-in work (open question O3) | `workspace.svelte.js` | W `git checkout -b x` in the test project · fails when the path line lacks "x" within 7 s |
| SIDE-014 | The footer's facts are split per Q6: model and settings in the model button; tokens, context, 5h, weekly and other limits in the meters (panel) or the strip (panel hidden); the detail popover gives the terminal's words ("~12.3k/200k tokens", "Context N% left" or "Context unknown", "5h N% left", "Weekly N% left", "resets …", joined with " · ") | Meters, MeterStrip, MeterDetail · `lib/footer.js` (P3.8); port of `composer_footer.go:26-55,95-139,250-349` | U the texts with "~" and unknowns; W hover · fails when a text differs |
| SIDE-015 | Context percent uses the chat's context budget for native-compaction CLIs, else the fallback limit (the smaller of the configured limit and the window minus a buffer of window/5, or 20,000 above 200,000; default limit 400,000); no telemetry shows "Context unknown" | `lib/compaction.js`; port of `composer_footer.go:183-207`, `config/autocompact.go:11-33` | U each kind · fails when a value differs |
| SIDE-016 | Customize Composer opens in the slot from the meters' detail popover ("Customize…", desktop entry; the palette entry comes in phase 5): 8 checkboxes, Space toggles, Enter saves ("Composer updated"), Esc cancels; nothing picked saves an explicit "none"; it refuses to open over another slot form | CustomizeForm (P3.8) · `api.setConfig` `options.tui.composer_footer` | W toggle two, Enter · fails when the config differs or the notice is missing |
| SIDE-017 | "Model name" controls the model name in the model button | CustomizeForm · ModelButton | W off · fails when the name still shows |
| SIDE-018 | "Reasoning, fast and Ultracode" controls the effort and flags in the model button | same | W off · fails when "· max" still shows |
| SIDE-019 | "Tokens used" controls the token count in the detail and the strip | same | W off · fails when tokens still show |
| SIDE-020 | "Context left" controls the CONTEXT meter | same · Meters | W off · fails when the meter still shows |
| SIDE-021 | "5-hour usage left" controls the 5-HOUR meter | same | W off · fails when the meter still shows |
| SIDE-022 | "Weekly usage left" controls the WEEK meter | same | W off · fails when the meter still shows |
| SIDE-023 | "Other usage limits" controls extra meters for other windows a CLI reports | same | U a third window · fails when it shows while off |
| SIDE-024 | "When usage limits reset" (off by default) adds "resets …" under each limit meter | same | W on · fails when the reset text is missing |
| SIDE-025 | PLAN or YOLO (plan wins) is the field's mode mark (TYPE-002); the Remote badge joins it in phase 6 through ModeMark's `badges` list | ModeMark | as TYPE-002 |
| SIDE-026 | Kept skills show as chips after the attachments (small-caps "SKILL" before the name) with ✕; ✕ calls `kept-skills` with `on: false`; always clickable, because notices never cover the chip row here | ChipRow (P3.7) · `sessions` | W turn a skill on (palette later; here by `curl` to `kept-skills`), click ✕ · fails when the chat still keeps it |
| SIDE-027 | Ctrl+G opens the help sheet over the timeline's bottom, above the composer; Ctrl+G or Esc closes it; nothing below moves | HelpSheet (P3.8) | W · fails when the field moves |
| SIDE-028 | Notices: kinds info, success, warn, error, update, plan, yolo; a line icon and color per kind (error `--error-ink`, warn `--amber-ink`); one line cut with "…"; laid over the bottom of the timeline above the composer rule (nothing moves); 5 s by default, longer TTLs as the terminal's; a newer notice replaces the older; errors also go to the app log | NoticeLine (P3.8) · `ui.notify` | U TTLs; W two quick notices · fails when both show or the timeline shifts |
| SIDE-029 | The help sheet's entries follow the state and focus, from `lib/keys.js`: slot form; init (quit); chat (run keys, Tab, Shift+Tab, commands, models, sessions, YOLO, new chat, Ctrl+End, Ctrl+B, then field, panel and timeline rows, tray keys); landing. The Tab entry says "focus chat" or "focus editor" and is hidden with `composer_focus_only` | HelpSheet · `lib/keys.js`; port of `help.go:8-241` | U entries per state; W · fails when an entry differs |
| SIDE-030 | Connection notices from `connection` events: "Lost connection to the Crush server — reconnecting…" (30 s), "Can't restore the connection to the Crush server. Restart Crush to recover." (1 min, with a "Try again" button, desktop addition), "Reconnected to the Crush server.", then the chat reloads (also one being opened mid-switch) | NoticeLine · `connection.svelte.js` | W `p3_reconnect` · fails when a text or the reload is missing |
| SIDE-031 | Tray to-do header "TO-DO 1/4"; closed it also shows the current step (activity mark and active form); open it lists the to-dos (✓ done, • in progress, ○ pending); hint "ctrl+t show/hide"; hidden while a slot form is open; an extra row when chips exist | Tray TodoSection · `todos.svelte.js` (P3.7); port of `pills.go:16-85,247-308` | W script `tools` step `todos` · fails when the count differs |
| SIDE-032 | Ctrl+T and Ctrl+Space show or hide the to-dos (only with unfinished ones); it opens by itself once per chat when the conversation column is at least 900 picture px tall (the desktop measure for 40 rows) | TodoSection · actions `todos.toggle` | W · fails when it toggles with no unfinished to-dos |
| SIDE-033 | With both to-dos and queue in the tray, ← and → switch sections while the tray has focus (the tray is a tab stop between the timeline and the field), so they never fight the panel focus keys (fix of the half-working hint, PLAN.md:195) | Tray · `lib/keys.js` tray scope | W focus the tray, → · fails when the section doesn't change or focus jumps to the panel |
| SIDE-034 | The to-do activity mark runs while the agent works on a step | TodoSection | W · fails when it runs while idle |
| SIDE-035 | The landing page's help shows the full help's entries only (no "tab change focus") | HelpSheet | U landing entries · fails when the Tab entry shows |

### 2.5 START: start-up, flags, onboarding, init, landing (PLAN.md:292 gives them to this design)

| ID | Desktop behavior | Where | Check · fails when |
|---|---|---|---|
| START-001 | No usable model (agent not ready and no configured provider): the middle column shows onboarding: the wordmark, then phase 5's model picker opened with `openDialog("models", {onboarding: true})` and the text "To start…"; only quit works; closing the picker reopens it | Onboarding (P3.9) · `lib/dialogs.js` | checked in phase 5 with the model picker (it needs that dialog) |
| START-002 | `GET project/needs-init` true: the init prompt "Would you like to initialize this project?", the folder path, the text naming the `initialize_as` file, hint "You can also initialize anytime via Ctrl+P.", buttons "Yep!" / "Nope"; Enter = chosen button, ←/→/Tab switch, y/Y yes, n/N/Esc no. Yes: new chat, send the `project/init-prompt` text, `POST project/init`. No: landing and `POST project/init`. Errors "Failed to initialize project: …", "Failed to mark project as initialized: …" (15 s) | InitPrompt (P3.9); port of `onboarding.go:17-119` | W fresh project, `y` · fails when the init prompt isn't sent or the prompt shows again on restart |
| START-003 | Landing (no chat open): the wordmark at `calc(64 * var(--px))`, the folder and Git branch, the model line, and three columns LSPs / MCPs / Skills (the panel's row parts, "…and N more"); the composer below at full width; the right panel stays | Landing (P3.9) | W start with no chat · fails when a column is missing or rows overflow without "…and N more" |
| START-004 | The CRUSH AI wordmark (the built one, `ChatList.svelte:12,83-96`; drawn type, PLAN.md:130) with "Charm™ · v<version>" under it on the landing page; "HYPERCRUSH" when the provider is Hyper; no stretched letter (terminal glyph art) | Wordmark (P3.9) | W · fails when the version differs from `app_info` |
| START-005 | No animation: the owner's wordmark is set in iron black on the paper (PLAN.md:244), so the terminal's gradient drift and shine do not apply (contradiction C3) | Wordmark | W `document.getAnimations()` on landing is 0 |
| START-006 | `crush-desktop -s ID` opens that chat after joining; `-C` / `--continue` opens the most recent chat; both together exit 1 before any window with "if any flags in the group [session continue] are set none of the others can be" (cobra's text for `MarkFlagsMutuallyExclusive`, `internal/cmd/root.go:47`) | `main.rs` flags (S3) · `sessions.svelte.js` | W launch with `-s <id>` · fails when another chat opens |
| START-007 | After joining, the stores load in parallel and each may fail alone: chats, config, agent, providers, LSPs, MCP states, skills, limits, Git branch (phase 6 adds update checks, memory and process polls) | `connection.svelte.js start()` | W with a broken MCP server · fails when any other section stays empty |
| START-008 | `-c` / `--cwd DIR`: relative to the process's folder; a missing folder shows "failed to change directory: <reason>" in a start-up notice and opens nothing (the window stays on the gate's message) | `main.rs` · EngineGate | W `--cwd /tmp/home/missing` · fails when the text differs or an engine starts |
| START-009 | `-D` / `--data-dir DIR` goes to `crush engine ensure --data-dir` and the workspace's `data_dir` (default `.crush`) | `main.rs` · `engine_cli::ensure` | R the argument list; W · fails when the engine's `data_dir` differs |
| START-010 | `-d` / `--debug` sends `debug: true` in the workspace create and turns on the app's debug log | `main.rs` · `connection.rs` | R · fails when the body lacks `debug` |
| START-011 | `-H` / `--host ADDR` skips `ensure` and joins that engine address directly; losing it only re-dials (today's client mode); with no flag the app always uses `ensure` | `main.rs` · `connection.rs` | R the join plan with a host · fails when `ensure` runs |
| START-012 | `-y` / `--yolo` (default from the settings file, itself default true); `--yolo=false` asks for permission; applied at launch only; the effective value comes back in the create answer and a difference shows the 5.2 notice (`engine-design.md:547-550`) | `main.rs` · `connection.rs` | W `--yolo=false` then a bash tool · fails when no permission slot opens |
| START-013 | `--channels server:name` (repeatable, hidden from `--help`) goes into the workspace's `channels` | `main.rs` | R · fails when the list differs |
| START-014 | Left out (PLAN.md:186) | — | — |
| START-015 | Project folder setup (the data folder's `.gitignore` with `*`, the project registered, idle saved diffs pruned) is done by the engine when the workspace is created (`internal/backend/create.go:188-193`) | engine | W fresh project · fails when `<proj>/.crush/.gitignore` lacks `*` |
| START-016 | The engine registers the project in `projects.json` in the global data folder (path, data_dir, last_accessed, saved) | engine | W · fails when the sandbox's `projects.json` lacks the project |
| START-017 | Config layering is the engine's; the desktop shows the merged result | engine · `ws.config` | W a global model and a project `crush.json` model · fails when the model button shows the global one |
| START-018 | Concurrent config writes are the engine's (copy-on-write store, file lock, staleness reload) | engine | T the desktop saves Customize Composer while the driver posts another `config/set` for the other client · fails when either value is lost |
| START-019 | Old settings are migrated by the engine on load | engine | W a config with the old `"theme"` string · fails when `ws.config.options.tui.active_theme` lacks it |
| START-020 | Moves with the crash notice to phase 6 (PLAN.md:170, "crash notice"; PLAN.md:133) | — | phase 6 |
| START-021 | `-s` takes a full ID, a full hash or a hash prefix (port of `resolveWorkspaceSessionID`, `root.go:296-316`, and its `sessionsMatchingHash`); errors "session not found: <id>" and `session ID "<id>" is ambiguous (N matches)` show in a start-up notice with the chat-list search focused (phase 5 swaps in the Sessions dialog, PLAN.md:134) | `lib/sessionid.js` (P3.9) | U prefix, shared prefix, none; W `-s abc` · fails when a text differs or the search is not focused |

### 2.6 LIST rows phase 3 needs

| ID | Desktop behavior | Where | Check · fails when |
|---|---|---|---|
| LIST-012 (part) | Chat-row states from the session record: `is_busy` → working icon and "Working…"; `pending_prompt` "permission" → clock icon and "Waiting for permission"; "question" → "Waiting for your answer"; `has_unfinished_work` without busy → "Background". The rest of the row (unread ✓, stopping, terminal marks) is phase 6 | ChatRow (P3.9) · `sessions` | W a busy chat in the list · fails when the state text differs |
| LIST-033 | Ctrl+N and "New chat" show the landing state (no record until the first send, TYPE-041); busy: "Agent is busy, please wait before starting a new session..."; an empty chat is never saved. The terminal's "LSPs stopped; background processes ended" does not apply: those are project-wide in the shared engine (decision D13) | ChatList · `sessions.newChat()` | W · fails when a session record appears before a send |
| LIST-034 | Switching chats shows "Opening <title>…" (or "Opening conversation…") with the activity mark and elapsed seconds after 1 s; a newer switch wins; a chat deleted during the open is abandoned | Opening (P3.9) · `sessions.openChat` | W open a 500-item chat · fails when an older switch's chat shows last |
| LIST-035 | Opening a chat restores its last model and provider when still available (the last assistant message, summaries skipped), as the terminal does in client mode (port of `model_select.go:40-101`). The model is project-wide, so this changes the other app's model too (risk R7) | `sessions.openChat` · `api.setModel` | T both apps on different chats with different models · fails when the desktop's chat does not run on its model |
| LIST-036 | Events for a chat that is loading are buffered and replayed (1.5.3) | `messages.svelte.js` | U `messages.test.js` interleavings · fails when a finished message is overwritten |
| LIST-037 | A chat deleted elsewhere: the window switches to the landing state with a notice | `sessions.svelte.js` | T delete from the terminal app · fails when the desktop keeps showing it |

### 2.7 Permission prompts (DLG-119 to DLG-140), in the typing box's place (Q8)

The prompt takes the composer slot of the window showing that chat (the engine sends prompts only to apps viewing the chat or its root, `internal/server/events.go:156-183`). The layout is in section 4.9. The answer is `POST permissions/grant` with the stored `PermissionRequest` and `allow`, `allow_session` or `deny` (`internal/proto/proto.go:187-220`).

| ID | Desktop behavior | Where | Check · fails when |
|---|---|---|---|
| DLG-119 | A `permission_request` for the shown chat opens the slot titled "Permission Required"; a newer request replaces it; one at a time; keys are ignored for the first 300 ms (the terminal's input grace; the builder copies its exact value from `dialogs.go:258-272`); the tool shows "Requesting permission..."; the slot closes when a `permission_notification` says it was answered elsewhere; the diff starts in `options.tui.diff_mode`. The system notification "Permission required to execute "X"" comes with phase 6 notifications | PermissionPrompt (P3.7) · `prompts.svelte.js` | W `--yolo=false`, script `tools` bash · fails when the slot doesn't open, or Enter within 300 ms answers |
| DLG-120 | No prompt when YOLO is on, the tool is in `allowed_tools`, a hook approves, the chat was allowed for the session, or a saved grant exists (engine rules) | engine | W "Allow for Session", then the same tool again · fails when a second prompt opens |
| DLG-121 | Tool and path lines; MCP tools as "server → tool"; a generic Path where the tool has no file or folder | PermissionPrompt; port of `permissions.go:445,508` | U |
| DLG-122 | Shell: the description and the highlighted command (display prefix removed) | PermissionPrompt content; port of `content.go:40` | W bash · fails when the command differs |
| DLG-123 | File writes (edit, write, multi-edit, replace symbol): a diff (DiffView) with a header | content; port of `permissions.go:313`, `content.go:54` | W edit · fails when no diff shows |
| DLG-124 | Download: URL, destination, timeout when set | content; port of `content.go:115` | U |
| DLG-125 | Fetch: URL | content; port of `content.go:129` | U |
| DLG-126 | Agentic fetch: the prompt and the URL when set | content; port of `content.go:138` | U |
| DLG-127 | View: file, start line (offset + 1), the limit when not the default 2,000 | content; port of `content.go:154` | U |
| DLG-128 | List: folder and ignore patterns | content; port of `content.go:171` | U |
| DLG-129 | Other tools: pretty highlighted JSON, else raw; MCP tools without the description | content; port of `content.go:185` | U |
| DLG-130 | Buttons Allow, Allow for Session, Deny; Allow selected first; ←/H and →/L or Tab move and wrap | PermissionPrompt | W · fails when the first selection is not Allow |
| DLG-131 | Enter or Ctrl+Y confirms the selected button | PermissionPrompt | W · fails when the action differs |
| DLG-132 | A, Shift+A, Ctrl+A allow once | same | W · fails when the grant isn't `allow` |
| DLG-133 | S, Shift+S, Ctrl+S allow for the session | same | W · fails when the grant isn't `allow_session` |
| DLG-134 | D, Shift+D deny; Esc denies (kept, PLAN.md:197) | same | W Esc · fails when the tool isn't denied |
| DLG-135 | Shift+↑/K and Shift+↓/J scroll the content; the wheel too; page keys do nothing | same | W a 200-line diff · fails when PgDn scrolls |
| DLG-136 | Shift+←/H and Shift+→/L scroll a diff 5 characters; clamped at the left | same | W · fails when it scrolls past the left edge |
| DLG-137 | T toggles split and unified (diff tools only); the start comes from `diff_mode`, else split when the content box is at least 140 mono characters wide | same | W · fails when T changes a non-diff prompt |
| DLG-138 | F shows the diff on the whole middle column (the slot grows over the timeline); forced when the slot would be under 77 mono characters wide or 20 lines tall | same | W small window · fails when the diff is not shown full |
| DLG-139 | A content scrollbar; buttons stack in a narrow column; key hints only when they apply | same | W narrow · fails when buttons overflow |
| DLG-140 | The buttons also take clicks (the row's GUI note) | same | W click "Deny" · fails when nothing happens |

### 2.8 Changes sheet (DLG-294 to DLG-311), opened by CHAT-084

A sheet slides over the right part of the window from the right edge (section 4.7), loads `GET sessions/{sid}/messages/{mid}/review` for the group's first tool message, and draws every change with DiffView.

| ID | Desktop behavior | Where | Check · fails when |
|---|---|---|---|
| DLG-294 | The sheet opens titled "<source> · Loading…", then the files | ChangesSheet (P3.6) | W click "Changes" · fails when the loading title is skipped on a slow fake review |
| DLG-295 | Heading: title, close ✕, source and action, file count, +/− totals where counted | ChangesSheet; port of `changes.go:273` | W · fails when a total differs from the review |
| DLG-296 | A file list: a kind square (added `--diff-add-ink`, removed `--error-ink`, other `--figure`), short path, +/− or a kind label | ChangesSheet; port of `changes.go:360` | U kinds |
| DLG-297 | File header: fold chevron, kind badge, path, counts | ChangesSheet; port of `changes.go:400` | U |
| DLG-298 | Diffs over 600 lines start folded | ChangesSheet; port of `changes.go:75` | U 600 vs 601 |
| DLG-299 | Columns: old and new numbers, sign, wrapped code; added and removed tints; hunk headings; wrapped continuation rows without numbers | DiffView (`wrap` mode); port of `changes.go:430,551` | W · fails when a continuation row has a number |
| DLG-300 | Tabs at 4-character stops; control characters dropped; paths relative, cut from the left with "…" | ChangesSheet; port of `changes.go:478,502,523` | U |
| DLG-301 | ↑/K and ↓/J one line; the wheel; clamped | ChangesSheet keys | W · fails when it scrolls past the end |
| DLG-302 | PgUp, B, Shift+Space and PgDn, Space, F scroll a page (height minus one line) | same | W · fails when the step differs |
| DLG-303 | Home/g, End/G | same | W |
| DLG-304 | N, ], Tab next file; P, [, Shift+Tab previous; no wrap | same | W at the last file · fails when it wraps |
| DLG-305 | Click a file in the list to jump to it; click a header (also the pinned one) to fold it | same | W · fails when the pinned header click is ignored |
| DLG-306 | The current file's header stays pinned at the top while its diff scrolls (`position: sticky`) | same | W · fails when the header scrolls away inside its file |
| DLG-307 | The scrollbar can be dragged; release ends | same | W · fails when dragging does nothing |
| DLG-308 | Esc, Alt+Esc, Q, ✕ or a left click outside closes; other buttons outside are ignored | same | W right-click outside · fails when it closes |
| DLG-309 | Width min(`calc(1180 * var(--px))`, window width minus `calc(160 * var(--px))` for the chat margin); the footer hints shorten when narrow; a tiny body draws nothing | same | W at 900 px · fails when the margin is gone |
| DLG-310 | Diff tints from the theme: a light paper takes the light set, a dark theme the dark set (by the lightness of `--paper`); unknown = dark | DiffView CSS · theme tokens | U the lightness rule |
| DLG-311 | An expired or missing diff shows the expired text of CHAT-084; other errors are reported; a late answer after closing is ignored | ChangesSheet; port of `reviews.go:15-73` | W script `tools` after pruning (the driver runs 6 prompts) · fails when the text differs |

### 2.9 Phase 3's own checks

**Engine gate (`p3_connect.py`).** The middle column shows EngineGate until `connection.state` is `connected`. States and texts:

| State | Text (section 4.12 has the look) | Made by | Fails when |
|---|---|---|---|
| connecting | "Starting Crush…" with the activity mark, after 400 ms (a fast join shows nothing) | `connection.svelte.js` | the text flashes on a join under 400 ms |
| `crush_not_found` | "Crush Desktop could not find crush on your PATH." plus the fix line "Install Crush, or set the crush path in Settings." (phase 5 adds Settings; until then the line names `CRUSH_DESKTOP_CRUSH`) | `crush_bin.rs` | a wrapper `crush` that exits 127 and an empty PATH show another text |
| `crush_too_old` | the message of 1.2.3, plus "Update Crush, then press Try again." and a "Try again" button | `engine_cli.rs` | `tests/fakes/crush-old` (prints "unknown command" and exits 1) shows another text, or Try again does nothing |
| `project_locked` and other ensure errors | the engine's message as is (for example `MsgLoneWindowHoldsProject`, `engine-design.md:1959`) and "Try again" | `engine_cli.rs` | the message is reworded |
| reconnecting, failed | the SIDE-030 notices; the timeline stays visible and read-only; the field keeps its text | `connection.svelte.js` | the field loses text or accepts a send |

After joining, the 5.2 notices: `The AI works in <path>, the folder the first window on this project opened.` when the answer's `path` differs from `cwd` (`engine-design.md:545`), and the two YOLO texts of `engine-design.md:550` when `yolo` differs. Checks: two desktop windows, then a desktop on a subfolder after a desktop on the top folder; the second shows the notice. Fails when no notice shows or the text differs.

**Two apps on one chat (`p3_two_apps.py`, the phase 3 done-line, PLAN.md:65).**

1. Build the terminal app from the worktree into `desktop/tests/out/bin/crush` (the coordinator's recipe, PLAN.md:294) and use the same binary as the desktop's engine (`DESKTOP_CRUSH`).
2. Start the desktop on `/tmp/home/proj` with script `basic`; send "first from desktop".
3. Start the terminal app in a hidden tmux on the same chat: `tmux -L crushtest -f /dev/null new-session -d -s tui -x 200 -y 50 "env CRUSH_CLIENT_SERVER=1 $CRUSH --cwd /tmp/home/proj -s $SID"` (`CRUSH_CLIENT_SERVER` is today's switch, `internal/cmd/workspace_setup.go:46-49`; after piece A it goes through `ensure` and joins the same engine).
4. Within 5 s, `tmux capture-pane -p -t tui` shows "first from desktop" and the reply's first line.
5. `tmux send-keys -t tui "second from terminal" Enter`; within 5 s the desktop probe's `chat.messages` has that prompt and its streamed reply, in the same order as `GET sessions/{sid}/messages` read with `curl --unix-socket` (the ground truth).
6. While the terminal's reply streams, the desktop's last item grows (two probe reads 500 ms apart differ) and its busy state is on; the terminal's pane shows its own status.
7. The desktop sends a third prompt while the terminal's turn runs: it is held (TYPE-044) in both apps' queues.
8. Close the desktop window: the terminal's turn keeps going (Q2, engine 5.5) and finishes.

Fails when any wait times out, the two apps or the engine disagree on order or text, the desktop shows a duplicate of any message, or step 8's reply stops.

**Speed bar (`p3_speed.py`, PLAN.md:156).** Script `stream2m`: one reply streamed for 120 s at 20 tokens per second in chunks of 1 to 4 words, with 12 tool calls spread through it (view, grep, bash with colored output, edit with a 40-line diff), two Mermaid fences and a 30-row table, on a chat that already has 300 items (seeded by `tests/fakes/seed_chat.py` through the engine API, so the virtual list is exercised).

1. Launch the app with `--probe`, open the chat, press Ctrl+Alt+F12 (the probe's frame mark), send the prompt.
2. Every 500 ms during the stream, type one letter into the field with `xdotool type --delay 0` (the input-latency sample) and once every 10 s press BackSpace ten times.
3. When `run_complete` arrives (probe `chat.busy` false), read `frames`.
4. Repeat steps 1-3 three times (three launches, same seed).

A run passes when: p95 of frame times is 20 ms or less; at most one frame is over 50 ms; at most one key-to-next-frame time is over 50 ms. The check passes when all three runs pass and no two runs have a frame over 50 ms within ±250 ms of the same time after the mark. The report (`tests/out/p3_speed/report.json`) lists every run's p95, max, and over-50 times. Fails otherwise.

**Losing the engine (`p3_reconnect.py`).** While script `basic` streams, the driver kills the engine (`kill -9` on the pid in `connection.engine.pid`). Expected: the reconnecting notice; a new engine from `ensure`; "Reconnected to the Crush server."; the chat reloads with every finished message the engine has; the field kept its text and sends again. A second case makes `ensure` fail 3 times (the wrapper `tests/fakes/crush-wrap` fails `ensure` while `/tmp/fail-ensure` exists): the failed notice and "Try again" show; removing the file and pressing Try again reconnects. Fails when the state is not `connected` within 15 s, or messages are missing or doubled against the engine's list.

**Safety (`p3_safety.py`).** Script `unsafe` sends replies with: raw `<script>`, `<img src=x onerror=…>`, `<a href="javascript:…">`, `[x](javascript:alert(1))`, `[x](data:text/html,…)`, an `<iframe>`, a `<form>` posting to the fake server, an `<img>` pointing at `http://127.0.0.1:<fake port>/beacon`, a Mermaid fence with `%%{init: {"securityLevel": "loose"}}%%` and a `click` callback, KaTeX `\href{javascript:alert(1)}{x}` and `\url{…}`, and a reply that tries to look like the permission slot. Expected: the probe's `safety` section reports `scripts_found 0`, `handlers_found 0` (no `on*` attribute anywhere in the timeline), `bad_hrefs_found 0`, `csp_violations 0`; `<probe>.opened.jsonl` stays empty; the fake server's log has no `/beacon` request; the Mermaid figure draws (strict) or shows its source with the fallback line; the fake slot is plain text inside the reply. Then the driver clicks each link once: only the `http` one adds a line to `opened.jsonl`. Fails on any other outcome. The same script runs in U (`render/sanitize.test.js`) against the renderer in Node with a DOM shim (`linkedom`, a dev dependency pinned by S3; inferred version, see 5.1).

**Long chats (`p3_long.py`).** A 1,000-item chat seeded by `seed_chat.py`; open it, End, Home, Shift+↓ ×50, a 3,000-line tool body opened and closed, a search for a word in the first message by scrolling. Fails when any frame is over 100 ms more than once, the item rects overlap, or the DOM holds more than 300 item elements.

### 2.10 Rows pulled into phase 3, and why

| Rows | Reason |
|---|---|
| START-001 to START-021 | PLAN.md:292 assigns them to this design; they are start-up behavior of the same window |
| DLG-119 to DLG-140 | PLAN.md:292; Q8 puts them in the typing box's place, which this design owns |
| DLG-294 to DLG-311 | CHAT-084 cannot pass without the Changes sheet |
| LIST-012 (states only), LIST-033 to LIST-037 | the picture shows the states; opening, loading and new chats are part of showing a chat |
| DLG-089 to DLG-112 | PLAN.md:292; designed in phase 4 (section 3.6), built when phase 5's palette exists |
| TYPE-015, START-020 | moved out to phase 6 (terminal tabs, crash notice) |

---

## 3. Phase 4: branching

Phase 4's done-line (PLAN.md:66): live checks for each AI tool are saved; every branching behavior passes with a real mouse in the real window: closing, reopening from a link, restarting, branches of branches, and branching mid-reply. It needs engine piece C (5.6, 5.11) and phase 3 (PLAN.md:71).

### 3.1 Rules (`lib/branch.js`, P4.1)

```js
export const TITLE_MAX = 34;                 // demo core.js:255-276: longer titles become 33 characters + "…"
export function branchTitle(text) {}         // collapse whitespace; cut to 33 + "…" when over 34
export function quoteDraft(text) {}          // the branch's typing box: "> " + collapsed text + "\n\n" (demo core.js:255-276)
export function quoteInReply(text) {}        // menu "Quote in reply": "> " before every line, then a blank line (decision D14)
/** The selection inside one message's text, in UTF-16 code units of M.Content().Text. */
export function selectionToCut(selection, sourceMap) {} // → {message_id, start, end, text} | {error}
export function overlaps(links, start, end) {}          // half-open [s, e): touching ranges are fine (engine 5.11 step 4)
export function canBranch(message, cut, links) {}       // → {ok} | {reason: "unfinished" | "tool_output" | "overlap" | "empty" | "two_messages"}
```

- **Where a cut can start.** Only inside one message's own text: a user message's text or a reply's Markdown text (PLAN.md:99). A selection that touches a tool row, a shell item, a notice, a footer, or two messages gives `two_messages` or `tool_output`, and "Branch from here" is disabled with that hint.
- **Offsets.** A user message maps through `render/usertext.js` (CHAT-004's offset map). A reply maps through the renderer's source map (CHAT-088), so the cut is a range of `M.Content().Text`, the stored Markdown, and `text` is exactly `t[start:end]` as the engine requires (`engine-design.md:2177`). JS string indexes are UTF-16 code units, the engine's unit (`engine-design.md:2151`), so no conversion is needed.
- **Selections that end inside markup.** When a selection covers only part of a link label, an emphasis or a code span, the cut is the source range of the selected characters only (no marks); when it covers a whole span, the cut takes the span's marks too, so the link still matches the visible words (the same rule as CHAT-088).
- **Overlap** is checked before the menu opens, against the links already on that message, and again by the engine (409 `highlight_overlaps_link`).
- **Unfinished replies.** A reply without a `finish` part, or with tool calls still lacking results, disables "Branch from here" with the hint "Wait for the reply to finish" (desktop text). The engine also refuses with 409 `cut_not_finished`.

### 3.2 Making a branch

1. The right-click menu's "Branch from here" (order 10, registered through `lib/menu.js`) calls `api.createBranch(sid, {message_id, highlight_text, highlight_start, highlight_end, title: branchTitle(text)})`.
2. On success (`proto.Branch`), the sessions store adds `branch.session`; the links store adds `branch.link`; the window opens the new chat at once (`openChat`) and puts `quoteDraft(text)` into the field with the caret at its end (PLAN.md:101). Auto-titling never replaces the title (engine 5.11).
3. Errors show as notices with these desktop texts: `bad_cut` → the engine's message; `cut_not_finished` → "This reply is still being written."; `highlight_overlaps_link` → "These words already open a branch."; `transport` or `engine_unavailable` → "Branch not made: lost the connection to Crush." The selection stays.
4. The other app learns about the branch from `session` created and `chat_link` created events; the terminal app lists it as a normal chat (PLAN.md:102).

### 3.3 Links, nesting, closing (P4.1)

**Stores** (S3 stubs, P4.1 bodies):

```js
// links.svelte.js
export const links = $state({ byId: {}, byParentMessage: {}, originOf: {} }); // originOf: branch session id → origin ChatLink
export async function loadLinks();         // api.chatLinks(); on start, on resync, after any session "deleted" event (engine-design.md:2310)
export function upsertLink(link);          // chat_link created
export function linksFor(messageId);       // decorations for the renderer: [{kind: "branch-link", start, end, data: link}]
export function parentOf(sessionId);       // origin link's parent_session_id, or null (top level)

// closed.svelte.js
export const closed = $state({ ids: new Set() });   // from settings.closed_chats[engine data_dir]
export async function close(sessionId);    // closed_set(project, id, true)
export async function reopen(sessionId);   // closed_set(project, id, false)
export function isClosed(sessionId);

// tree.svelte.js
export const tree = $state({ bySession: {} }); // sid → {revision, entries}; read when a link's message is not on the shown path
```

**How a link looks and acts.** The renderer draws each `branch-link` decoration over its source range as a link (`--link`, 1px underline at `calc(4 * var(--px))` offset, as `Conversation.svelte:181-186`), and the message gets one vermilion key on the rail (section 4.8). A click with a collapsed selection opens the branch (demo `core.js:346-352`): if the branch is closed it is reopened first (`reopen`, demo `core.js:52-60`). A click that ends a drag-selection does not open it. Links copied into a branch come along (engine 5.11), so sibling branches stay reachable from inside a branch (PLAN.md:104).

**Links hidden by the tree.** A link shows only when its `source_message_id` is on the shown chat's current path (the loaded messages). After `tree_changed` reloads the chat, links on messages no longer on the path disappear, and show again when the path comes back (PLAN.md:104). No tree read is needed for this, because the loaded messages are the current path.

**Nesting in the left list.** `listmodel.buildRows(sessions, {parentOf, isClosed})` (P3.9) nests each branch under its origin link's parent, at any depth. A closed chat is left out and its children move up to the nearest open ancestor (or the top level) (PLAN.md:103; demo `visibleChildren`, `core.js:67-76`). A parent that was deleted has no origin link left, so its branches are top level (engine 5.11). The open chat's ancestors are expanded automatically. The look is section 4.8.

**Closing.** Each list row gets a close control (× on hover and on the selected row, the terminal's LIST-011 place). Closing calls `closed.close(id)`; when the open chat is closed, the window opens the first visible chat in the list, or the landing state when none is left (decision D15). The list footer says "N closed · links reopen them" (demo `core.js:93-94`) while N > 0; a closed chat without a link comes back through the Sessions search (Ctrl+K; phase 5's dialog lists closed chats too, PLAN.md:103). The closed set lives in the desktop settings file under the engine's `data_dir`, so all desktop windows agree and it survives restarts; `settings-changed` updates other windows at once.

**Head path line.** A branch's chat head path line reads "branched from <parent title>" (demo `core.js:101`), a link button that opens the parent; other chats show the folder and Git branch (D12).

### 3.4 AI-tool memory: the forkers (P4.2, Go)

The engine design fixed the seam and left the forkers to phase 4 (`engine-design.md:2254-2268`): `ForkPoint{Native, Marker, MarkerKind}`, `Forker.Fork(ctx, p) (native string, err error)`, `ErrNoFork`.

**Where it hooks in.** `cliStream` (`internal/agent/cli_turn.go:57-166`) reads the tool link at `treeCLILink` (`cli_turn.go:70`). P4.2 adds, right after it: when the chat has no link for this tool and is a branch (`BranchOrigins.BranchOrigin`, engine 5.11), look up the source chat's link for this tool and the marker recorded on the cut message (or its last tool message when the cut ran tools, `For: "tool"`). With both present, call that tool's `Forker.Fork`; on success, save a link `{Native: <new native ID>, Through: <the cut message>}` with `saveCLILink` (`cli_turn.go:149-151,211-228`) and run the turn with `Turn.Resume` = the new native ID, so the normal resume path continues and the handoff (`cli_handoff.go:28-45`) adds only the messages after the cut. On `ErrNoFork`, a missing marker, or any error, nothing is saved and today's written-copy handoff runs (`cliHandoffWithTranscript`, `cli_turn.go:78`). The source chat's own native session is never resumed in place.

| Tool | Fork call (P4.2 builds; P4.3 confirms live first) | Cut |
|---|---|---|
| Claude (`claude.go`) | run once with `--resume <native> --fork-session --resume-session-at <uuid>` (inferred from t6's research and the hidden-flag note in PLAN.md:181; `--resume` is built at `claude.go:297-299`), take the new session ID from the stream's `system` init line | exact: the marker message |
| Codex (`codex.go`) | `thread/fork` on the app server (`codex.go:290` starts it) with the source thread and the last whole turn, instead of `thread/start`/`thread/resume` (`codex.go:474-486`) | the last whole turn; the rest of the cut turn goes in as a written note (below) |
| Grok (`acp.go`) | ACP `session/fork` extension (`x.ai/session/fork`, inferred from t6), then `session/load` of the new session (`acp.go:261-267`) | the last whole prompt (`prompt_index`), plus the note |
| OpenCode (`acp.go` or its HTTP API) | the fork at the message after the cut (PLAN.md:107), only if the live check (3.5) confirms it; otherwise `ErrNoFork` | the message after the cut |
| AGY (`agy.go`) | `ErrNoFork` (Q3) | written copy |

**The partial-turn note** (Codex and Grok). When the cut falls inside a turn, the messages of that turn up to the cut are sent with the first branch prompt, after the intro line "This chat is a branch made partway through your last turn. Here is the part of that turn the branch keeps:" (desktop text, in the style of `cli_handoff.go`'s two intros).

**Go tests (P4.2)**, with the fake CLIs of `internal/agent/cliagent/cliagent_test.go:20-28` extended to log their arguments and stdin:

| Test | Proves | Fails when |
|---|---|---|
| `TestClaudeForkArgs` | the first send in a branch runs Claude with `--resume <src> --fork-session --resume-session-at <marker uuid>` and later sends resume the new ID | the source ID is resumed, or the marker is wrong |
| `TestCodexForkAtLastWholeTurn` | `thread/fork` names the last whole turn; the note carries the rest | the cut turn is dropped or doubled |
| `TestGrokForkPromptIndex` | fork then `session/load` of the new session with the prompt index | `session/load` of the source |
| `TestOpenCodeForkOrNone` | with the probe flag off, `ErrNoFork` and the written copy | a fork without confirmation |
| `TestAGYWrittenCopy` | AGY always gets the written copy | a fork is tried |
| `TestForkFailureFallsBack` | a fake tool that fails the fork leads to the written copy and no saved link | the turn errors |
| `TestSourceChatUntouchedByFork` | the source chat's saved link and native session are unchanged after branching and sending in the branch | the source link changes |
| `TestBranchOfBranchForks` | a branch of a branch forks from the middle branch's native session at its marker | it forks from the root |

### 3.5 Live checks for each AI tool (P4.3; the first step of phase 4, PLAN.md:112)

Each tool is checked once, live, outside the sandbox, in a throwaway project under `/var/tmp/crush-desktop-live/<tool>/` with a throwaway Crush data folder, using the user's subscription (no paid API calls). Each step's output is saved under `desktop/docs/evidence/phase4/live-<tool>/` (not `/tmp`). The scripts never read `cli-sessions.json` or `~/.claude/projects`: they take native IDs from the tools' own stream output.

1. `<tool> --help` (and the fork subcommand's help) saved as text.
2. A tiny chat: "Say the word ALPHA, nothing else." then "Say the word BETA." Record the markers.
3. Fork at the first reply; in the fork ask "Which words did you say?" The expected answer names ALPHA only.
4. Ask the source chat the same question: it names ALPHA and BETA (source untouched).
5. Mid-reply: start "Count slowly to 20" in the source, branch at the finished first reply while it runs, send in the branch at once. The branch answers from the cut; the source finishes counting.

The result table goes to `docs/evidence/phase4/live-summary.md`: tool, version, fork command, pass or fail per step. A tool that fails steps 3-5 uses the written copy, and the coordinator tells the owner (PLAN.md:112). Fails when any step's output is missing from the evidence folder.

### 3.6 Session tree and file restore dialogs (DLG-089 to DLG-112; P4.4)

They use the 5.6 routes (`engine-design.md:1236-1246`). As in the terminal, the palette is their only entry point (Session Tree, Fork from Prompt, Undo Last File Restore), so P4.4 registers three actions and two dialogs, and its window checks run once phase 5's palette exists.

| ID | Desktop behavior | Check · fails when |
|---|---|---|
| DLG-089 | With no chat open, the action shows "start a chat first" (the terminal's notice); the dialog reads `GET …/tree` | W palette "Session Tree" on landing · fails when the dialog opens |
| DLG-090 | Rows: the start-of-chat row, the active row marked `*`, indentation by depth, −/+ fold marks, role, bookmark label, one-line text; empty text "[tool activity]" | U `tree.test.js` from a fixture tree · fails when a row differs |
| DLG-091 | Typing searches text and labels, including folded rows; the selection resets | U |
| DLG-092 | Tab/Shift+Tab cycle the filters: default, no tools, user only, labeled only, all; default hides empty, unlabeled non-text rows | U |
| DLG-093 | ↑/Ctrl+P, ↓/Ctrl+N; scroll into view; no wrap | W · fails when it wraps |
| DLG-094 | ← and → fold and unfold; search still shows folded rows | W |
| DLG-095 | Alt+↑ / Alt+↓ jump between branch starts and leaves | W |
| DLG-096 | Ctrl+R edits the label (`…/tree/label` with `expected_revision`); empty clears; not on the root; Esc cancels | W · fails when the label isn't saved |
| DLG-097 | Ctrl+E on a past user prompt: navigate there and put the prompt in the field; refused for non-user, root or expired rows | W |
| DLG-098 | Enter jumps to the selected point: the summary choice, then the file preview | W |
| DLG-099 | Summary choice "No summary" (default) or a summary; Esc goes back | W |
| DLG-100 | Progress "Switching branch…" / "Writing branch summary…"; results "Branch switched/copied … Files on disk are unchanged.", "Jump canceled. Your old branch is still selected."; Esc cancels; other keys ignored | W · fails when the old branch changes before success |
| DLG-101 | Fork mode: only user prompts, no start row, no filter switching; Enter forks (`…/tree/copy` with `fork: true`) into a new chat with the prompt in the field | W · fails when the new chat lacks the prompt |
| DLG-102 | Guards with the terminal's texts: "start a chat first", "turn off phone sharing first", "wait for the chat to finish loading", "wait for the reply to finish", "wait for pending prompts, or recall them first"; a 409 `tree_busy` shows the engine's text; a 409 `revision_conflict` re-reads the tree and says "The chat's branches changed. Try again." (desktop text) | U each guard; W busy chat · fails when a jump happens |
| DLG-103 | Esc closes (label, confirm and progress states use Esc first) | W |
| DLG-104 | "Checking files…" while `…/files/preview` runs; only Esc works | W |
| DLG-105 | Preview rows: action, +/− counts, path, SKIP/unavailable or CONFLICT reason | U |
| DLG-106 | The scope notice (conflicted and unavailable skipped; only tracked files; databases, packages, network effects and commits not undone) | U |
| DLG-107 | The Git notice: a moved HEAD warns that restored files become uncommitted; else it points to the palette's undo | U `git_moved` true and false |
| DLG-108 | Buttons Restore files (default) / Chat only / Cancel; ←/→, Tab/Shift+Tab, Enter; also clicks | W |
| DLG-109 | Undo: "Undo restore", "Keep files", "Cancel" (`…/files/undo-preview`, then `…/files/undo` with `plan_id`) | W |
| DLG-110 | ↑/↓ scroll; paths wrap; buttons stack when narrow; the title has the file count | W narrow |
| DLG-111 | "Restoring files…" "Please wait" while it runs | W |
| DLG-112 | A stale preview is ignored; a changed chat (409 `revision_conflict`) rejects the approval; a zero-change preview can be skipped; an expired plan (409 `plan_expired`) asks to check again | U each error code |

### 3.7 Branching behaviors, item by item (`p4_branch.py`, `p4_restart.py`, `p4_mid_reply.py`, `p4_two_apps.py`)

All with the real mouse: the driver selects words by pressing on the first word's rect and releasing on the last word's rect (from `chat.words`), then right-clicks inside the selection.

| # | Behavior | Check · fails when |
|---|---|---|
| B1 | The right-click menu shows only our items: Branch from here, Copy, Quote in reply, Copy whole message (PLAN.md:99); the native menu never shows | W right-click on a reply · fails when `menu.items` differs or a native menu appears (screenshot compares the menu box to the probe rect) |
| B2 | Branch from a reply's words: the new chat opens at once, its title is `branchTitle(words)`, the field holds `quoteDraft(words)`, the source shows the link and a key | W · fails when any of the four is missing |
| B3 | Branch from your own message's words: the branch ends with that message; the next send's AI request (fake AI log) holds it unanswered before the new prompt | W · fails when the request lacks it or has an answer after it |
| B4 | The cut of a reply that ran tools includes their results | W script `tools` · fails when the branch's messages differ from the source's path through the cut |
| B5 | Tool output cannot be a cut: the item is disabled with the hint | W select in a tool body · fails when "Branch from here" is enabled |
| B6 | An overlapping highlight is refused before the call; touching ranges work | W · fails when an overlap is sent or a touching one refused |
| B7 | Mid-reply: a still-streaming reply's item is greyed with "Wait for the reply to finish"; branching from an earlier finished message works while the source streams, and the source reply finishes undisturbed | `p4_mid_reply.py` script `stream2m` · fails when the source stream stops or the branch holds unfinished messages |
| B8 | Sending in the branch at once (mid-reply source) works | same · fails when the send waits for the source |
| B9 | Closing: × on a branch hides it, its children move up, the footer says "1 closed · links reopen them" | W · fails when a child disappears with it |
| B10 | Reopening from a link: clicking the link in the parent opens the closed branch and it is listed again | W · fails when it stays closed |
| B11 | Restart: closed state, links and nesting survive quitting and starting the app | `p4_restart.py` · fails when anything differs from before the restart |
| B12 | Branches of branches nest one level deeper; links copied into a branch open their siblings | W · fails when depth or a sibling link is wrong |
| B13 | A deleted parent makes its branches top level (delete through the terminal app or `curl`) | T · fails when a branch disappears or stays nested |
| B14 | The terminal app lists the branch as a normal chat with its title (PLAN.md:102) | T capture-pane · fails when the title is missing |
| B15 | Moving the parent's tree head away from the linked message (terminal app's tree, or `…/tree/switch`) hides the link; moving back shows it | T · fails when the link shows off-path |
| B16 | A second desktop window sees the new branch, its link and closing at once | W two windows · fails when the second window lags past 1 s |
| B17 | AI memory: the first send in a branch of a Claude chat runs the fake `claude` with the fork flags; Codex with `thread/fork`; Grok with the fork call; AGY with the written copy | `p4_fork_args.py` with `FAKE_TOOL_LOG` · fails when the logged arguments differ from 3.4's table |

"Two windows" in B16 means two app processes on the same project (`--probe /tmp/probe2.json`); one app hosting several windows is phase 6's single-window work.

---

## 4. Look decisions

One exact proposal per new surface, in the Scholar's edition language of the built screen (`src/app.css`, `src/components/*`). Under the overnight rule (PLAN.md:299) the coordinator decides each one and shows the owner a picture in the morning. Sizes are picture pixels (`calc(N * var(--px))`); "1px" is one real pixel. Every surface keeps square corners, flat color, hairline rules, no shadows, glow, blur, gradients, emoji or sparkle icons, and vermilion only on the rail, keys, the send button, focus hairlines and the open chat's edge (all already built).

### 4.0 New `app.css` tokens (S3 adds them; phases 5-6 themes set the same names)

```css
  /* Errors, warnings and diffs */
  --error-ink: #a1261b;        /* oxblood: ERROR tags, exit codes, failed counts; 7.0:1 on --paper */
  --amber-ink: #8a5a00;        /* WARN text, "~" estimates in warnings; 5.6:1 on --paper (--amber stays for icons) */
  --diff-add-bg: #eaf0dc;
  --diff-add-gutter: #dce6c8;
  --diff-add-ink: #2f5a1e;     /* "+N" counts, added squares; 7.6:1 */
  --diff-del-bg: #f6e6df;
  --diff-del-gutter: #efd5ca;  /* removed lines use --error-ink for "−N" */

  /* Selection and small tints */
  --selection: #e6d9a8;        /* ::selection in the timeline and fields */
  --skill-tint: #ece6d2;       /* $skill words in the field */
  --code-inline: #6a3d12;      /* inline code text; 8.6:1 */
  --code-bg: var(--band);      /* code blocks and tool bodies */
  --scrim: rgb(249 248 239 / 0.92);  /* image viewer and sheet backdrop: paper, no blur */

  /* Code highlighting (highlight.js classes map onto these) */
  --hl-keyword: var(--ink);    /* weight 600 */
  --hl-string: #3f5f22;
  --hl-number: #7a4a08;
  --hl-comment: #5f5e5a;       /* italic; 5.7:1 on --band */
  --hl-function: var(--link);
  --hl-type: #3e4f7a;
  --hl-attr: #6a3d12;
  --hl-meta: #5f5e5a;

  /* The 16 program colors for this paper (every one 4.5:1 or more on --paper) */
  --ansi-0: #0f0f0f;  --ansi-1: #a1261b;  --ansi-2: #3f6b22;  --ansi-3: #8a5a00;
  --ansi-4: #1f4f8a;  --ansi-5: #7a3b7d;  --ansi-6: #1d6670;  --ansi-7: #6f6e6f;
  --ansi-8: #5f5e5a;  --ansi-9: #c2341f;  --ansi-10: #4a7827; --ansi-11: #946400;
  --ansi-12: #2d63b0; --ansi-13: #924a95; --ansi-14: #23727c; --ansi-15: #4a4947;

  /* Reading roles for Markdown */
  --t-h1: 400 calc(30 * var(--px)) / calc(38 * var(--px)) var(--face-display);
  --t-h2: 400 calc(25 * var(--px)) / calc(32 * var(--px)) var(--face-display);
  --t-h3: 600 calc(21 * var(--px)) / calc(30 * var(--px)) var(--face-text);
  --t-code: 400 calc(17 * var(--px)) / calc(24 * var(--px)) var(--face-mono);
```

Contrast values were computed from the WCAG formula against `--paper` `#f9f8ef` (inferred arithmetic; the S3 builder re-runs it with the design detector). H4 to H6 use `--t-label` small caps.

### 4.1 Markdown in replies

- Paragraphs `--t-reading`, `calc(14 * var(--px))` between blocks; prose width `calc(540 * var(--px))`.
- H1 `--t-h1`, H2 `--t-h2`, H3 `--t-h3`, H4-H6 `--t-label` uppercase; `calc(22 * var(--px))` above a heading, `calc(8 * var(--px))` below.
- Lists: bullets are `•` in `--figure`, hanging indent `calc(22 * var(--px))`; numbers in `--t-reading` tabular figures; task boxes are a drawn `calc(14 * var(--px))` square (1px `--figure`) with a check icon when done.
- Quotes: 1px `--rule` on the left, `calc(16 * var(--px))` padding, slate text.
- Code blocks: `--t-code` on `--code-bg`, 1px `--rule-faint`, padding `calc(12 * var(--px)) calc(14 * var(--px))`, horizontal scroll inside the block, full timeline width; a language label in `--t-label` slate at the top right inside the block.
- Rules (`---`): 1px `--rule` with the + marks of `Rule.svelte`.
- Links as built (`Conversation.svelte:181-186`).

### 4.2 Right-click menu (phase 3: three items; phase 4 adds the first)

```
┌──────────────────────────────────┐   paper-raised, 1px --rule-control, square, no shadow
│  Branch from here                │   phase 4; disabled: --mark text + hint line below
│    Wait for the reply to finish  │   hint: --t-small italic, --mark, only when disabled
├──────────────────────────────────┤   1px --rule-faint, 6 px inset each side
│  Copy                            │
│  Quote in reply                  │
│  Copy whole message              │
└──────────────────────────────────┘
```

- Box: min width `calc(260 * var(--px))`, max `calc(360 * var(--px))`; padding `calc(6 * var(--px)) 0`; at the pointer, flipped to stay inside the window.
- Items: `calc(36 * var(--px))` tall, padding `0 calc(16 * var(--px))`, `--t-body-small` ink; hover and keyboard focus on `--band`; no icons.
- Copy and Quote in reply are disabled without a timeline selection; Copy whole message is enabled on any message.
- Keys: ↑/↓ move (skipping disabled), Enter runs, Esc closes; Shift+F10 or the Menu key opens it on the selected item.
- Closes on an outside press, a scroll, a window blur or any run (demo `core.js:337-343`).

### 4.3 Visuals chips (chat) and figures (right panel)

- **Chip**, on its own line where the block was:
  - `calc(30 * var(--px))` tall, padding `0 calc(12 * var(--px))`, 1px `--rule-control`, `--paper-raised`.
  - Inside: the kind in small caps (`--t-label`, `--track-label`, slate: DIAGRAM, MATH, TABLE, PICTURE), the tool row's 4 px dot (`Conversation.svelte:237-245`), then the title in `--t-small` ink, cut with "…" at `calc(420 * var(--px))`.
  - Titles: a diagram's front-matter `title` or its first node label; a table's first three header cells joined by " · "; math's first 24 characters; a picture's alt text or file name.
  - Hover underlines the title. Click or Enter scrolls VISUALS to the figure and marks it with a `calc(2 * var(--px))` ink bar at its left for 1.2 s.
- **Figure** in VISUALS, in chat order:
  - No box. A heading in `--t-heading` (the title, last two words kept together as `SidePanel.svelte:19-21`), the body, then the caret rule between figures (`SidePanel.svelte:77`), as the picture's flow and table.
  - Clicking the heading scrolls the chat to its chip.
  - Mermaid: `look: "neo"` (PLAN.md:14 "modern style"; falls back to `classic` if 12.2.0 lacks it), `theme: "base"`, `themeVariables: {background: "transparent", primaryColor: "#f9f8ef", primaryBorderColor: "#383634", primaryTextColor: "#0f0f0f", lineColor: "#383634", fontFamily: "Spectral", fontSize: "15px"}`; square node corners; the SVG sized to the panel width.
  - Math: KaTeX display, centered, `1.1em`.
  - Tables: the picture's table (`SidePanel.svelte:244-288`); wide tables scroll inside the figure.
  - Pictures: full panel width at most, 1px `--rule-faint` frame; click opens the viewer.
- **Panel hidden:** figures draw inline in place of their chips, with a small-caps caption line (kind and title) above.
- **Web pictures:** a chip with the kind PICTURE and the host name; clicking opens the browser (the CSP blocks loading web pictures).
- **Empty VISUALS:** the demo's empty text (`core.js:157`) in `--t-small` slate.

### 4.4 Timeline items

- Selection bar: `calc(2 * var(--px))` ink at `calc(56 * var(--px))`, the item's full height.
- Keys: one vermilion key (the built `.key`, `Conversation.svelte:189-203`) per item that holds a branch link (phase 4) or is a tool group. The rail's top dot stands for the chat's start, A; keys run B, C, … in timeline order, then AA, AB (inferred from the picture, where the first key is B; decision D16).
- Error banner: `calc(2 * var(--px))` `--error-ink` rule on the left, padding-left `calc(14 * var(--px))`; "ERROR" `--t-label` in `--error-ink`; title `--t-body-small` ink; details `--t-small` slate. Refused: the same with "REFUSED" in `--amber-ink`. Canceled: italic `--t-small` slate, no rule.
- Notices (task, question, skill) and footers: `--t-small` slate, a 16 px line icon, one line.
- Plan box: CHAT-031.
- Pending prompt: the user box with "sending…" in place of the time.

### 4.5 Image viewer

```
┌──────────────────────────────────────────────────────────────────────────────────┐ --scrim over the window
│ screenshot.png   1280 × 720 · 214 KB                       Fit  Actual size  Close Esc │ 56 px bar
+──────────────────────────────────────────────────────────────────────────────────+ rule with + marks
│                                                                                  │
│                 ┌──────────────────────────────────┐                             │
│                 │          the picture              │ 1px --rule-faint frame     │
│                 └──────────────────────────────────┘                             │
└──────────────────────────────────────────────────────────────────────────────────┘
```

- Bar: `calc(56 * var(--px))` tall, padding `0 calc(24 * var(--px))`; the name in `--t-item`, size in `--t-small` slate; text buttons in `--t-body-small` (the active fit mode underlined).
- The picture is centered with `calc(24 * var(--px))` margins, fit by default; "Actual size" (or F) shows 1:1 with drag and arrow-key panning (40 px steps); transparent pictures sit on `--paper-raised`, no checkerboard.
- Esc closes; focus stays inside while open and returns to the chip.

### 4.6 Tool rows when opened

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ ▾  Ran 4 tools · read session.go, ran a command                     Changes │ the built 44 px row; "Changes" only with file changes
├─────────────────────────────────────────────────────────────────────────────┤
│    … 2 earlier                                                              │ text button, slate
│    ✓  View   auth/session.go  (limit=200)                                   │ 36 px lines, 1px --rule-faint between
│    ✓  Bash   go test ./auth/...                                             │
│    ▾ ✓ Edit   auth/session.go                                     +3 −1     │ an open tool
│       ┌ diff, full box width, 10 lines + "… (N lines hidden) [click or space to expand]" ┐
└─────────────────────────────────────────────────────────────────────────────┘
```

- The open group continues the row's box: left, right and bottom 1px `--rule-control`.
- Tool lines: padding-left `calc(21 * var(--px))` (under the disclosure); a 16 px status mark (CHAT-047); the name at weight 500 in `--t-body-small`; the main parameter in `--face-mono` `calc(17 * var(--px))` slate; pairs in slate; counts right-aligned (`+N` `--diff-add-ink`, `−N` `--error-ink`).
- Bodies open in place under their line, indented to the name, on `--code-bg`.
- The disclosure turns 90° in 120 ms (none with reduced motion).
- Agent, Question and Agentic Fetch use the same line and body in their own box.
- Verbs when no file changed (CHAT-083), joined with ", ", at most three, then "…":
  - View: "read <name>" or "read N files".
  - Glob, Grep, List, Sourcegraph: "searched".
  - Bash: "ran a command" or "ran N commands".
  - Fetch, Download: "fetched <host>". Web Search: "searched the web".
  - To-dos: "updated the plan". MCP: "used <server>". Agent: "asked a helper".
  - Diagnostics, LSP tools: "checked code".

### 4.7 Changes sheet

- From the right edge over the panel and part of the chat: width min(`calc(1180 * var(--px))`, window − `calc(160 * var(--px))`); `--paper`; 1px `--rule-frame` on its left; the uncovered strip gets `--scrim` at 60%.
- Heading `calc(72 * var(--px))`: title `--t-heading`, the source line (action, file count, totals) `--t-small` slate, ✕ at the right; then a rule with + marks.
- Body: a file list column `calc(260 * var(--px))` wide (1px `--rule-faint` on its right) and the diff column. Kind squares are `calc(8 * var(--px))`. File headers are sticky, on `--paper-raised`, with a 1px bottom rule.
- Footer hints in `--t-small` slate: "↑↓ scroll · n/p file · esc close", shortened when narrow.

### 4.8 Branch rows, nesting marks and links (phase 4)

- **Rows** keep the built branch row (`ChatList.svelte:252-312`): 72 px tall, `--t-branch` title, the mono agent line, the time.
- **Depth.** Each level adds `calc(28 * var(--px))`: the vertical tree line (`max(1px, calc(1.5 * var(--px)))` `--figure`) sits at `calc((35 + 28 * (depth - 1)) * var(--px))`, its 16 px tick at the row's 34 px line, and the row's text starts at `calc((64 + 28 * (depth - 1)) * var(--px))`. The indent stops growing at depth 6.
- **Expanding.** A parent row shows the chevron (as built) when it has visible branches; the open chat and its ancestors are expanded.
- **Closing.** A 16 px × button replaces the time while the row is hovered or focused, and stays on the selected row (the time then moves left of it).
- **Footer.** Above the list's end rule: "N closed · links reopen them" in `--t-small` slate, only while N > 0.
- **Links** look as built (`--link`, 1px underline, `calc(4 * var(--px))` offset); hover thickens the underline to `max(1px, calc(1.5 * var(--px)))`; the title attribute says "Open <branch title>" (or "Reopen …" when closed).
- **Keys.** The message holding links gets one key (4.4).
- **Head.** A branch's path line reads "BRANCHED FROM <parent title>" in the built path style (`Conversation.svelte:91-100`), the parent title as a link.

### 4.9 Composer area: mode mark, chips, tray, slot, permission prompt

- **Mode mark**, inside the field before the text, vertically centered on the first line:
  - plan: "PLAN" in `--t-label` small caps, ink;
  - YOLO: the 16 px amber warning icon;
  - bang: "!" in `--face-mono` 700.
  - The text starts `calc(8 * var(--px))` after the mark.
- **Chip row** between the composer rule and the field: chips `calc(30 * var(--px))` tall, 1px `--rule-control`, padding `0 calc(10 * var(--px))`, a 16 px icon, the name in `--t-small`, a 16 px ✕; gaps `calc(8 * var(--px))`; `calc(10 * var(--px))` above and below. Kept skills show "SKILL" in small caps before the name.
- **Tray** above the composer rule, only while it has something (Q7):
  - a 1px `--rule-faint` top;
  - one header line per section: "TO-DO 1/4" or "QUEUE 2" in `--t-label`, the active step or "waiting · sends next" in `--t-small` slate, and the hint at the right;
  - an open section lists its items in `--t-small`;
  - the whole tray is at most 40% of the timeline's height and scrolls.
- **Slot** (permission prompt, plan handoff, Customize Composer, later question forms):
  - It takes the field-and-send row's place and grows upward to at most 60% of the middle column (full column for F).
  - Box: 1px `--rule-control`, `--paper-raised`.
  - Title line: "PERMISSION REQUIRED" in `--t-label`, then the tool name in `--t-body-small`.
  - Content: CHAT-style bodies and DiffView, scrolling.
  - Buttons at the bottom right: `calc(44 * var(--px))` tall, `--t-body-small`, 1px `--rule-control`. The selected one has a 1px ink border, a `--band` fill and its key letter underlined (A, S, D). No vermilion and no red: Deny is a normal button.
  - Hint line under the content in `--t-small` slate: "a allow · s allow for session · d deny · t split/unified · f full screen".

### 4.10 Streaming and thinking states

- **Activity mark** `line`: three round dots of `calc(4 * var(--px))` in ink, `calc(6 * var(--px))` apart; each fades 0.25 → 1 → 0.25 over 1.2 s, 0.2 s apart (CSS only). `small`: 3 px dots, 3 px apart.
- With `prefers-reduced-motion: reduce` the dots stay at full ink.
- **Status line:** the mark, the label in `--t-small` slate, " · 12s" in `--t-stamp` slate, then the thought peek in italic slate cut with "…".
- **Streaming text** just grows: no cursor, no fade-in, no typing effect.
- **Thinking** shows only the status line, never reasoning text.

### 4.11 Color use for states

- Errors use `--error-ink`; warnings use `--amber-ink` for text and `--amber` for icons.
- Success uses an ink check: no green, because green as a main color is banned and success should be quiet.
- Pending uses the activity mark: the terminal's soft green is dropped.
- Added and removed lines use the diff tints.
- Nothing new uses vermilion.

### 4.12 Empty states, errors, "crush too old", the engine gate

- **Engine gate** (2.9): a block centered in the middle column, max width `calc(560 * var(--px))`:
  - title in `--t-figure`;
  - body in `--t-reading`;
  - fix line in `--t-body-small` slate;
  - one "Try again" button in the New chat button's look (`ChatList.svelte:98-116`).
  - Titles: "Crush not found", "Crush is too old", "Crush could not open this project".
  - The left and right columns still draw, empty.
- **Empty states:**
  - No chats: "No chats yet" in `--t-small` slate under the search rule.
  - Search without a match: "No chats match “x”."
  - Panel sections: "None", the terminal's word.
  - A chat that fails to load: "Couldn't load this chat: <message>" with a "Try again" text button.

### 4.13 Right panel additions

- **Meter detail popover** on hover or focus of the meters: `--paper-raised`, 1px `--rule-control`, padding `calc(12 * var(--px)) calc(16 * var(--px))`, aligned with the panel's left padding.
  - Lines in `--t-small`: "Context 38% left · ~124k/200k tokens", "5h 14% left · resets 2h41m", "Weekly 69% left · resets …".
  - Then a "Customize…" text button.
- **Meter strip** (panel hidden), in the composer's bottom padding under the field (25 picture px):
  - per meter: the small-caps label, a `calc(60 * var(--px))` × `calc(4 * var(--px))` bar (1px `--meter-edge`, `--meter` fill), the percent in `--t-small`, the warning icon when it applies;
  - meters `calc(20 * var(--px))` apart, aligned with the field's left edge.
- **Sections** FILES, LSP, MCP, SKILLS:
  - the PROJECT label style, rows in `--t-small`, separated by the caret rule;
  - file counts as in the tool rows;
  - LSP and MCP counts with 12 px error (cross, `--error-ink`) and warning (triangle, amber) icons.

### 4.14 Notices, help sheet, details overlay

- **Notice line:** `calc(40 * var(--px))` tall over the timeline's bottom, sitting on the composer rule.
  - `--paper-raised`, 1px `--rule-faint` top, padding `0 calc(23 * var(--px)) 0 calc(66 * var(--px))` (aligned with the timeline text).
  - A 16 px icon per kind, the text in `--t-small` cut with "…", an action text button at the right when present.
- **Help sheet:** over the timeline's bottom, at most half its height; `--paper-raised`, a top rule with + marks.
  - Three columns of key and action pairs: the key in `--t-stamp`, the action in `--t-small`.
  - Group labels in `--t-label` small caps.
- **Details overlay:** drops from under the head rule over the timeline; `--paper-raised`, a bottom rule with + marks.
  - A header line: title, model info, version at the right.
  - Then four columns: Files, LSPs, MCPs, Skills.

---

## 5. Build pieces and the scaffold

### 5.1 Scaffold S3 (the coordinator commits it first)

**When.** After engine pieces A and D are merged into `desktop`. S3's Rust uses `batch.rs` (D), and its wire types carry the 5.1 fields (A).

**What it holds.** Every shared name, every frozen file, and empty bodies for the rest, so all pieces compile from the first commit.

| File | S3 content |
|---|---|
| `src-tauri/Cargo.toml` | the dependencies of 1.2 |
| `src-tauri/src/main.rs` | flags (`--version`, `--probe`, `--fixture`, `--cwd`/`-c`, `-s`/`--session`, `-C`/`--continue`, `-D`/`--data-dir`, `-d`/`--debug`, `-H`/`--host`, `-y`/`--yolo`, `--channels`), managed `AppState`, the full `generate_handler!` list of 1.3, the close hook |
| `src-tauri/src/{error,state,commands}.rs` | complete (1.2.2, 1.2.9, 1.3) |
| `src-tauri/src/fixture.rs` | complete: test-only `--fixture FILE` mode (section 6.3) |
| `src-tauri/src/{crush_bin,engine_cli,uds_http,sse,connection,login_env,settings,clipboard,opener,files}.rs` | the public signatures of 1.2, bodies `Err(AppError::not_built(..))` |
| `src-tauri/capabilities/default.json` | unchanged unless `cargo check` and the smoke test show a missing permission (inferred that `core:default` covers events and windows, and app commands need none) |
| `package.json` | add `marked` 18.1.1, `dompurify` 3.4.17, `katex` 0.19.0, `mermaid` 12.2.0, `highlight.js` 11.11.1; dev `jsdom` at its current release, pinned exactly (the version goes in the PLAN progress record) |
| `vite.config.js` | Vitest `environmentMatchGlobs` so `src/lib/render/**` tests run in `jsdom` |
| `src/app.css` | the tokens of 4.0 |
| `src/App.svelte` | final layout: the three columns, the engine gate host, the overlay host |
| `src/components/Conversation.svelte` | final shell: ChatHead, Timeline, NoticeLine, Tray, Composer, MeterStrip in their places |
| `src/components/{ChatHead,timeline/Timeline,tools/ToolGroup,composer/Composer}.svelte` | today's markup and CSS moved out of `Conversation.svelte` unchanged |
| `src/components/overlay/OverlayHost.svelte`, `Icon.svelte` | final: OverlayHost renders menu, viewer, sheet, help, details and the open dialog; Icon gains check, cross, dot, gear, diamond, info, update, close, image, file, text, skill, half-circle, circle, offline |
| one placeholder `.svelte` per component named in section 2 | an empty element with the right props, so every import resolves |
| `src/main.js`, `src/lib/plugins.js`, `src/lib/engine/{wire,commands,events}.js`, `src/lib/{keys,actions,dialogs,menu,decorations,probe}.js`, `src/lib/stores/ui.svelte.js` | complete (1.4, 1.5, 1.8, 1.9, 1.10) |
| `tests/fixtures/picture.json` | the approved picture's chat (from `src/lib/sample.js`) as a fixture, for the look checks |
| the other store files of 1.5.1 | the exports of 1.5.3 and 3.3 with empty bodies |
| `tests/sandbox.sh` | also passes `DESKTOP_CRUSH` and `DESKTOP_ARGS` through `env -i` (today only `DESKTOP_APP`, `tests/sandbox.sh:37`) |
| `tests/run-in-sandbox.sh` | copies `tests/project/` to `/tmp/home/proj`, runs `git init`, writes its `crush.json` with the fake provider's port, sets `CRUSH_DESKTOP_CRUSH=$DESKTOP_CRUSH`, and starts the app with `--cwd /tmp/home/proj $DESKTOP_ARGS` |
| `tests/check.sh`, `tests/build-crush.sh` | section 6.1 |
| `tests/driver/harness.py` | shared helpers: `probe_section`, `word_rect`, `select_words`, `right_click`, `type_text`, `key`, `wait_text`, `engine_get` (curl over the socket), `fake_ai_requests`, `step(row)` (names the screenshot) |
| `tests/project/` | the throwaway project template: `README.md`, `auth/session.go`, `crush.json.in` (the fake `openai-compat` provider with models `fake-small` and `fake-large`, `options.notifications: "disabled"`) |

**Frozen after S3.** These files are frozen for the rest of phases 3 and 4. A piece that needs a change asks the coordinator, who changes it on `desktop` and every piece merges it:

- Rust: `main.rs`, `error.rs`, `state.rs`, `commands.rs`, `fixture.rs`, `Cargo.toml`;
- build config: capabilities, `tauri.conf.json`, `package.json`, `vite.config.js`;
- styles and shell components: `app.css`, `App.svelte`, `Conversation.svelte`, `OverlayHost.svelte`, `Icon.svelte`;
- shared JS: `main.js`, `plugins.js`, `wire.js`, `commands.js`, `events.js`, `keys.js`, `actions.js`, `dialogs.js`, `menu.js`, `decorations.js`, `probe.js`, `ui.svelte.js`;
- harness: `sandbox.sh`, `run-in-sandbox.sh`, `check.sh`, `harness.py`.

**S3's own check.**

1. `pnpm test` and `cargo test` pass.
2. `tests/build.sh` builds.
3. The look checks give the same scores as before the split: `look_real.py`, then `tests/look/align_real.py` on its capture. The app runs on the fixture `tests/fixtures/picture.json`, made from `src/lib/sample.js`.
4. Every Tauri command answers through `invoke` (`not_built` is a valid answer).

### 5.2 Pieces

Each piece is built in its own worktree from `desktop` after S3. "Owns" lists the only files it may create or change; "Must not touch" is everything else, and especially the frozen files. Fixture checks run in the piece's worktree; engine checks run on `desktop` after the merges.

| Piece | Rows | Owns | Needs | Done-checks |
|---|---|---|---|---|
| P3.1 Rust engine client | START-006, START-008 to START-013 (flag effects), SIDE-030 (events), 2.9 gate and reconnect | `src-tauri/src/{crush_bin,engine_cli,uds_http,sse,connection,login_env}.rs`, `tests/fakes/{crush-old,crush-wrap}` | S3; engine A, D | R: resolve order; version texts; ensure JSON and error JSON; status → code mapping; SSE lines split across chunks; one doorbell per batch; backoff 250 ms doubling to 10 s, failed after 3; rejoin order; calls while reconnecting give `engine_unavailable`; route list. W `p3_connect.py`, `p3_reconnect.py` (engine) |
| P3.2 Rust services | CHAT-090, CHAT-102, TYPE-010 to TYPE-014 (file side), phase 4's closed set | `src-tauri/src/{settings,clipboard,opener,files}.rs` | S3 | R: atomic write keeps unknown keys; closed set per `data_dir`; `classify` cases of `links.go:19-93`; probe mode writes `opened.jsonl`; `files::list` limits. W CHAT-102 links step (fixture) |
| P3.3 Stores and events | LIST-034 to LIST-037, TYPE-042 to TYPE-049 (state side), SIDE-007, SIDE-008 data, CHAT-026 | `src/lib/engine/api.js`, `src/lib/stores/{connection,sessions,messages,workspace,limits,prompts}.svelte.js`, `src/lib/agents.js`, their tests | S3; engine A (5.1 fields), B and C event shapes | U `messages.test.js` (load buffer and replay, duplicates, protected fields, interrupted runs, finished-wins), `sessions.test.js`, `limits.test.js`, `api.test.js` (paths and bodies). W `p3_connect.py` steps that need only stores (engine) |
| P3.4 Renderers | CHAT-004, CHAT-014, CHAT-017, CHAT-018, CHAT-033, CHAT-045, safety (1.7) | `src/lib/render/*`, `src/components/render/{MarkdownView,VisualChip,FigureView}.svelte`, their tests, `tests/fakes/scripts/unsafe.json`, `tests/driver/p3_safety.py`, `tests/fixtures/unsafe-*.json` | S3 | U `markdown`, `sourcemap`, `incremental`, `sanitize` (the 2.9 safety corpus), `math`, `code`, `ansi`, `usertext`, `plan`; the source-tree grep for `innerHTML`. W `p3_safety.py` (fixture first, engine after) |
| P3.5 Timeline | CHAT-001 to CHAT-037 (items), CHAT-087 to CHAT-101, CHAT-103 to CHAT-105, the menu | `src/components/timeline/*` (including `Timeline.svelte` after S3's move), `src/lib/{timeline,vlist,selection,copy,timefmt,thought}.js`, `src/lib/plugins/timeline.js`, `tests/driver/{p3_chat,p3_long}.py`, `tests/fixtures/chat-*.json`, `tests/fakes/scripts/{basic,markdown,errors,plan,long}.json` | S3; P3.4 for rich text in window checks | U `timeline`, `vlist`, `copy`, `timefmt`, `thought`. W `p3_chat.py`, `p3_long.py` on fixtures, then on the engine |
| P3.6 Tool views, diffs, Changes sheet | CHAT-038 to CHAT-086, CHAT-107 to CHAT-113, DLG-294 to DLG-311 | `src/components/tools/*` (including `ToolGroup.svelte` after S3's move), `src/components/sheets/ChangesSheet.svelte`, `src/lib/tools/*`, `src/lib/diff.js`, `tests/driver/p3_tools.py`, `tests/fixtures/tools-*.json`, `tests/fakes/scripts/{tools,agent}.json` | S3; P3.4 (code, ANSI) | U `registry`, `status`, `summary`, `copy`, `sniff`, `shell`, `todos`, `diff`. W `p3_tools.py` |
| P3.7 Composer | TYPE-001 to TYPE-061, SIDE-025, SIDE-026, SIDE-031 to SIDE-034, DLG-119 to DLG-140 | `src/components/composer/*` (including `Composer.svelte` after S3's move), `src/lib/composer/*`, `src/lib/stores/{composer,queue,todos}.svelte.js`, `src/lib/plugins/composer.js`, `tests/driver/{p3_composer,p3_permissions}.py`, `tests/fixtures/{composer-*.json,dot.png}` | S3; P3.6's DiffView for permission diffs (until it merges, the prompt shows plain text) | U `history`, `paste`, `mentions`, `skillwords`, `modes`. W `p3_composer.py`, `p3_permissions.py` |
| P3.8 Panel and overlays | SIDE-001 to SIDE-024, SIDE-027 to SIDE-030 (look), SIDE-035, visuals, viewer | `src/components/SidePanel.svelte`, `src/components/panel/*`, `src/components/{NoticeLine,HelpSheet}.svelte`, `src/lib/stores/visuals.svelte.js`, `src/lib/{footer,compaction,modified}.js`, `src/lib/plugins/panel.js`, `tests/driver/p3_side.py`, `tests/fixtures/side-*.json`, `tests/fakes/scripts/visuals.json` | S3; P3.4 (figures) | U `footer`, `compaction`, `modified`. W `p3_side.py` |
| P3.9 List, head, start | START-001 to START-021, LIST-012 (states), LIST-033, LIST-034 (screen), CHAT-001 (landing) | `src/components/ChatList.svelte`, `src/components/list/*`, `src/components/ChatHead.svelte` (after S3's move), `src/components/start/*`, `src/lib/{listmodel,sessionid}.js`, `src/lib/plugins/list.js`, `tests/driver/p3_start.py`, `tests/fixtures/list-*.json` | S3 | U `listmodel` (nesting-ready, 3.3), `sessionid`. W `p3_start.py` |
| P3.10 Fakes and engine-wide drivers | 2.9 (two apps, speed), the fake tools' real formats | `tests/fakes/fake_ai_server.py` (the script loader of 6.2), `tests/fakes/scripts/stream2m.json`, `tests/fakes/fake-tool`, `tests/fakes/streams/*`, `tests/fakes/{fake_mcp,fake_lsp,seed_chat}.py`, `tests/driver/{p3_two_apps,p3_speed,clip}.py` | S3; engine A-D merged | `fake_ai_server.py --self-test` plays every script in `scripts/`; its drivers pass on the merged phase 3 |
| P4.1 Branching UI | 3.1 to 3.3, B1 to B16 | `src/lib/branch.js`, `src/lib/stores/{links,closed,tree}.svelte.js` (bodies), `src/components/list/*` and `ChatHead.svelte` (phase 3 is done by then), `src/lib/plugins/branch.js` (registers the menu item, the decoration source and its click handler), `tests/driver/{p4_branch,p4_restart,p4_mid_reply,p4_two_apps}.py` | phase 3 merged; engine C | U `branch.test.js` (titles, quotes, UTF-16 cuts after emoji, overlaps with touching ranges, `canBranch`), `links.test.js`. W the four drivers |
| P4.2 Go forkers | 3.4, B17 | `internal/agent/cliagent/{fork,fork_claude,fork_codex,fork_acp}.go`, their tests, the hook in `internal/agent/cli_turn.go`, `tests/fakes/fake-tool` fork modes, `tests/driver/p4_fork_args.py` | engine C; P4.3's findings | G the tests of 3.4 through `tests/go-test.sh ./internal/agent/...`, then the whole suite and the race detector on `agent`; W `p4_fork_args.py` |
| P4.3 Live checks | 3.5 | `tests/live/*.sh`, `docs/evidence/phase4/live-*` | none; runs first in phase 4 | the evidence files exist with every step's output |
| P4.4 Tree and restore dialogs | DLG-089 to DLG-112 | `src/components/tree/*`, `src/lib/tree.js`, `src/lib/plugins/tree.js`, `tests/driver/p4_tree.py` | phase 5's palette and shared dialog frame (t32) | U `tree.test.js`; W `p4_tree.py` |

**Order.**

- Phase 3: S3; then P3.1 to P3.10 in parallel. Merge P3.1, P3.2, P3.3 and P3.10's fake server first (the engine path and the script loader every engine check needs); then P3.4 to P3.9 in any order. P3.10's own drivers (two apps, speed) run last, on the merged branch. Phase 3 is done when every driver of 2.2 to 2.9 passes on `desktop`.
- Fake scripts belong to the piece whose driver uses them (listed in "Owns"); any piece may read another's script.
- Phase 4: P4.3 first (PLAN.md:112); then P4.1 and P4.2 in parallel; P4.4 when phase 5's palette lands.

**Hot spots and how they are kept apart.**

- **Stores:** one owner per store file; registration hooks (`onEvent`, `onResync`) mean no store edits another.
- **Types:** `wire.js` is frozen.
- **Command names:** `commands.rs` and `commands.js` are frozen.
- **The page:** `App.svelte` and `Conversation.svelte` are frozen shells with every child's place fixed.
- **Rust entry:** `main.rs` is frozen.
- **Capabilities and libraries:** capabilities and `package.json` are frozen with every phase 3-4 library already added. A piece needing another library asks the coordinator.

---

## 6. Tests and harness

### 6.1 Exact commands

All commands run from the piece's worktree (`<WT>` = its absolute path), in the background through `crush bg`, inside `systemd-run` on CPUs 6-11 and 18-23 with a time limit (PLAN.md:151, 236; `--working-directory` because `systemd-run --user` ignores the shell's folder, PLAN.md:266).

```sh
# Unit tests (Vitest)
crush bg -- 'systemd-run --user --wait --pipe --collect -p AllowedCPUs=6-11,18-23 -p RuntimeMaxSec=600 --working-directory=<WT>/desktop -E PATH="$PATH" pnpm test'

# Rust tests (need desktop/dist: build the screens first)
crush bg -- 'systemd-run --user --wait --pipe --collect -p AllowedCPUs=6-11,18-23 -p RuntimeMaxSec=1200 --working-directory=<WT>/desktop -E PATH="$PATH" bash -c "pnpm build && cargo test --manifest-path src-tauri/Cargo.toml"'

# Build the app (existing script)
crush bg -- 'systemd-run --user --wait --pipe --collect -p AllowedCPUs=6-11,18-23 -p RuntimeMaxSec=1800 --working-directory=<WT>/desktop -E PATH="$PATH" bash tests/build.sh'

# Build the engine (the crush binary) from the worktree into tests/out/bin/crush
crush bg -- 'systemd-run --user --wait --pipe --collect -p AllowedCPUs=6-11,18-23 -p RuntimeMaxSec=900 --working-directory=<WT>/desktop -E PATH="$PATH" bash tests/build-crush.sh'

# One window driver against the engine
crush bg -- 'systemd-run --user --wait --pipe --collect -p AllowedCPUs=6-11,18-23 -p RuntimeMaxSec=900 --working-directory=<WT>/desktop -E PATH="$PATH" bash tests/check.sh p3_chat.py'

# One window driver against a fixture (no engine)
crush bg -- 'systemd-run --user --wait --pipe --collect -p AllowedCPUs=6-11,18-23 -p RuntimeMaxSec=600 --working-directory=<WT>/desktop -E PATH="$PATH" bash tests/check.sh --fixture tests/fixtures/chat-basic.json p3_chat.py'

# Go tests for P4.2 (existing runner, committed work only)
crush bg -- 'bash <WT>/desktop/tests/go-test.sh ./internal/agent/...'
```

**`tests/build-crush.sh`** (S3): `go build -o desktop/tests/out/bin/crush .` from the worktree root, the same path the coordinator used for the terminal app (PLAN.md:294).

**`tests/check.sh [--fixture FILE] [--size WxH] DRIVER.py [driver args]`** (S3) runs these steps:

1. Fail when `src-tauri/target/release/crush-desktop` or (without `--fixture`) `tests/out/bin/crush` is missing.
2. Run `DESKTOP_CRUSH=<abs>/tests/out/bin/crush DESKTOP_ARGS="<--fixture FILE>" tests/sandbox.sh -s WxH tests/run-in-sandbox.sh DRIVER.py args`.
3. Run `tests/outside-check.py` before and after, as the self-test does.
4. Exit with the driver's code; the driver's PASS/FAIL lines and screenshots land in `tests/out/<driver>/`.

### 6.2 Fake AI server and scripts (P3.10)

`tests/fakes/fake_ai_server.py` keeps its OpenAI-style API and request log (`FAKE_AI_LOG`) and gains scripts.

**Script shape:**

```json
{
  "turns": [
    {"when": "substring of the last user message",
     "steps": [
       {"delay_ms": 10, "reasoning": "optional thinking text",
        "text": "streamed word by word",
        "tool_calls": [{"name": "bash", "arguments": {"command": "printf 'a\\nb'"}}],
        "finish": "stop | tool_calls | length | content_filter",
        "error": {"status": 500, "body": "…"},
        "usage": {"prompt_tokens": 1200, "completion_tokens": 300}}
     ]}
  ],
  "default": {"text": "OK."}
}
```

- **Picking the turn and step.** The server stays stateless: it takes the turn whose `when` matches the last user message, and the step whose index equals the number of assistant messages after that user message. Helper chats (Agent tool) get their own `when`.
- **Streaming.** `delay_ms` spaces the chunks; `text` streams in chunks of 1-4 words; `tool_calls` stream as OpenAI deltas.
- **Local files.** `GET /file.txt` and `GET /page.html` are served for Download and Fetch. Every other path is logged as a beacon, for the safety check.

**Scripts:** `basic`, `markdown`, `tools` (view, edit, multiedit with one bad edit, write, bash with colors and a progress bar, grep, glob, ls, fetch, download, todos, MCP `echo`, a failing last tool), `errors` (500, 429 once, content filter), `visuals`, `unsafe`, `plan`, `agent` (Agent and Agentic Fetch), `stream2m`, `long`.

**Fake AI tools.** `tests/fakes/fake-tool` (claude, codex, grok, opencode, agy) keeps logging `{tool, args, cwd, at}` and also logs its stdin lines.

- It replays a recorded stream per tool from `tests/fakes/streams/<tool>/<name>.jsonl`, chosen by the prompt's first word, then waits for stdin to close, as the Go fake does (`internal/agent/cliagent/cliagent_test.go:20-28`).
- Claude's stream includes `uuid` fields; Codex's includes turn IDs; Grok's is ACP.
- The streams are written from the formats the Go fakes already pin (`cliagent_test.go:43-63` for Claude), not from real sessions, so no real chat is read.
- P4.2 adds fork modes: `--fork-session`, `thread/fork`, `session/fork`.

**Other fakes:**

- `fake_mcp.py`: a stdio MCP server with tool `echo`, one resource and one channel push.
- `fake_lsp.py`: answers `initialize`, then publishes one error and one warning on `didOpen`.
- `seed_chat.py`: makes N turns through the engine's API with `curl --unix-socket`, using the fake server's instant replies.
- `crush-old`: prints "unknown command" and exits 1.
- `crush-wrap`: runs the real crush but fails `engine ensure` while `/tmp/fail-ensure` exists.

### 6.3 Fixture mode (S3)

`--fixture FILE` replaces the engine connection with a player, so UI pieces are checked in the real window before the engine client exists, and look checks get fixed data.

**File shape:**

```json
{
  "workspace": { /* proto.Workspace */ },
  "agent": { /* proto.AgentInfo */ },
  "get": {"sessions": [ … ], "sessions/S1/messages": [ … ], "config": { … }},
  "events": [{"at_ms": 0, "entries": [{"kind": "event", "type": "message", "event": "updated", "payload": { … }}]}]
}
```

**How it plays.**

- `engine_open` answers from `workspace` and pushes a `connected` connection event.
- `api` GET answers `get[path]` (404 `not_found` when absent).
- Every other call is appended to `<probe>.api.jsonl` and answered with `null` (202 for `agent`).
- The entries of `events` are pushed into the window's batcher at their `at_ms` after `engine_open`, through the same doorbell, so the frame loop and stores run as they do with the engine.

Fixtures are JSON made by hand or recorded from a real run. A recording option in the probe saves the stores' snapshot and the batch stream, but it runs only in the sandbox (never on real chats).

### 6.4 The terminal app in a hidden tmux (two-app checks)

Inside the sandbox, after the desktop is connected:

```sh
tmux -L crushtest -f /dev/null new-session -d -s tui -x 200 -y 50 \
  "env CRUSH_CLIENT_SERVER=1 $DESKTOP_CRUSH --cwd /tmp/home/proj -s $SID"
tmux -L crushtest capture-pane -p -t tui     # read its screen as text
tmux -L crushtest send-keys -t tui "second from terminal" Enter
```

- **Ground truth:** `curl --unix-socket $SOCK http://crush/v1/workspaces/$WS/sessions/$SID/messages`, with the socket from the probe's `connection.engine.socket`.
- **Tear-down:** `tmux -L crushtest kill-server` at the end. This is a dedicated tmux socket inside the sandbox, so it only stops this test's server.

### 6.5 What each done-check runs

| Check | Command | Passes when |
|---|---|---|
| Phase 3 unit | `pnpm test` | all files pass, including the `innerHTML` grep |
| Phase 3 Rust | `cargo test` | all pass |
| Phase 3 window | `tests/check.sh` with `p3_connect`, `p3_chat`, `p3_tools`, `p3_composer`, `p3_permissions`, `p3_side`, `p3_start`, `p3_long`, `p3_safety`, `p3_reconnect`, `p3_two_apps`, `p3_speed` | every step PASS and every row's screenshot present |
| Phase 3 look | `look_real.py` and `align_real.py` on the picture fixture; the design detector at three zooms (PLAN.md:157) | scores not below phase 2's; no new detector findings |
| Phase 4 | P4.3 evidence; `go-test.sh ./internal/agent/...` then the whole suite; `p4_branch`, `p4_restart`, `p4_mid_reply`, `p4_two_apps`, `p4_fork_args` | all pass; evidence files complete |
| After every merge | the whole phase's list on `desktop` | as above |

---

## 7. Risks

| # | Risk | What contains it |
|---|---|---|
| R1 | WebKitGTK on a hidden software screen may not reach 20 ms frames during long streams | batching (1.2.5), virtual list and incremental Markdown (1.6), heavy work in idle slices; `p3_speed.py` measures the real window three times |
| R2 | Mermaid may need something the CSP blocks (for example `eval` in a parser) | the safety check fails loudly; the fallback shows the source; the fix would be a worker-free build option, never loosening the CSP (open question O6) |
| R3 | Hidden or changed fork flags (Claude's `--resume-session-at`, Codex `thread/fork`, Grok's fork extension) | the live checks first (P4.3); every fork error falls back to the written copy; `TestForkFailureFallsBack` |
| R4 | The source map can be wrong for unusual Markdown (nested emphasis in links, HTML blocks, reference links) | branching uses it, so a wrong map gives a `bad_cut` refusal rather than a wrong branch (the engine checks `t[start:end] == text`); unit cases from the terminal's copy tests |
| R5 | Login-shell environment capture can hang or print noise | the 5 s limit and the marker; fallback to the app's own environment |
| R6 | The `@` file list may not match the terminal's ignore rules exactly | the builder compares with `internal/fsext` and notes differences |
| R7 | Model, main agent (plan or code) and YOLO are project-wide in the engine, so one app's change reaches the other (`engine-design.md:1525`); LIST-035's model restore on chat switch can flip the other app's model | the same as the terminal in client mode today; `p3_two_apps.py` step on two chats with different models shows the effect; the owner decides whether a per-chat model belongs in a later engine change (open question O9) |
| R8 | Auto-copy keeps the selection, unlike the terminal | needed for branching; recorded (D8) |
| R9 | A real provider may refuse a branch that ends with an unanswered user message | engine-design.md:2357 covers the request builder |
| R10 | Fixture mode can drift from the real engine's answers | engine drivers run after every merge; fixtures recorded from real runs where possible |
| R11 | The Changes button and row-click split (D10) differs from the terminal | both features stay; the help sheet names both |

---

## 8. Decisions made (each can be changed in the morning)

| # | Decision | Other option | To change |
|---|---|---|---|
| D1 | Engine data reaches the window only through `events_take`, once per frame, after a doorbell | push every event as a window event (rejected: no frame control) | `connection.rs`, `events.js` |
| D2 | The app's connection states travel through the batcher as the barrier type `connection`; `connected` with `resync: true` stands for "push one Resync" | a separate window event (rejected: loses order against engine events) | `connection.rs` |
| D3 | Rejoin order: version, create (joins a live workspace or makes one), events, agent init, current session; a changed workspace ID means a fresh engine. The engine design reopens the stream first and reads 404 as fresh (`engine-design.md:2852-2853`); both end in the same state | the engine design's order | `connection.rs` |
| D4 | Placeholders follow the terminal (TYPE-003), so with YOLO on (the default) the field says "Go crazy"; the picture's "Ask Claude…" was sample text | "Ask <agent>…" when ready | `lib/composer/modes.js` |
| D5 | Errors use a new oxblood `--error-ink`; vermilion stays for keys and send | vermilion errors (rejected by the picture's rule) | `app.css` |
| D6 | No green for success or pending; ink checks and the activity mark | the terminal's soft green | `app.css`, ToolHeader |
| D7 | "Copy whole message" copies what `c`/`y` copies | Markdown source | `lib/copy.js` |
| D8 | Auto-copy keeps the selection | clear it as the terminal does (breaks right-click branching) | Timeline |
| D9 | The tray's queue gets a "Clear" button | phone only | QueueSection |
| D10 | The group row opens on click (brief); a "Changes" button opens the sheet | the terminal's count click | ToolGroup |
| D11 | The panel hides under 1,060 picture px of width or 640 of height, or with `compact_mode` | other thresholds | `App.svelte` |
| D12 | The chat head holds the title and the folder · branch line; the model is in the model button; the panel holds project, meters and sections | a sidebar copy of the terminal's | ChatHead, SidePanel |
| D13 | New chat does not stop LSPs or end background work (project-wide in the engine) | the terminal's stops | `sessions.newChat` |
| D14 | "Quote in reply" quotes every line; the branch draft keeps the demo's one-line quote | the demo for both | `lib/branch.js` |
| D15 | Closing the open chat opens the first visible chat, else the landing page | always landing | `closed.svelte.js` |
| D16 | Rail keys: A is the chat's start (the rail's dot); keys run B, C, … | letters from A on the first key | `timeline.js` |
| D17 | Tables, display math, Mermaid and pictures become chips plus panel figures; inline while the panel is hidden | tables inline in the chat | `render/markdown.js` |
| D18 | Prose 540 picture px wide; code, diffs and tools full width | one width for all | ReplyItem CSS |
| D19 | A test-only fixture mode lets UI pieces be checked before the engine client | wait for the engine client | `fixture.rs` |
| D20 | Tree and restore dialogs are built after phase 5's palette, their only entry point | add a new entry point | P4.4 |
| D21 | `--host` joins a given engine with no `ensure` | refuse the flag | `connection.rs` |
| D22 | Clipboard writes go through Rust; paste stays the web view's event | web clipboard API | `clipboard.rs` |
| D23 | Our own line diff | add the `diff` package | `lib/diff.js` |
| D24 | Permission buttons: the selected one in ink and band, never vermilion or red | a red Deny | PermissionPrompt CSS |
| D25 | Customize Composer opens from the meters' popover in phase 3 (the palette adds its entry in phase 5) | palette only | MeterDetail |
| D26 | The tray is a tab stop; ←/→ inside it switch sections | the terminal's keys | `keys.js` |
| D27 | The chat head stays in compact layout | hide it | Conversation |
| D28 | Ctrl+C copies when there is a selection (else it is phase 5's quit key) | always quit | `keys.js` |

---

## 9. Open questions (each has a built default)

| # | Question | Default |
|---|---|---|
| O1 | No route gives a chat's agent for the list rows | use `context_budget.provider` (5.1(a)) or the last loaded model; rows with neither show no agent line |
| O2 | highlight.js version | 11.11.1 (inferred); S3 records what installs |
| O3 | Hyper credits need an engine route (the terminal fetches them itself, `ui/model/polling.go:31-82`) | not shown until phase 5's Hyper work adds one |
| O4 | Whether OpenCode has a fork call | `ErrNoFork` unless P4.3 confirms one |
| O5 | Whether Grok's fork at a prompt index keeps or drops that prompt | P4.3 decides; the note covers either |
| O6 | Whether Mermaid 12.2.0 runs under the CSP without `unsafe-eval` | the safety check decides; fallback shows source |
| O7 | Whether Mermaid 12.2.0 has `look: "neo"` | `classic` if not |
| O8 | jsdom version for renderer tests | the current release, pinned by S3 |
| O9 | Should model, plan mode and YOLO become per chat or per app in a later engine change (R7) | stay project-wide, as the terminal in client mode |
| O10 | The YOLO mark is always on by default (YOLO default on); hide it in that state? | show it, as the terminal does |

---

## 10. Where the code or the plan contradict each other

| # | Finding | Handling |
|---|---|---|
| C1 | TYPE-015 is a typing-box row, but its desktop version needs terminal tabs (PLAN.md:131), which are phase 6 | moved to phase 6 |
| C2 | TYPE-031 to TYPE-034 describe terminal code marked uncommitted; `internal/ui/model/skill_mentions.go` is not on `desktop` | the checklist text is the spec; the attachment form copies the palette's committed one |
| C3 | START-005 animates the logo; the owner's wordmark is iron black on paper (PLAN.md:244) | no animation |
| C4 | CHAT-084 opens the Changes sheet from the count; the brief says the folded line "opens on click" (PLAN.md:14) | D10 |
| C5 | TYPE-003's placeholders against the picture's "Ask Claude…" | D4 |
| C6 | CHAT-031's rounded plan card is a banned look | square box |
| C7 | Branch cuts are offsets into the stored Markdown (`engine-design.md:2151,2177`), while people select drawn text | the renderer's source map |
| C8 | Permission prompts are DLG rows (phase 5's table) but Q8 and PLAN.md:292 put them here | built in phase 3 |
| C9 | The sandbox's `env -i` passes only `DESKTOP_APP` (`tests/sandbox.sh:29,37`), so `CRUSH_DESKTOP_CRUSH` never reaches the app | S3 passes `DESKTOP_CRUSH` and sets the variable inside |
| C10 | Ctrl+K (Sessions search, PLAN.md:103) and Ctrl+C (copy) differ from the terminal's line and quit keys | by the plan; D28 |
| C11 | The forkers are Go agent work that the engine design left to phase 4 (`engine-design.md:2254-2268`) | P4.2 is a Go piece inside phase 4 |
| C12 | `engine-design.md:2367` and `:2626` cite `handlers_workspace.go:121-181`; the events handler is now at `internal/server/handlers_workspace.go:130-209` | cite the new lines |
| C13 | CHAT-014 draws tables in the chat; brief 14 puts tables in the visuals panel | D17 |
| C14 | The batcher takes only events (`push(ev: Event)`), so "push one Resync" (`engine-design.md:2855`) cannot be done by `push` | D2 |
| C15 | CHAT-091's auto-copy clears the selection; branching needs it (PLAN.md:99) | D8 |
| C16 | LIST-033's new chat stops LSPs and ends background processes; in the shared engine they belong to the whole project | D13 |
| C17 | PLAN.md:51 names a crush-path setting, but Settings arrives in phase 5 | phase 3 accepts `CRUSH_DESKTOP_CRUSH` and an existing `crush_path` key in the settings file; the screen comes in phase 5 |

---

## 11. Not in scope (ideas only)

- Code-block "Copy" buttons in replies (the terminal has none; copy works through selection and `c`/`y`).
- Previous and next picture keys in the image viewer.
- Per-tool durations in open tool groups.
- A "Reveal in files" action for file links.
- Pausing a notice's timer while hovered.
- A per-chat or per-app model (O9) and the engine change it needs.
