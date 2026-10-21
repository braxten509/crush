# Crush Desktop: engine design for phase 1

This document is the build contract for the engine work in PLAN.md section 5, items 1 to 14. Four builders work from it at the same time, each in its own Git worktree. Every name, path, JSON field and file owner below is fixed. If something is missing or wrong, the builder asks the coordinator. A builder never invents a shared name on their own.

Inputs: `desktop/PLAN.md` sections 2, 3, 4, 5 (items 1-14), 6, 7, 8, 11 and 12, `desktop/CHECKLIST.md`, and the research notes `t8-connection.md` and `t6-branch-memory.md`. The code is the `desktop` branch, whose Go code matches commit 6b2146f1.

## 0. How to read this

- `file:line` means a path under `internal/` in the repository root, unless a full path is given. Line numbers are from commit 6b2146f1. `PLAN.md` and `CHECKLIST.md` lines follow those files as they stand on 2026-10-18, after the plan's look update; they move when those files change.
- **inferred** marks a claim I did not read in the code. The builder confirms each one before relying on it. Unmarked claims were read in the code.
- **MUST** and **MUST NOT** bind the builders.
- The pieces are named S (the scaffold), A, B, C and D. They are defined in "Build pieces" near the end.
- The plan's behavior is not changed anywhere, except in decision 1, which needs the owner's OK and is listed as a notice under "Open questions for the owner". Where the code makes a planned behavior impossible or clearly wrong, the problem is listed under "Where the code contradicts the plan". Choices the plan leaves open are listed under "Decisions made", with their reasons. Only a choice whose default changes behavior the owner approved, or would notice, is listed under "Open questions for the owner", with the default that is built until the owner answers. Additions the plan does not name, but that an approved item needs, are listed under "Small additions that serve approved items".

## 0.1 Conventions every piece follows

### Routes

- Every new route is an `rpc.Op` value. It lives in the owning piece's own file:
  - `internal/rpc/routes_lifecycle.go` (A)
  - `internal/rpc/routes_live.go` (B)
  - `internal/rpc/routes_tree.go` (C)
  - `internal/rpc/routes_share.go` (D)
- Each declaration uses the style of `rpc/routes.go:16-21` and `rpc/routes.go:94-113`. Example: `Op[Req, Resp]{Method: post, Path: "...", Status: ...}`.
- The server binds each route with `route(c, op, handler)…Build()` (`server/rpc.go:52-54`). The binding goes in the piece's own `internal/server/endpoints_<piece>.go`. The only exception is the secure-entry submit handler, which is hand-written (section 5.8).
- An empty `Status` means 200, as in today's routes. Routes whose response is `rpc.Empty` answer 200 with no body, like `CancelAgent` today (`server/rpc.go:83-85`).
- The paths in this document are final. No piece adds a path that is not listed here.

### Caller identity

Routes that need to know which app is calling take the `client_id` query parameter: the app's UUID, the same value used for the event stream. This is today's pattern:

- `SetCurrentSession` already takes it (`server/endpoints.go:93-101`).
- The event stream takes it too (`server/endpoints.go:119-133`).
- It is checked by `clientID(r)` (`server/handlers_workspace.go:78-87`).

The tables below mark each such route "id: required" or "id: optional".

- A required id that is missing or not a UUID fails with 400.
- An id whose app is not registered on the workspace fails with 409 `client_not_attached`.

### Errors

Error bodies stay `proto.Error`. Scaffold S adds two fields:

```go
// proto.Error (scaffold S adds Code and Field)
type Error struct {
    Message string `json:"message"`
    Code    string `json:"code,omitempty"`  // stable machine-readable code, listed below
    Field   string `json:"field,omitempty"` // secure-entry question ID for secure_field_error
}
```

The Rust side checks `code`, never the message text. Codes for today's errors (S maps them in `server/rpc.go:116-152`):

| Error (today's mapping) | HTTP | `code` |
|---|---|---|
| `badRequestError`, `ErrAgentNotInitialized`, `ErrPathRequired`, `ErrInvalidPermissionAction`, `ErrUnknownCommand`, `ErrInvalidClientID` | 400 | `bad_request` |
| `ErrWorkspaceNotFound`, `ErrLSPClientNotFound` | 404 | `not_found` |
| `ErrAgentBusy` | 409 | `agent_busy` |
| `ErrClientNotAttached` | 409 | `client_not_attached` |
| `ErrWorkspaceClosing` | 409 | `workspace_closing` |
| `ErrServerNotIdle` | 409 | `server_not_idle` |
| `ErrClientRetired` | 409 | `client_retired` |
| `ErrServerShuttingDown` | 503 | `shutting_down` |
| anything else | 500 | `internal` |

New codes, each owned by one piece. Each piece lists its errors in its own `server/endpoints_<piece>.go` (see "Build pieces", S9 and S11):

| Sentinel | Declared in | HTTP | `code` | Piece |
|---|---|---|---|---|
| `backend.ErrProtocolMismatch` | `backend/engine.go` | 426 | `protocol_mismatch` | A |
| `backend.ErrWrongProject` | `backend/engine.go` | 409 | `wrong_project` | A |
| `backend.ErrChannelOptInMismatch` (exists, today falls to 500) | as today | 409 | `channel_mismatch` | A |
| `db.ErrDataDirLocked` (exists) | as today | 423 | `project_locked` | A |
| `backend.ErrHelperNotFound` | `backend/live.go` | 404 | `helper_not_found` | B |
| `backend.ErrJobNotFound` | `backend/live.go` | 404 | `job_not_found` | B |
| `backend.ErrBadEnvName` | `backend/live.go` | 400 | `bad_request` | B |
| `backend.ErrMemoryLimitInvalid` | `backend/live.go` | 400 | `bad_request` | B |
| `agent.ErrTreeRevisionChanged` | `agent/tree.go` | 409 | `revision_conflict` | C |
| `agent.ErrTreeBusy` | `agent/tree.go` | 409 | `tree_busy` | C |
| `message.ErrBadCut` | `message/branch.go` | 400 | `bad_cut` | C |
| `message.ErrCutNotFinished` | `message/branch.go` | 409 | `cut_not_finished` | C |
| `message.ErrHighlightOverlapsLink` | `message/branch.go` | 409 | `highlight_overlaps_link` | C |
| `backend.ErrFilePlanExpired` | `backend/tree.go` | 409 | `plan_expired` | C |
| `backend.ErrNotShareOwner` | `backend/share.go` | 403 | `not_owner` | D |
| `backend.ErrShareOff` | `backend/share.go` | 404 | `share_off` | D |
| `backend.ErrActionNotFound` | `backend/share.go` | 404 | `action_not_found` | D |
| `backend.ErrSecureFormNotFound` | `backend/secure.go` | 404 | `secure_form_not_found` | D |
| `backend.ErrNotFormRecipient` | `backend/secure.go` | 403 | `not_recipient` | D |
| `backend.ErrLocalOnly` | `backend/secure.go` | 403 | `local_only` | D |
| `*secureentry.FieldError` (exists) | as today | 422 | `secure_field_error` (+ `field`) | D |

`handleError` keeps logging `err.Error()` (`server/rpc.go:110-113`). So **no error text may ever contain a secure-entry value**. Today's secure-entry errors are fixed text (`secureentry/form.go:110-112`).

### Events

The wire format stays as it is today. Each Server-Sent Events line is `data: {"type": <payload type>, "payload": {"type": "created"|"updated"|"deleted", "payload": {...}}}`, built by `envelope` (`server/events.go:185-195`) and read by `readEvents` (`client/workspaces.go:130-175`).

Events reach a stream by one of three delivery kinds:

- **Workspace-wide**: today's events from the app's event fan-in. Every attached app gets them, except permission and question prompts, which go only to the apps viewing that chat (`server/events.go:126-182`). Unchanged.
- **To all apps (engine-sent)**: new. The engine itself sends these to every live stream of the workspace, using `Backend.SendAll`.
- **To one app (engine-sent)**: new. Sent with `Backend.SendTo` to every live stream of exactly one app. These never touch the app event fan-in. Phone sharing reads that fan-in (`remote/source.go:63-65`), so the phone can never see them.

Engine-sent events (both kinds) run through the same `wrapEvent` as other events. They are not filtered again by `deliverToClient`, because they are already addressed. There is no ordering between engine-sent events and fan-in events. Each event is complete on its own.

Engine-sent events never enter the fan-in, so none of them can reach the phone. This covers every row below marked "all apps", "one app" or "recipients", including `prompt_pending`, `remote_action` and `secure_form`.

A full stream drops an engine-sent event (S5). Two events ask an app to act: `remote_action` and `secure_form`. For those, `SendTo` returning 0 means "not taken", and the sender handles it at once (5.7, 5.8). An app that missed events reads the state again through the routes after it reconnects.

The new payload types are fixed. S adds all of them to `pubsub/events.go` (next to `pubsub/events.go:19-33`):

| Constant | Value | Delivery | Payload (proto) | Item |
|---|---|---|---|---|
| `PayloadTypeHelper` | `helper` | all apps (comes from the fan-in today, see 5.3) | `proto.Helper` | 5.3 |
| `PayloadTypeShellOutput` | `shell_output` | one app (the requester) | `proto.ShellOutput` | 5.9 |
| `PayloadTypeLimits` | `limits` | all apps | `proto.CLILimits` | 5.9 |
| `PayloadTypeMemoryStatus` | `memory_status` | all apps | `proto.MemoryStatus` | 5.9 |
| `PayloadTypeCLIUpdatesInstalled` | `cli_updates_installed` | one app (the requester) | `proto.CLIUpdatesInstalled` | 5.9 |
| `PayloadTypeTreeChanged` | `tree_changed` | all apps | `proto.TreeChanged` | 5.6 |
| `PayloadTypeChatLink` | `chat_link` | all apps | `proto.ChatLink` | 5.11 |
| `PayloadTypeRemoteAction` | `remote_action` | one app (the share owner) | `proto.RemoteAction` | 5.7 |
| `PayloadTypeRemoteStatus` | `remote_status` | one app (the share owner) | `proto.RemoteStatus` | 5.7 |
| `PayloadTypeSecureForm` | `secure_form` | one app per recipient | `proto.SecureForm` | 5.8 |
| `PayloadTypeSecureFormClosed` | `secure_form_closed` | one app per recipient | `proto.SecureFormClosed` | 5.8 |
| `PayloadTypePromptPending` | `prompt_pending` | all apps | `proto.PromptPending` | 5.1 |

### Protocol version

- S adds `internal/proto/protocol.go` with `const ProtocolVersion = 1`.
- Phase 1, finished as a whole, is protocol 1. Builds in between also report 1, because no engine is released between the pieces.
- Later phases bump the number for any change an older app cannot handle: a removed or renamed field, route or event, or a changed meaning.
- Adding a field, route or event does not need a bump. Apps must ignore unknown fields and event types.

### Logging and secrets

- The request log writes only the method, path, remote address and user agent (`server/logging.go:9-39`). It MUST stay that way. Routes MUST NOT put secret values in paths or query strings.
- The panic handler logs the panic value and stack (`server/recover.go:14-38`). Secure-entry code MUST NOT panic with a value inside the panic.
- Builders MUST NOT log request bodies, environment values, form values, or the `env` field of a create request.

---

## 5.1 Wire fixes

### Current code facts

- **Chat record fields.** `proto.Session` has no context budget and no "estimated usage" field (`proto/session.go:16-32`).
  - The domain record has both: `session.Session.ContextBudget` and `EstimatedUsage` (`session/session.go:51-72`), where `ContextBudget{Provider, Model string; Tokens int64; Estimated bool}` (`session/session.go:86-91`).
  - Both converters drop them (`proto/convert_session.go:17-33`, `41-57`).
  - `session.Save` writes the budget only when `Provider != ""`, and it always writes the estimated-usage state it is given (`session/session.go:234-279`).
  - The save route decodes a `proto.Session` and converts it (`server/handlers_session.go:86-89`). So every client save clears the "~" marker and never stores a budget.
- **Unfinished-work flag.** `proto.AgentSession{Session; IsBusy}` (`proto/proto.go:173-176`) is filled with only the ID, title and busy state (`backend/session.go:42-58`).
  - `agent.SessionHasUnfinishedWork` exists (`agent/tasks.go:234-246`). The terminal app calls it in-process (`ui/model/session_host.go:64`).
  - `sessionToWire` computes `is_busy` and `attached_clients` when sessions are read (`server/handlers_session.go:16-21`). Session events leave both empty, as documented at `proto/session.go:5-15`.
- **Waiting-for-an-answer marks.** An app cannot tell which other chats wait for a permission or a question answer (CHECKLIST LIST-012, `CHECKLIST.md:60`).
  - Permission and question prompts reach only the apps viewing that chat (`server/events.go:155-182`).
  - A pending prompt is sent again only to an app that switches to that chat (`backend/clients.go:295-302`, `backend/events.go:85-107`).
  - There is at most one pending permission (`permission.Pending`, `permission/permission.go:297-308`) and one pending question batch (`question.Pending`, `question/question.go:339-347`) per workspace.
  - A prompt whose turn is cancelled ends without any notification: `Request` returns on `ctx.Done()` (`permission.go:283-285`), and so does `Ask` (`question.go:329-331`).
- **Memory box.** `crush server` never joins it (`cmd/server.go:27-101`). The terminal app runs `_ = memlimit.Join(memlimit.Slice)` first thing (`cmd/root.go:99-102`). Outside Linux, `Join` returns `errors.ErrUnsupported` (`memlimit/memlimit_other.go:8`).
- **Version checks.** `proto.VersionInfo` holds version, commit, build_id, go_version and platform (`proto/version.go`, filled at `backend/backend.go:228-236`).
  - On a version mismatch, create only logs it (`backend/create.go:106-112`).
  - `restartIfStale` compares `Version` and `BuildID` (`serverctl/stale.go:32-70`). It reuses a mismatched server that refuses the idle shutdown (`stale.go:53-58`).
  - `BuildID` changes on every rebuild (`stale.go:21-25`).
- **Socket.** `socketDir()` uses `$XDG_RUNTIME_DIR`, else `os.TempDir()` (`server/server.go:30-35`).
  - `DefaultHost()` builds `<socketDir>/crush-<uid>.sock`, for example `/run/user/1000/crush-1000.sock`. When the path is over `maxUnixSocketPathLen = 104` bytes, it falls back to the bare `/tmp` (`server/server.go:23`, `82-84`).
  - `listen()` removes a stale socket and calls `net.Listen` (`server/net_other.go:31-61`). It never runs `chmod` and never checks who connects.
  - `golang.org/x/sys/unix` is already a dependency (`lock/lock_unix.go`).

### Change

**(a) Context fields** (A, `proto/session.go`, `proto/convert_session.go`):

```go
type ContextBudget struct {
    Provider  string `json:"provider"`
    Model     string `json:"model"`
    Tokens    int64  `json:"tokens"`
    Estimated bool   `json:"estimated"`
}
// added to proto.Session:
ContextBudget     *ContextBudget `json:"context_budget,omitempty"` // nil when the domain Provider is ""
EstimatedUsage    bool           `json:"estimated_usage"`
HasUnfinishedWork bool           `json:"has_unfinished_work"`      // computed on read, like is_busy
PendingPrompt     string         `json:"pending_prompt"`           // computed on read: "" | "permission" | "question"
```

- `SessionFromDomain` and `SessionToDomain` carry both stored fields. Nil maps to the zero budget.
- `has_unfinished_work` is filled in `sessionToWire` and in `backend.GetAgentSession`, from `agent.SessionHasUnfinishedWork(id)`. `proto.AgentSession` embeds `Session`, so both reads carry it.
- `pending_prompt` is filled in the same two places, from `Backend.PendingPrompt(ctx, workspaceID, sessionID)` (see (e)).
- Session events never fill `is_busy`, `attached_clients`, `has_unfinished_work` or `pending_prompt`. The app MUST NOT overwrite those four from events.
- A save MUST send back the whole record it read. Today's save replaces the whole record. With the converters fixed, a record read and then sent back keeps its "~" marker and its budget.

**(b) Memory box** (A): `cmd/server.go` and `crush engine serve` both start with `_ = joinMemoryBox(memlimit.Slice)`, where `var joinMemoryBox = memlimit.Join` is a seam for tests.

**(c) Protocol check:**

- (S) Add these two fields:

  ```go
  // proto.VersionInfo, filled from proto.ProtocolVersion:
  Protocol int `json:"protocol"`
  // proto.Workspace, the create request:
  Protocol int `json:"protocol,omitempty"`
  ```

- (A) Set `Protocol` in `client.CreateWorkspace` (`client/workspaces.go:29-36`), in `recreateArgs` (`workspace/client_events.go:186-197`) and in `connectToServer` (`cmd/workspace_setup.go:210-270`).
- (S) Tests build create requests through one shared helper, `prototest.CreateRequest` (Build pieces, S22), so the check below does not break today's tests.
- (A) `backend.CreateWorkspace` checks first, in engine mode and on the legacy server alike:

  ```go
  if args.Protocol != proto.ProtocolVersion {
      return nil, proto.Workspace{}, fmt.Errorf("%w: this Crush engine speaks protocol %d (Crush %s) and the app speaks protocol %d. Update the app, or close every Crush window on this project so the engine restarts",
          ErrProtocolMismatch, proto.ProtocolVersion, version.Version, args.Protocol)
  }
  ```

- (A) `serverctl` replaces `restartIfStale` with `checkEngine(ctx, c) (reuse bool, err error)`:
  1. If `vi.Protocol != proto.ProtocolVersion`, call `ShutdownServerIfIdle`.
     - If the engine accepts, wait for its socket to go (`stale.go:102-115`) and start a new engine.
     - If it refuses (409), return `ErrProtocolMismatch` with: `The Crush engine for this project is busy and speaks protocol %d; this crush speaks %d. Wait for its work to finish or close the apps using it, then try again.`
     - Never reuse it.
  2. Same protocol but a different `Version` or `BuildID`: call `ShutdownServerIfIdle`. If it accepts, restart. If it refuses, reuse it and log at info level. This is today's behavior (`stale.go:53-58`).
  3. Everything equal: reuse.
- An engine from before this change has no `protocol` field, which decodes as 0, so it counts as a mismatch.

**(d) Socket privacy** (A):

- `server.PrivateSocketDir() (string, error)` (new file `server/socketdir.go`) tries these folders in order:
  1. Linux only: `$XDG_RUNTIME_DIR/crush`, when `XDG_RUNTIME_DIR` is set.
  2. `filepath.Join(os.TempDir(), fmt.Sprintf("crush-%d", os.Getuid()))`. On macOS `os.TempDir()` is the per-user `$TMPDIR` (inferred).
  3. `/tmp/crush-<uid>`, only when the socket path from 1 or 2 would be over 104 bytes.
- For each candidate, `PrivateSocketDir` runs `os.MkdirAll(dir, 0o700)`, then `os.Lstat`. It accepts the folder only if it is a directory, not a symlink, owned by `os.Getuid()`, with mode exactly 0700.
  - If we own the folder but its mode differs, it sets the mode with `chmod 0700` and checks again.
  - A rejected folder is skipped with `slog.Warn("Unsafe socket folder skipped", "dir", dir, "reason", reason)`.
  - If no folder is accepted, it fails with `no private folder for the Crush socket: <reasons>`. Bare `/tmp` is never used.
- **`DefaultHost()` moves.** This is a change of the legacy shared server's address, from `<socketDir>/crush-<uid>.sock` (or bare `/tmp`) to `unix://<PrivateSocketDir>/crush.sock`, for example `unix:///run/user/1000/crush/crush.sock`. The `/tmp` fallback at `server/server.go:82-84` is deleted.
  - An old `crush server` that an older binary started keeps listening at the old path. New clients never dial that path, so they never reuse it. It lives until its own clients leave, then exits after its 60 s idle delay (`backend/backend.go:55`), as today.
  - While it hosts a project, it holds that project's data-folder lock (`backend/create.go:186`). A new engine for that project is refused with `project_locked` and `MsgOtherEngineHoldsProject` (5.10) until the old server exits. Ensure waits for that lock first (5.2), up to its 15 s, so a brief overlap is absorbed. The old server can take up to a minute after its last client leaves, so the message says "or wait a minute".
- In `listen()`, right after `net.Listen`, call `os.Chmod(path, 0o600)`. If it fails, close the listener and return the error.
- The same-user check wraps the listener:

  ```go
  // server/listener_peer.go
  type peerCheckListener struct {
      net.Listener
      uid int
  }
  // Accept loops: it accepts, checks the peer, closes refused peers and keeps
  // accepting. It never returns an error for a refused peer.
  func (l peerCheckListener) Accept() (net.Conn, error)
  var peerUID = func(c *net.UnixConn) (int, error) // build-tagged default; a var so tests can inject one
  ```

  - `server/peercred_linux.go`: inside `SyscallConn().Control`, call `unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)` and use `int(cred.Uid)`.
  - `server/peercred_darwin.go`: call `unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)` and use `int(cred.Uid)`. These names are inferred from x/sys; `GOOS=darwin go build ./...` proves them.
  - `server/peercred_other.go` (`//go:build !linux && !darwin`): returns `errors.ErrUnsupported`. The wrapper is installed only on Linux and Mac.
- A connection from another user is closed at once and logged with `slog.Warn("Refused a connection from another user", "uid", uid)`. If `peerUID` fails, the connection is closed and logged with `slog.Warn("Refused a connection: peer unknown", "error", err)`.

**(e) Waiting-for-an-answer marks** (A; new `backend/pending_prompts.go`; the type goes in `proto/engine.go`):

```go
// proto/engine.go
type PromptPending struct {
    SessionID string `json:"session_id"` // the root chat (a helper's prompt counts for its parent chat)
    Kind      string `json:"kind"`       // "permission" | "question"
    Pending   bool   `json:"pending"`
}

// backend/pending_prompts.go
// PendingPrompt reports what the chat waits for: "permission", "question" or "".
// A permission wins when both are pending.
func (b *Backend) PendingPrompt(ctx context.Context, workspaceID, sessionID string) string
```

- **What counts.** `PendingPrompt` reads `ws.Permissions.ActiveRequest()` and `ws.Questions.Pending()`, as `republishPendingPrompts` does (`backend/events.go:85-107`). A prompt counts for `sessionID` when `b.sessionMatches(ctx, ws, req.SessionID, sessionID)` holds (`backend/events.go:113`), so a helper's prompt counts for its parent chat.
- **The watcher.** It starts at the workspace's first `agentInit` and stops at teardown. It subscribes to five sources:
  - permission requests (the service's broker) and permission notifications (`SubscribeNotifications`, `permission.go:324-326`)
  - question requests and question notifications (`question.go:288-292`)
  - run completions (`ws.RunCompletions()`, `app/app.go:260`), because a cancelled prompt ends without a notification (Current code facts)
- **When it sends.** After each item from any source, it recomputes the state for the root chat of that item's session, for both kinds. When a value differs from the last one sent for that `(root, kind)`, it calls `SendAll(PromptPending{…})` and stores the new value. A run completion recomputes both kinds for its chat's root.
- **Never to the phone.** The event is engine-sent, so it never enters the fan-in (section 0.1).
- **Reconnect.** An app that reconnects reads `pending_prompt` from the chat list again. Events it missed are not replayed.
- **Secure forms** are not part of this mark. They have their own list (5.8).
- **The terminal** in client mode does not use the event. A's `translateLifecycleEvent` returns `nil, true` for it (handled, nothing to pass on), so `translatePieceEvent` logs no warning (S18).

### Rules and failure paths

- **Protocol mismatch:** a mismatched engine is never reused. Create answers 426 `protocol_mismatch`. `crush engine ensure` exits with code 3 (5.2).
- **Out-of-date engines:** the next ensure restarts an idle out-of-date engine, whether its protocol or only its build differs. The engine decides whether it is idle (`ShutdownIfIdle`, `backend/shutdown.go:123-141`); the caller never does.
- **Folder check before connecting:** the engine checks the socket folder before it listens, and ensure checks it before connecting. A client never connects to a socket in a folder it has not checked.

### Tests

| Test (package) | Proves | Why it can fail |
|---|---|---|
| `TestSessionWireKeepsContextBudget` (proto) | budget and estimated flag survive both converters | a converter drops a field |
| `TestSaveSessionKeepsEstimatedUsage` (server) | a PUT of a GET result keeps the "~" marker and budget | save path zeroes them |
| `TestAgentSessionHasUnfinishedWork` (server) | true while a fake helper runs, false after its result is handed over | not wired |
| `TestSessionListHasUnfinishedWork` (server) | the list route fills it per chat | only one route fills it |
| `TestSessionListPendingPrompt` (server) | a permission pending in chat C, raised by C's helper: the list shows `pending_prompt:"permission"` for C and `""` for chat D | the helper's prompt is not mapped to its parent chat |
| `TestPromptPendingReachesEveryApp` (server; two apps, one viewing D; a phone hub built on the engine's Source) | both app streams get `prompt_pending{C, permission, true}`, then `false` after a grant, and `false` after the asking turn is cancelled; the phone stream never gets it | sent only to viewers, never cleared after a cancel, or put on the fan-in |
| `TestCreateRefusesProtocolMismatch` (backend + server) | 426, code `protocol_mismatch`, message names both numbers | mismatch only logged |
| `TestEnsureRestartsIdleMismatchedEngine` (serverctl) | idle engine at protocol 0 is replaced | old reuse path |
| `TestEnsureRefusesBusyMismatchedEngine` (serverctl) | busy engine at another protocol → `ErrProtocolMismatch`, engine untouched | busy engine reused |
| `TestEnsureReusesBusySameProtocolOtherBuild` (serverctl) | today's build rule kept | restart forced |
| `TestSocketIsOwnerOnly` (server) | socket 0600, folder 0700 | `chmod` missing |
| `TestPrivateSocketDirRejectsUnsafe` (server) | symlinked, foreign-owned or 0755 folder skipped | folder trusted blindly |
| `TestNoBareTmpFallback` (server) | an over-long path never lands in bare `/tmp` | old fallback kept |
| `TestDefaultHostInPrivateDir` (server) | `DefaultHost()` is `unix://<PrivateSocketDir>/crush.sock` | the old `crush-<uid>.sock` path kept |
| `TestPeerCheckRefusesOtherUID` (server) | an injected `peerUID` returning another uid closes the connection, and the listener keeps serving | Accept returns an error or serves the peer |
| `TestPeerUIDIsOwnUID` (server, Linux and Mac) | a real unix socket pair reports our uid | wrong socket option |
| `TestServerJoinsMemoryBox` (cmd) | both `server` and `engine serve` call `joinMemoryBox` | call missing |

Mac: `GOOS=darwin go build ./...` must pass.

### Risks

- **`chmod` race:** a gap between `Listen` and `Chmod`. The 0700 folder contains it.
- **Darwin syscall names:** the darwin compile check contains it.
- **Clients without `protocol`:** they are refused. All clients are the same `crush` binary, so a mismatch means an old engine, which ensure restarts when it is idle.

---

## 5.2 One engine per project

### Current code facts

- **One shared server per user.** `--host` defaults to `server.DefaultHost()` (`cmd/root.go:40`).
  - `serverctl.Ensure` connects to that server or starts it (`serverctl/serverctl.go:83-147`).
  - Starting holds `start.lock` in a 0700 folder for that host (`serverctl/spawn.go:28-59`, `115-121`).
  - `startDetachedServer` runs `<exe> server [--host X]` with `Setsid` (`spawn.go:61-109`).
  - The ready wait is 10 s, overridable with `CRUSH_SERVER_READY_TIMEOUT` (`serverctl/health.go:28-39`, `47-82`).
- **Workspaces are matched by folder.** The key is `resolveWorkspaceKey(args.Path)` (`backend/create.go:38`, `249-258`).
  - `joinExistingLocked` keeps the first app's settings and logs differences. It refuses an app whose `--channels` differ (`create.go:126-142`).
  - `newWorkspace` runs `config.Init(args.Path, args.DataDir, args.Debug)` and opens the database with `db.WithDataDirLock(true)` (`create.go:166-222`, `186`).
- **Workspace lifetime.** A workspace keeps its creator's `Path` (`backend/workspace.go:164-181`) and is torn down when its last client goes:
  - `expireHold` (`backend/clients.go:182-196`)
  - `RetireClient` (`clients.go:89-119`)
  - `teardown` (`backend/shutdown.go:19-49`)
- **Idle exit.** The server exits 60 s after the last workspace (`DefaultIdleShutdownDelay`, overridable with `CRUSH_SERVER_IDLE_TIMEOUT`, `backend/backend.go:55`; `shutdown.go:65-80`). The detach grace is 10 s (`backend.go:65`).
- **Data folder rules** (`config/defaults.go:71-80`), in order:
  1. An explicit data folder wins.
  2. Otherwise `options.data_directory` from config.
  3. Otherwise the closest `.crush` folder, searching upward and stopping at the Git worktree root, or at the folder itself when there is no worktree (`config/paths.go:151-160`).
  4. Otherwise `<cwd>/.crush`.
  5. The result is cleaned and joined to cwd.
- **The terminal's switch dials the legacy server.** With `CRUSH_CLIENT_SERVER=1` (`cmd/workspace_setup.go:44-49`), `setupWorkspace` calls `setupClientServerWorkspace` (`workspace_setup.go:72-77`, `188-206`). Its `connectToServer` runs `serverctl.Ensure` on `clientHost`, whose default is `DefaultHost()` (`workspace_setup.go:210-218`; `cmd/root.go:34`, `40`), and retries a 503 through `ReplaceExiting` (`workspace_setup.go:246-248`).
  - `crush run` takes the same path only with the switch on (`cmd/run.go:120`, `142`); otherwise it runs in-process (`run.go:176`).
  - `crush logout` always calls `connectToServer` (`cmd/logout.go:59`).
  - PLAN 5.2 says the terminal calls ensure directly (`PLAN.md:76`), and phase 3 needs the terminal "in shared-engine mode, using today's switch" to show the same chat live as the desktop app (`PLAN.md:65`).
- **Package-level state** assumes one project per process:
  - helper hubs (`agent/tasks.go:183-189`, marked "ponytail")
  - the limits store (`cliagent/limits.go:40-47`)
  - the secure-entry broker (`secureentry/secureentry.go:33-38`)
  - the launch ID (`remote/launcher.go:47-56`)

### Change

**Engine address** (A, `server/socketdir.go`):

```go
// EngineSocketPath is the project's engine socket:
// <PrivateSocketDir>/engine-<first 16 hex of sha256(canonical data dir)>.sock
func EngineSocketPath(dataDir string) (string, error)
func EngineAddress(dataDir string) (string, error) // "unix://" + EngineSocketPath
// CanonicalDataDir makes d absolute and clean, then resolves symlinks in the
// longest part of it that exists: filepath.EvalSymlinks on that existing
// ancestor, with the rest of the path appended unchanged.
func CanonicalDataDir(d string) (string, error)
```

`filepath.EvalSymlinks` fails on a path that does not exist yet, and a new project's `.crush` folder does not exist before its first engine starts. Resolving only the existing ancestor gives the same name before and after the folder is created, even under a symlinked parent. It walks up with `filepath.Dir` until `os.Lstat` succeeds. The root always exists.

**Data folder resolver** (A, new `config/datadir.go`):

```go
// ResolveDataDir applies the rules of defaults.go:71-80 without loading
// providers. It reads options.data_directory from the same files, with the
// same precedence, as Load (inferred: the builder reuses Load's file lookup).
func ResolveDataDir(workingDir, dataDir string) (string, error)
```

**Engine mode in the backend** (A):

```go
const EngineIdleShutdownDelay = 10 * time.Second // CRUSH_ENGINE_IDLE_TIMEOUT overrides it (tests)
func (b *Backend) SetEngine(canonicalDataDir string) // one project, workspaces keyed by data folder, 10 s idle exit
func (b *Backend) EngineDataDir() string            // "" on the legacy server
```

In engine mode, `CreateWorkspace` runs these steps in order:

1. The protocol check (5.1).
2. `d := config.ResolveDataDir(args.Path, args.DataDir)`. If `CanonicalDataDir(d)` is not the engine's folder, fail with `fmt.Errorf("%w: this Crush engine serves the project whose data folder is %s, not %s", ErrWrongProject, engineDir, d)`.
3. The key is the engine's data folder, not the app's folder. So every app of the project joins the one live workspace. `joinExistingLocked` is unchanged: the first app wins for path, YOLO, debug and environment, and a different channel opt-in is still refused.
4. A new workspace uses `args.Path` as the folder the AI works in. It opens the database **without** `db.WithDataDirLock`, because the engine already holds the project lock (5.10). A second `flock` from the same process on a new file handle would conflict.

The legacy server (no `SetEngine`) keeps folder keys and the 60 s exit.

**Engine info route** (A):

| Op | Method and path | id | Request | Response |
|---|---|---|---|---|
| `rpc.EngineInfo` | GET `/v1/engine` | none | `rpc.Empty` | `proto.EngineInfo` |

```go
// proto/engine.go (A)
type EngineInfo struct {
    Protocol      int       `json:"protocol"`
    Version       string    `json:"version"`
    PID           int       `json:"pid"`
    DataDir       string    `json:"data_dir"` // canonical; "" on the legacy server
    Address       string    `json:"address"`
    StartedAt     time.Time `json:"started_at"`
    WorkspaceID   string    `json:"workspace_id,omitempty"`
    WorkspacePath string    `json:"workspace_path,omitempty"` // the folder the AI works in
    Apps          int       `json:"apps"`                     // registered clients
    Closing       bool      `json:"closing"`                  // the engine decided to exit (Backend.closing)
}
```

- `GET /v1/engine` answers while the engine is closing, as `GET /v1/version` does today (`server/handlers_workspace.go:27-29`). Only create checks the closing latch (`admitLocked`, `backend/clients.go:69-77`).
- `closing` comes from a new `func (b *Backend) Closing() bool`, which reads `b.closing` under `b.mu`. The latch is set at `backend/shutdown.go:73`, `103` and `128`.

**CLI commands** (A, new `cmd/engine.go`):

- Each command prints exactly one JSON value on stdout. Messages for people go to stderr.
- They use the root flags `--cwd`, `--data-dir` and `--debug` (`cmd/root.go:37-39`).

| Command | Output (stdout) | Exit |
|---|---|---|
| `crush engine version` | `{"version":"<version.Version>","protocol":1}` | 0 |
| `crush engine ensure [--cwd DIR] [--data-dir D] [--timeout 15s]` | success object below | 0 |
| same, failing | `{"error":{"code":"<code>","message":"<text>"}}` | 3 `protocol_mismatch`, 4 `project_locked`, 5 `start_failed`, 6 `unsafe_socket_dir`, 1 other |
| `crush engine list` | JSON array of `proto.EngineInfo`, one per live `engine-*.sock` in `PrivateSocketDir()` (1 s timeout each). Dead sockets are skipped and left alone. | 0, `[]` when none |
| `crush engine serve --data-dir D` (hidden) | nothing (serves) | 0 on a clean stop; 4 on a lock failure; 1 otherwise |

Success output of `ensure`:

```json
{"address":"unix:///run/user/1000/crush/engine-0123456789abcdef.sock","socket":"/run/user/1000/crush/engine-0123456789abcdef.sock","protocol":1,"version":"v0.97.1","pid":4242,"data_dir":"/home/u/proj/.crush","started":true}
```

`started` is true when this call started the engine.

`engine serve` runs these steps:

1. Join the memory box (5.1).
2. Make the data folder canonical.
3. Take the project lock (5.10). On failure, write `start-error.json`.
4. Run `config.Load(config.GlobalWorkspaceDir(), D, debug)`, as `cmd/server.go` does.
5. Create the backend, then call `SetEngine(D)`.
6. Listen on `EngineSocketPath(D)` and serve.
7. On stop, shut down within 5 s, like `crush server`.

**Shared ensure function** (A, new `serverctl/engine.go`):

```go
type EnsureOptions struct {
    Cwd     string        // the app's folder
    DataDir string        // explicit --data-dir, or ""
    Debug   bool
    Timeout time.Duration // 0 means 15 s
}
type EngineHandle struct {
    Address, Socket string
    Protocol        int
    Version         string
    PID             int
    DataDir         string
    Started         bool
}
func EnsureEngine(ctx context.Context, opts EnsureOptions) (EngineHandle, error)
var (
    ErrProjectLocked = errors.New("project locked")      // wraps the 5.10 message
    ErrEngineStart   = errors.New("engine did not start")
)
```

`EnsureEngine` runs these steps in order:

1. Resolve and canonicalize the data folder.
2. Find the socket path. This also checks the folder (5.1 d).
3. `GET /v1/version` with a 2 s timeout, then apply the rules of 5.1 (c).
4. If the engine is usable, `GET /v1/engine` and require `data_dir` to match. A mismatch is `ErrWrongProject` (a hash collision or a foreign engine).
   - If `closing` is true, the engine is on its way out: go to step 5.
   - Otherwise return it.
5. If the engine cannot be reached (refused, no socket file, or stale per `server.IsStaleSocketErr`, `server/socket_classify.go:18-27`), or it is closing, or it answers 503:
   1. Take `start.lock` in `perHostServerDir(address)` (reuse `spawn.go:28-59`, `115-121`).
   2. Check again (steps 3 and 4).
   3. **Wait for the old engine to let go of the project.** While another process holds the project lock exclusively, keep waiting. The probe is `lock.TryFileShared(<data dir>/crush.lock)` (5.10), released at once, every 100 ms. The wait lasts at most the time left of the ensure timeout. If the lock is still held then, return `ErrProjectLocked` with the 5.10 message for the PID in the owner info. A lone window's shared lock does not block the probe; the new engine reports it (5.10).
   4. Delete any old `start-error.json`.
   5. Start `<exe> engine serve --data-dir <canonical> [--debug]` detached (`Setsid`), with stdout and stderr going to `engine.log` in the same folder. The new engine's `listen()` removes a leftover socket file, as today (`server/net_other.go:31-61`).
   6. Wait until it is ready (`health.go:47-82`).
6. If it never becomes ready:
   - If `start-error.json` exists, read `{"code","message"}` from it and return `ErrProjectLocked` or `ErrEngineStart` wrapping that message.
   - Otherwise return `ErrEngineStart` with the last 20 lines of `engine.log`.
7. **A create that answers 503 `shutting_down`** (`admitLocked`, `backend/clients.go:69-71`) means the engine's exit timer fired between ensure and create. The caller runs `EnsureEngine` again and creates once more:
   - the Go callers, through `serverctl.CreateWorkspace`'s replace callback (`cmd/workspace_setup.go:246-248`)
   - the desktop app, by running `crush engine ensure` again
   - The closing engine reports `closing: true`, or no longer answers at all. Either way, ensure reaches step 5, waits there for the lock, then starts a new engine.
   - A second 503 in a row is returned to the caller as an error.

**Why ensure waits on the lock, not on the socket.** The legacy path waits up to 2 s for the old socket file to go and then deletes it (`awaitSocketGone`, `serverctl/stale.go:99-115`). An engine stops listening before it exits, and it holds the project lock until the process ends (5.10). So a slow-to-stop engine can have no socket and still hold the lock. If ensure started a new engine then, that engine's own 5 s lock wait could run out, and the start would fail. Waiting for the lock itself removes that race. `awaitSocketGone` stays as it is for the legacy path only.

**Users of the function:**

- `crush engine ensure` calls it, and the desktop app calls that command.
- The terminal's switch calls it directly, from phase 1 (below).

**The terminal's switch** (A, `cmd/workspace_setup.go`). From phase 1, `CRUSH_CLIENT_SERVER=1` means the terminal's shared-engine mode, as PLAN 5.2 and the phase-3 done-line need (Current code facts). `connectToServer` changes as follows:

1. **The engine path.** When `useClientServer()` is true and `--host` was not given (`cmd.Flags().Changed("host")`, as `cmd/root.go:229` checks):
   - Run `h, err := serverctl.EnsureEngine(ctx, serverctl.EnsureOptions{Cwd: cwd, DataDir: dataDir, Debug: debug})`.
   - Connect with `client.NewClient(cwd, "unix", h.Socket)`.
   - Build `wsReq` as today, plus `Protocol` (5.1 c).
   - The create's replace callback runs `EnsureEngine` again (step 7), instead of `ReplaceExiting`.
2. **A live engine first, for `crush logout`.** With the switch off and no `--host`, only `crush logout` calls `connectToServer` (`cmd/logout.go:59`; `crush run` calls it only with the switch on, `cmd/run.go:120-121`, `142`). In that case `connectToServer` first looks for the project's live engine with the new `serverctl.FindEngine(ctx, opts)`. That is ensure's steps 1 to 4 with a 2 s limit, and it never starts an engine. When an engine answers, is not closing and speaks this protocol, logout joins it as the engine path does. Otherwise it takes the legacy path.
3. **The legacy path.** Every other case keeps today's code: `serverctl.Ensure` on `clientHost` and `ReplaceExiting`. That covers an explicit `--host`, and `crush logout` when no engine runs for the project.
4. **Crash recovery.** On the engine path, `setupClientServerWorkspace` calls `clientWs.SetEnsureEngine(func(ctx context.Context) error { _, err := serverctl.EnsureEngine(ctx, opts); return err })` (5.14).
5. **Other commands.** `crush run` with the switch on takes the engine path through the same function (`cmd/run.go:120`, `142`).

```go
// serverctl/engine.go (A)
// FindEngine runs EnsureEngine's steps 1 to 4 and never starts an engine. It
// reports ok=false when no engine answers within opts.Timeout (0 means 2 s),
// or when the one that answers is closing or speaks another protocol.
func FindEngine(ctx context.Context, opts EnsureOptions) (h EngineHandle, ok bool, err error)
```

**Why logout joins the engine instead of skipping the lock.** The engine holds the project lock for its whole life (5.10), and the legacy create takes the same lock (`backend/create.go:186`). So while the desktop has the project open, today's logout path fails with the lock error. Joining is the smaller safe fix:

- `FindEngine` is ensure's first four steps, already built for 5.2.
- Logout changes only global config fields (`RemoveConfigField(…, config.ScopeGlobal, …)`, `cmd/logout.go:127-190`). Through the engine, the engine's own config is written and reloaded (`config/store_fields.go:250-260`), so its loaded copy forgets the sign-in too. Through a second server, only the file would change.
- Skipping the lock would need a new create field. The legacy server would then hold a workspace without the lock, keyed by folder, which a later `--host` client of that folder would join (`joinExistingLocked`, `backend/create.go:126-142`) and run AI work in without the lock.
- Logout's cleanup retires only its own client (`cmd/workspace_setup.go:262-268`), so the desktop's workspace stays.

### Rules

- **Join by project.** Two apps whose folders resolve to one data folder get one workspace ID. That means one coordinator, so one prompt queue and one AI worker per chat.
- **First app's folder wins until every app has left.** "Left" means retired, or its hold expired after the 10 s grace. This is true even if the first app leaves early.
  - Every later app compares `path` in the create response with its own folder. If they differ, the desktop shows: `The AI works in <path>, the folder the first window on this project opened.`
  - After teardown, the next app's folder wins.
- **YOLO follows the same rule.** The desktop sends `yolo` from its own setting. That setting defaults to true, like the terminal's `--yolo` (`cmd/root.go:42`).
  - The first app's value wins (`backend/create.go:136-141`; `newWorkspace` sets it at `create.go:174`). A different value is only logged today.
  - The create answer already carries the effective value: `workspaceToProto` fills `yolo` from `SkipPermissionRequests` (`backend/workspace.go:164-181`, line 169). So no new field is needed.
  - When the answer's `yolo` differs from what the app sent, the desktop shows a notice like the folder notice. For `yolo:true`: `Crush skips permission prompts on this project, as the first window on it chose.` For `yolo:false`: `Crush asks before using tools on this project, as the first window on it chose.`
- **The 10-second exit.** The workspace is torn down when its last app leaves, as today. The engine exits 10 s after that.
  - Clean close: the workspace goes at once, and the engine exits at 10 s.
  - Dropped connection: the hold expires at 10 s, the workspace goes, and the engine exits at 20 s.
- **Reopen at 5 s.** Ensure reaches the lingering engine. Create cancels the shutdown timer (`shutdown.go:84-89`) and builds a fresh workspace.
  - If the timer fires during the create, create answers 503 `shutting_down`. The app runs ensure again (step 7). Ensure waits for the old engine to release the project lock, then starts a new engine.
- **Reopen at 11 s.** Ensure starts a new engine. If the old engine is still exiting, ensure waits for its lock first (step 5.3). The new engine's own 5 s lock wait (5.10) is a second guard.
- **Two ensures at once.** `start.lock` makes them take turns. The second finds the engine running.
- **One project per engine.** `ErrWrongProject` keeps every other project out. This is what makes the package-level state above safe.

### Tests

| Test (package) | Proves | Why it can fail |
|---|---|---|
| `TestEngineJoinsAppsFromDifferentFolders` (backend) | creates from `<root>/a` and `<root>/b`, which share one `.crush`, get one workspace; its path is `<root>/a` | still keyed by folder |
| `TestEngineOneQueueOneWorkerPerChat` (backend, fake coordinator) | sends from both apps to one chat: one run active, one prompt queued | two coordinators |
| `TestFirstFolderWinsUntilAllLeave` (backend) | first app retires, path unchanged; last app leaves; a new app from `b` makes the path `b` | path reset early or kept after teardown |
| `TestCreateAnswerCarriesEffectiveYolo` (backend) | the first app sends `yolo:true`, the second `yolo:false`; the second answer says `yolo:true` | the answer echoes the request instead of the workspace's value |
| `TestEngineRefusesOtherProject` (backend + server) | create for another data folder → 409 `wrong_project` | engine serves two projects |
| `TestEngineExitDelayIsTenSeconds` (backend) | the engine exit timer uses `EngineIdleShutdownDelay`; legacy still uses 60 s | 60 s kept |
| `TestReopenBeforeAndAfterIdleExit` (serverctl; real engine subprocess via test-binary re-exec; `CRUSH_ENGINE_IDLE_TIMEOUT=2s`; the last app closes cleanly, then reopens at 1 s and, after closing again, at 3 s: the plan's 5 s and 11 s for a 10 s exit, `PLAN.md:78`, scaled to the 2 s exit) | both reopens succeed; the first reuses the PID, the second has a new one | dead socket left, or a 503 reaches the caller |
| `TestCreateDuringExitRetriesThroughEnsure` (serverctl; idle timeout injected so the exit latch closes between ensure and create) | the create's 503 `shutting_down` runs ensure again; the second create lands on a new engine | the 503 reaches the caller, or ensure reuses the closing engine |
| `TestEngineInfoReportsClosing` (server) | `GET /v1/engine` answers `closing:true` after `ShutdownIfIdle` latches | ensure cannot tell a closing engine from a live one |
| `TestEnsureWaitsForSlowOldEngine` (serverctl; a helper process holds the exclusive project lock for 3 s after its socket is gone) | ensure waits, then starts a new engine once the lock is free; the new engine never reports `project_locked` | ensure starts as soon as the socket is gone |
| `TestEnsureGivesUpOnStuckOldEngine` (serverctl; ensure timeout 1 s; the lock is held throughout) | `ErrProjectLocked` with the 5.10 message and the holder's PID | ensure waits forever, or starts an engine that then fails its lock wait |
| `TestEnsureConcurrentStartsOneEngine` (serverctl) | 5 parallel ensures → one PID | start lock missing |
| `TestEngineEnsureJSON` (cmd) | exact keys and exit codes for success and each error | output drift |
| `TestResolveDataDirMatchesLoad` (config) | flag; relative and absolute config option; `.crush` at the worktree root seen from a subfolder; no `.crush` | resolver drift |
| `TestEngineSocketNameStable` (server) | same folder → same name; a symlinked path → same name; a data folder not created yet under a symlinked parent → the same name as through the real parent, before and after the folder is created | hash input not canonical, or `EvalSymlinks` fails on the missing folder |
| `TestEngineListShowsLiveEngines` (cmd) | two engines listed; a dead socket skipped and not deleted | one dead socket fails the whole list, or the list deletes it |
| `TestClientServerEnvJoinsEngineWorkspace` (cmd + serverctl; real engine subprocess) | with `CRUSH_CLIENT_SERVER=1` and no `--host`, `connectToServer` reaches the project's engine at `EngineSocketPath`, and gets the same workspace ID as a second client created on that engine directly | the switch still dials `DefaultHost()` |
| `TestExplicitHostKeepsLegacyServer` (cmd) | with `--host unix://<temp>/x.sock`, and also with the switch off and no engine running for the project (as `crush logout` calls it), `connectToServer` takes the legacy path and no engine socket appears | the engine path taken for an explicit host, or `FindEngine` starts an engine |
| `TestLogoutJoinsLiveEngine` (cmd + serverctl; a real engine subprocess holds the project, as the desktop leaves it; the switch off; a test global config with `providers.hyper.oauth` set) | `crush logout --force hyper` succeeds; the engine's config no longer has the field; no legacy server socket appears; the engine's workspace is still there | today's path: the legacy create takes the project lock that the engine holds (`backend/create.go:186`), so logout fails with the lock error |

### Risks

- **First app's environment wins** (`create.go:126-142`). A desktop app started from the desktop session may have a shorter `PATH` than a terminal. The desktop side handles this by sending its login-shell environment (desktop work, inferred need). The folder notice makes the first-app rule visible.
- **Hash collision:** contained by the `data_dir` check in ensure.
- **Package globals:** contained by one project per engine. The legacy server with several projects keeps today's sharing.
- **The switch starts one engine per project.** With `CRUSH_CLIENT_SERVER=1`, terminals on three projects now run three engines instead of one shared server. Each engine exits 10 s after its last app leaves.
- **`crush logout` next to a closing engine.** `FindEngine` treats a closing engine as absent, and the legacy create then meets the lock until that engine exits, within seconds. Logout then fails with the lock error, and running it again works.

---

## 5.3 Helpers (sub-agents)

### Current code facts

- **The record.** A helper is an `agent.Task` (`agent/tasks.go:56-79`). Its status is one of `running`, `done`, `failed` or `stopped` (`tasks.go:46-53`).
  - Helpers live in one hub per coordinator (`tasks.go:114-150`).
  - The hubs are listed in package state (`tasks.go:183-189`).
- **Listing and stopping.**
  - `SessionTasks(sessionID)` returns a chat's helpers, running or ended (`tasks.go:213-228`).
  - `StopTask(id)` stops a helper in any chat, by ID alone. It marks the helper stopped by the user, so its parent chat is told (`tasks.go:193-210`).
- **Events.**
  - Every change is published as `pubsub.UpdatedEvent` (`tasks.go:424-428`).
  - The app adds these events to its event fan-in (`app/events.go:27`).
  - `wrapEvent` has no case for `pubsub.Event[agent.Task]`, so its default case drops them with a warning (`server/events.go:113-115`). Client mode never sees helpers.
- **In-process callers today:**
  - the terminal's stop button (`ui/dialog/background.go:174`)
  - the phone source (`remote/source.go:93-98`)
  - the terminal's task row, which refreshes every 1 s while helpers are listed (`ui/model/tasks.go:146-152`)

### Change

Proto (B, `proto/live.go`):

```go
type Helper struct {
    ID             string    `json:"id"`
    SessionID      string    `json:"session_id"`       // the chat that started it
    ChildSessionID string    `json:"child_session_id"` // its own transcript; read it with the existing session and message routes
    Name           string    `json:"name"`
    CLI            string    `json:"cli"`
    Model          string    `json:"model"`
    Effort         string    `json:"effort,omitempty"`
    Fast           bool      `json:"fast,omitempty"`
    ReadOnly       bool      `json:"read_only,omitempty"`
    Status         string    `json:"status"` // "running" | "done" | "failed" | "stopped"
    Started        time.Time `json:"started"`
    Ended          time.Time `json:"ended,omitzero"`
    Delivered      bool      `json:"delivered,omitempty"`
    StartedBy      string    `json:"started_by,omitempty"` // owning app's client ID (5.5); "" = no app
}
func HelperFromTask(t agent.Task) Helper // proto/live.go
```

Routes (B):

| Op | Method and path | id | Request | Response |
|---|---|---|---|---|
| `rpc.ListHelpers` | GET `/v1/workspaces/{id}/sessions/{sid}/helpers` | none | `rpc.Empty` | `[]proto.Helper` |
| `rpc.StopHelper` | POST `/v1/workspaces/{id}/sessions/{sid}/helpers/{hid}/stop` | optional | `rpc.Empty` | `rpc.Empty` |

Event (B, in `wrapLiveEvent`): `pubsub.Event[agent.Task]` becomes `envelope(pubsub.PayloadTypeHelper, pubsub.Event[proto.Helper]{Type: e.Type, Payload: proto.HelperFromTask(e.Payload)})`. Every app gets it. Apps filter by `session_id` and `child_session_id`.

Go signatures (B):

```go
// agent/tasks.go
type Task struct { /* existing fields */ StartedBy string `json:"started_by,omitempty"` }
// StopSessionTask is StopTask limited to one chat: same user-stop meaning
// (the parent is told). It returns ErrTaskNotInSession when the helper is
// not in sessionID.
func StopSessionTask(sessionID, id string) error
var ErrTaskNotInSession = errors.New("no such helper in this chat")

// backend/live.go
func (b *Backend) ListHelpers(workspaceID, sessionID string) ([]proto.Helper, error)
func (b *Backend) StopHelper(workspaceID, sessionID, helperID string) error // ErrTaskNotInSession → ErrHelperNotFound

// client/live.go
func (c *Client) ListHelpers(ctx context.Context, workspaceID, sessionID string) ([]proto.Helper, error)
func (c *Client) StopHelper(ctx context.Context, workspaceID, sessionID, helperID string) error
```

`translateLiveEvent` (B, workspace) turns `proto.Helper` back into `pubsub.Event[agent.Task]`. This lets the terminal app's task row work in client mode.

### Rules

- The list holds only helpers whose `SessionID` is `{sid}`. A helper's own helpers are listed under that helper's child chat, as `SessionTasks` does today.
- Stopping a helper from another chat answers 404 `helper_not_found`, and the helper keeps running.
- Stopping a helper that has already ended answers 200 and changes nothing. That is today's `StopTask` result for a helper it finds.
- The fan-in can drop events. So the app lists helpers again:
  - on reconnect
  - when a chat opens
  - every 1 s while any helper of the shown chat is running (the terminal's cadence)

### Tests

| Test (package) | Proves | Why it can fail |
|---|---|---|
| `TestHelperEventReachesSSE` (server) | a task event on the fan-in reaches a stream as type `helper` with every field | still dropped |
| `TestListHelpersScopedToChat` (backend + server) | two chats with one helper each → each list has one | global list |
| `TestStopHelperOtherChatRefused` (server) | 404 `helper_not_found`; helper still running | ID-only stop |
| `TestStopSessionTaskTellsParent` (agent) | user stop marks `userStopped`, so the parent gets the "stopped" result | wrong stop kind |
| `TestClientHelpersRoundTrip` (client) | both client methods against the real handler | path or type drift |
| `TestClientWorkspaceTranslatesHelperEvent` (workspace) | the terminal task row gets `agent.Task` in client mode | missing translator |

### Risks

- **Lost events:** contained by the re-list rules above.
- **`StartedBy` persistence:** `StartedBy` is saved in `tasks.json` along with the rest of the task. That is harmless: after a restart it names a dead app, and 5.5 gives the helper a new owner on resume.

---

## 5.4 Background jobs

### Current code facts

- **The record.** `agent.Process{PID, JobID, Command, Name, Started, Session, marker}` (`agent/background.go:26-38`).
- **The list is program-wide.** `BackgroundProcesses()` (`background.go:112-136`) returns:
  - managed jobs
  - processes from the process table that carry a chat ID
- **Managed jobs carry no chat** (`background_jobs.go:299-313`, line 307), even though the hub knows it (`backgroundOwners[job.ID] = sessionID`, `background_jobs.go:185`).
- **Kill.** `StopBackgroundProcess(Process)` (`background.go:283-293`) and `KillProcess(pid)` (`background.go:297-332`) send SIGTERM, then SIGKILL 3 s later.
- **Polling.** The terminal polls the list every 1 s (`bgProcsRefresh`, `ui/model/tasks.go:22`, `162-172`).
- **Ctrl+B.**
  - `AppWorkspace.AgentBackground(sid)` is `cliagent.Background(sid) || tools.BackgroundShell(sid)` (`workspace/app_workspace.go:141-143`).
  - `tools.BackgroundShell` signals the chat's running shells to move to the background, and reports whether there was one (`agent/tools/background.go:87-99`).
  - `ClientWorkspace.AgentBackground` always returns false (`workspace/client_workspace.go:248-250`), so Ctrl+B does nothing in client mode.
  - The hint uses `cliagent.CanBackground(kind)` (`cliagent/cliagent.go:366`; called at `ui/model/workspace_cache.go:215`).
- **The tree guard is program-wide.** It refuses when any of these exists (`agent/tree.go:38-46`, `194-206`):
  - a managed shell in any hub
  - any background process
  - any unfinished job in the shell manager
- **Mac.** Reading the process table is Linux-only, so on Mac only managed jobs are listed (inferred). The plan's Q11 keeps it that way.

### Change

Proto (B, `proto/live.go`):

```go
type Job struct {
    PID       int       `json:"pid,omitempty"`    // 0 for managed jobs without a process of their own
    JobID     string    `json:"job_id,omitempty"` // managed job (`crush bg`, a backgrounded bash call)
    Command   string    `json:"command"`
    Name      string    `json:"name,omitempty"`
    Started   time.Time `json:"started"`
    SessionID string    `json:"session_id"`
    StartedBy string    `json:"started_by,omitempty"` // 5.5
}
type JobRef struct {
    JobID string `json:"job_id,omitempty"` // exactly one of the two is set
    PID   int    `json:"pid,omitempty"`
}
type BackgroundResult struct {
    Moved bool `json:"moved"`
}
type AgentCapabilities struct {
    Kind          string `json:"kind"`           // catwalk type of the current coder model; "" when none
    CanBackground bool   `json:"can_background"` // cliagent.CanBackground(kind)
}
```

Routes (B):

| Op | Method and path | id | Request | Response |
|---|---|---|---|---|
| `rpc.ListJobs` | GET `/v1/workspaces/{id}/sessions/{sid}/jobs` | none | `rpc.Empty` | `[]proto.Job` |
| `rpc.KillJob` | POST `/v1/workspaces/{id}/sessions/{sid}/jobs/kill` | optional | `proto.JobRef` | `rpc.Empty` |
| `rpc.MoveToBackground` | POST `/v1/workspaces/{id}/sessions/{sid}/background` | optional | `rpc.Empty` | `proto.BackgroundResult` |
| `rpc.AgentCapabilities` | GET `/v1/workspaces/{id}/agent/capabilities` | none | `rpc.Empty` | `proto.AgentCapabilities` |

Go signatures (B):

```go
// agent
// background_jobs.go:307 adds: Session: hub.backgroundOwners[job.ID]
func SessionBackgroundProcesses(sessionID string) []Process // BackgroundProcesses filtered by Session
// agent/session_work.go (file created by S with today's program-wide body; B narrows it)
func SessionBackgroundBlock(sessionID string) error

// backend/live.go
func (b *Backend) ListJobs(workspaceID, sessionID string) ([]proto.Job, error)
func (b *Backend) KillJob(workspaceID, sessionID string, ref proto.JobRef) error // ErrJobNotFound when not in the chat's list
func (b *Backend) MoveToBackground(workspaceID, sessionID string) (bool, error)
func (b *Backend) AgentCapabilities(workspaceID string) (proto.AgentCapabilities, error)

// workspace/live.go (optional interface, like TreeManagement)
type LiveWork interface {
    Helpers(ctx context.Context, sessionID string) ([]agent.Task, error)
    StopHelper(ctx context.Context, sessionID, id string) error
    Jobs(ctx context.Context, sessionID string) ([]agent.Process, error)
    KillJob(ctx context.Context, sessionID string, p agent.Process) error
    CanBackground(ctx context.Context) (bool, error)
    CLILimits(ctx context.Context) ([]proto.CLILimits, error) // 5.9
}
// Both AppWorkspace (workspace/app_live.go, calls the globals) and
// ClientWorkspace (workspace/client_live.go, calls the routes) implement it.
// ClientWorkspace.AgentBackground calls rpc.MoveToBackground and returns false
// on error (logged).
```

`SessionBackgroundBlock(sid)` (B's version) returns today's errors (`tree.go:39`, `tree.go:44`). It counts only:

- managed shells whose `backgroundOwners[job.ID] == sid`, including those still delivering their end-of-job message
- `BackgroundProcesses()` entries with `Session == sid`
- unfinished shell-manager jobs whose owner is `sid`, or whose owner is unknown. Unknown blocks, so the check fails safe.

### Rules

- **Scoped to the chat:** lists, kill, Ctrl+B and the tree guard all look only at `{sid}`.
- **Ctrl+B with nothing to move:** `moved:false` is not an error. The app shows `No running command can move to the background`, the phone's text at `ui/model/remote.go:388-392`.
- **Killing outside the chat:** a job from another chat answers 404 `job_not_found`, and the job is left running.
- **Polling:** the desktop polls `ListJobs` every 1 s for the chat on screen, the terminal's cadence. There is no job event; see "Not in scope".

### Tests

| Test (package) | Proves | Why it can fail |
|---|---|---|
| `TestManagedJobsCarrySession` (agent) | managed jobs listed with their chat | line 307 unchanged |
| `TestListJobsScopedToChat` (server) | jobs of chat A absent from chat B's list | program-wide list |
| `TestKillJobOtherChatRefused` (server) | 404; job alive | unscoped kill |
| `TestMoveToBackgroundSignalsChatShell` (backend) | a shell registered with `RegisterShellBackground(A, …)` is signalled for A, not for B | wrong chat |
| `TestAgentCapabilitiesRoute` (server) | `can_background` matches `cliagent.CanBackground` | the route reads another model kind than the terminal's hint does (`ui/model/workspace_cache.go:215`) |
| `TestTreeGuardIgnoresOtherChatsJobs` (agent) | a job in chat B does not block a switch in chat A; a job in A blocks with today's message | program-wide guard |
| `TestClientWorkspaceAgentBackgroundUsesRoute` (workspace) | Ctrl+B works in client mode | still returns false |

### Risks

- **Polling cost.** Several apps each polling the process table every second is the same load as several terminal windows today.
- **Unknown-owner jobs** make the guard stricter than scoped, but never looser.

---

## 5.5 Ownership (Q2)

### Current code facts

- **No owner is recorded.**
  - `SendMessage` gets no client ID (`server/handlers_agent.go:26-28`; `backend/agent.go:31-62`).
  - `proto.AgentMessage` has no client field (`proto/proto.go:145-153`).
  - The Go client sends none (`client/agent.go:56-70`).
  - Runs live on the workspace's context (`backend/agent.go:88-131`).
- **When an app counts as gone:**
  - it is retired (`clients.go:89-119`)
  - its hold expires 10 s after its last stream drops (`detachStream` arms the timer at `clients.go:225-258`; `expireHold` at `182-196`)
  - it releases the workspace (`clients.go:125-142`, `198-223`)
  - its last stream drops while the grace is 0 (`CRUSH_SERVER_DETACH_GRACE`, `backend/backend.go:64`, `149`; `SetDetachGrace`, `backend.go:185-189`), or after it released its claim. Then `detachStream` deletes the claim at once, in its delete branch (`clients.go:249-252`).
  - Reattaching inside the grace cancels the hold, because `AttachClient` stops the timer (`clients.go:44-47`).
- **What an app shows.** `SetCurrentSession` stores the app's current chat (`clients.go:273-304`; read at `clients.go:313-325`). Chat roots come from `RootSessionID` (`backend/events.go:42-76`).
- **Prompts and the queue.**
  - Every prompt is a `SessionAgentCall` with a `SubmissionID` (`agent/agent.go:42-58`). It is set from the request context (`agent/coordinator_run.go:160`; `backend/agent.go:93`). The terminal always sends one (`ui/model/send.go:104-109`); other callers may send none.
  - Queued prompts keep their `SubmissionID`. They wait in `messageQueue` or, during an interrupt, in `interrupting` (`agent/scheduler.go:26-38`). `QueuedPromptSummaries` lists them, led by prompts already handed to an agent CLI (`agent/queue_summary.go:5-30`).
  - Prompts already handed to an agent CLI cannot be withdrawn (`scheduler.go:579-580`).
  - Every accepted submission gets exactly one `RunComplete` that carries its `SubmissionID`:
    - its own turn (`agent/agent.go:379-401`; one per original call of a batch, lines 380-386)
    - the turn it was folded into (`agent/turn.go:287-291`)
    - or a cancelled drop from the queue (`scheduler.go:419-443`)
  - `Cancel` stops the active turn **and clears the whole queue** (`scheduler.go:498-540`; `clearQueueAndNotify` at 449-468). The cancel route uses it (`Backend.CancelSession`, `backend/agent.go:194`; `ops_agent.go:23-28`).
  - `Interrupt` stops the active turn and **keeps the queue**, which runs next (`scheduler.go:560-570`; `backend/agent.go:288-297`). With nothing queued it ends the turn, like a cancel (`ui/model/send.go:309-316`).
  - The active-run record holds only a cancel function (`agent/agent.go:169-176`, set at `turn.go:200-202`). So the agent cannot say whose prompt is running.
- **Today's full stop** at teardown:
  - `Workspace.Shutdown` cancels, then calls `CancelAll`, waits, and calls `App.Shutdown` (`workspace.go:130-145`). `CancelAll` is only called on shutdown paths (`app/app.go:318-327`, `backend/workspace.go:139`).
  - The hubs' `stopAll` saves running helpers as interrupted and cancels session runs (`agent/tasks.go:307-324`). The saved owner stays this process's PID: `remember` sets it (`tasks_resume.go:98`), and `markInterrupted` only adds the time (`tasks_resume.go:120-136`).
  - Jobs cut off by shutdown are saved as interrupted, not reported (`background_jobs.go:226-230`). Their owner stays this PID too: `rememberJob` sets it (`background_jobs_resume.go:59-70`), and `interruptJob` only adds the time and the last output (`background_jobs_resume.go:82-101`). `claimInterruptedJobs` skips jobs whose owner is this process or a live one (`background_jobs_resume.go:106-124`, line 113).
  - **Teardown runs outside `b.mu`.** `teardown` removes the workspace from the index under `b.mu`, releases it, and only then calls `invokeShutdown` (`backend/shutdown.go:19-49`, lines 31-43). So a create for the same project can build a new workspace and run its interactive `InitAgent`, which calls `ResumeInterruptedTasks` (`backend/agent.go:160-165`), while the old workspace is still shutting down. `ResumeInterruptedTasks` works on a copy of `hubs` taken under `hubsMu` (`tasks_resume.go:50-57`), so that copy can still hold the dying hub.
  - **The hub is never removed.** `newTaskHub` appends to `hubs` (`tasks.go:177-179`). `coordinator.CancelAll` only calls `stopAll` (`agent/coordinator.go:272-277`), and nothing removes the hub. Its watchers end once its folder is gone (`agent/tasks_inbox.go:27-30`, `background.go:172-175`). Its reaper stays open until the process exits (`agent/reaper_linux.go:33-35`).
  - So when the project reopens inside the same engine process, the dead hub is still scanned by `StopTask`, `SessionTasks`, `SessionHasUnfinishedWork` and `SessionHasRunningWork` (`tasks.go:193-264`), and by the tree guard. Its task IDs (`t1`, …) shadow the new hub's.
- **Resume.**
  - `ResumeInterruptedTasks` runs once per hub (`resumeOnce`, `agent/tasks_resume.go:50-57`; `tasks.go:133-134`).
  - `claimInterrupted` skips every saved helper whose owner PID is this process or a live one (`tasks_resume.go:141-161`, line 148).
  - `taskProcessAlive(0)` is true: it calls `unix.Kill(0, 0)`, which signals the caller's own process group and succeeds (`agent/tasks_directory_unix.go:33-36`).
  - Nothing on the resume path reads `closing`. `resumeInterrupted` claims, resumes, then reports jobs (`tasks_resume.go:163-177`). Neither `resume` (`tasks_resume.go:180-226`) nor `start` (`tasks_spawn.go:196-230`) checks it, and a resume that fails `forget`s the entry (`tasks_resume.go:169-172`).
  - The engine calls resume on every interactive `InitAgent` (`backend/agent.go:154-168`).
  - So in a long-lived engine, a helper interrupted while the engine runs on would never resume, and neither would a helper cut off by a teardown that this engine survives. See "Where the code contradicts the plan", item 2.
- **Turns no app starts.** Some turns start in a chat without any app:
  - helper results (`tasks.go:371-381`)
  - job-ended notices (`background_jobs.go:232-243`)
  - notices that a detached process ended (`background.go:210-217`)

### Change

S makes these changes:

- **`client_id` on two routes.** `SendMessage` and `InitAgent` take an optional `client_id`. The signatures become:
  - `backend.SendMessage(workspaceID, clientID string, msg proto.AgentMessage) error`
  - `backend.InitAgent(ctx, workspaceID, clientID string, interactive bool) error`
  - The Go client sends `client_id` on both.
- **Backend hooks.** S adds the hook calls (Build pieces, S12 to S14): `messageSent`, `agentInit`, `viewChanged`, `gone` and `teardown`. `gone` fires in every place an app's claim ends: `RetireClient`, `expireHold`, `releaseHoldLocked`, and the delete branch of `detachStream` (`clients.go:249-252`). In that branch the reason is `GoneReleased` when the app had released its claim, and `GoneExpired` otherwise (grace 0).

**Ownership follows the work.** An app owns:

- each prompt it sent, while that prompt waits in the queue or runs
- each helper or managed job started by a turn that one of its prompts started
- for a process that no hook saw start (the AI tool put it in the background itself): the owner of its chat's running turn, or else the app that last sent in that chat

Work that another open app started is never stopped when this app leaves.

B adds the ownership seam in the agent package (new `agent/ownership.go`):

```go
type WorkKind string

const (
    WorkHelper WorkKind = "helper"
    WorkJob    WorkKind = "job"
)

type WorkStart struct {
    Kind      WorkKind
    ID        string // task ID or managed job ID
    SessionID string // the chat it works for
    Resumed   bool   // a helper restarted by ResumeInterruptedTasks
}

// SetWorkStartHook installs fn, called synchronously when a helper or managed
// job starts, before the first event about it. fn returns the owning app's
// client ID ("" for none), stored as Task.StartedBy or the job's owner.
// nil uninstalls it. The terminal app never installs one.
func SetWorkStartHook(fn func(WorkStart) string)

// SetTaskStartedBy changes a helper's owner and publishes an updated event.
func SetTaskStartedBy(id, clientID string)

// InterruptTask ends a running helper the way shutdown does: it stays saved,
// marked interrupted and released (Owner 0), so the next
// ResumeInterruptedTasks in any Crush, this one included, starts it again.
// The parent is not told. An ended helper is a no-op.
func InterruptTask(id string) error

// InterruptJob ends a listed job the way shutdown does. A managed job is
// killed and saved as interrupted and released, with no "job ended" turn now;
// resume tells the agent, as after a restart. Any other listed process is
// killed, and its end is not reported to the agent.
func InterruptJob(p Process) error
```

B also adds one queue call, in a new file `agent/prompt_owner.go`. It needs no edit to `scheduler.go`, because Go methods can live in any file of the package:

```go
// StopOwned ends a leaving app's prompts in one chat. It runs under the
// session's dispatch mutex, so no other prompt can slip in between its steps.
// It never calls cancelLocked. A cancel raises the cancel mark over every
// accepted prompt that has not reached the scheduler (scheduler.go:529-537),
// and those carry no submission ID (scheduler.go:147), so a cancel would
// also end another app's prompt accepted at that moment (turn.go:146-155).
//  1. It drops the waiting prompts whose submission ID makes owned return
//     true, from messageQueue and interrupting, and publishes their cancelled
//     RunComplete the way ClearQueue does (publishCanceledQueueDrops,
//     scheduler.go:419-443). The other prompts keep their order.
//  2. It ends the running turn only when all of these hold, under the same
//     mutex:
//     - a turn is active (isRunning, scheduler.go:653-656)
//     - no queue handoff is in progress (Interrupt checks the same,
//       scheduler.go:566-568)
//     - no accepted prompt waits to start a new turn: an interrupt cancels
//       such a primary acceptance (queue_handoff.go:69-70), and its app is
//       unknown
//     - ownsTurn(queued) returns true, where queued holds the submission IDs
//       that QueuedPromptSummaries lists at that moment (queue_summary.go:17-30)
//     Then it interrupts: startQueueHandoffLocked(sessionID, true), as
//     Interrupt does (scheduler.go:560-570). That moves the handed prompts
//     and the queue into interrupting at once (queue_handoff.go:51-84).
//     Accepted follow-ups keep their place in the queue (queue_handoff.go:66-71),
//     so another app's prompt accepted a moment earlier still runs. StopOwned
//     then removes the owned calls from interrupting again, with their
//     cancelled RunComplete, before it unlocks. The other prompts run next.
//     An empty snapshot is already handled (queue_handoff.go:148-155).
// It returns how many prompts it dropped.
func (s *sessionScheduler) StopOwned(sessionID string, owned func(submissionID string) bool, ownsTurn func(queued []string) bool) int

// OwnedStopper is implemented by the coordinator. The backend type-asserts
// it, the way QueueSummaries does (agent/queue_summary.go:34-48), so the
// Coordinator interface (coordinator.go:40-65) and its mocks stay unchanged.
type OwnedStopper interface {
    StopOwned(sessionID string, owned func(submissionID string) bool, ownsTurn func(queued []string) bool) int
}
func (c *coordinator) StopOwned(sessionID string, owned func(string) bool, ownsTurn func([]string) bool) int // type-asserts c.currentAgent()
```

Prompts handed to an agent CLI but not taken in yet cannot be withdrawn while the turn runs (`scheduler.go:579-580`). Ending the turn frees them: the interrupt moves them into the snapshot (`takePendingSteered`, `cli_steps.go:99-111`; `queue_handoff.go:54-58`), where `StopOwned` drops the owned ones. So a leaving app's handed prompts stop with its own turn. Only a turn that another app started keeps them (see the rules).

**Why the decision is taken under the lock.** `ownsTurn` is the backend's test, "every running prompt of this chat is the leaving app's", but `StopOwned` calls it under the dispatch mutex, with the queue it is about to take. A test made before the lock could be stale: the leaving app's turn may have ended, and another app's prompt may have been queued, taken into the turn, or started its own turn in between. `QueuedPromptSummaries` takes no dispatch mutex (`queue_summary.go:17-30`), so `StopOwned` can call it while holding one.

The rest of B's agent changes, in the order they appear in the code:

- **Hook call sites:**
  - `spawn` (`tasks_spawn.go:48-109`): after the task is registered, before its first `publish`.
  - `resume` (`tasks_resume.go`): with `Resumed: true`.
  - `watchJob` (`background_jobs.go:174`): after `backgroundOwners` is set.
- **New hub map:** `taskHub.interrupting map[string]bool`, keyed by task IDs and job IDs.
  - `finish` treats an entry like `closing`: no `forget`, and the parent is not told (`tasks.go:329-330`, `351-354`).
  - `watchJob` treats it like `closing()` (`background_jobs.go:226-230`, `238-241`).
  - `reportDetached` skips detached processes that `InterruptJob` killed, matched by `detachedKey`.
- **Release helpers and jobs:** a new `releaseInterrupted(childIDs)` does what `markInterrupted` does (`tasks_resume.go:120-136`) and also sets `Owner = 0`. Jobs get the same release: `interruptJob` also sets `Owner = 0`, in the same edit that adds the time (`background_jobs_resume.go:82-101`). It still finds the entry through `ownJob` (lines 53-56), because the match runs before the change.
  - `InterruptTask` and `InterruptJob` use them.
  - **`stopAll` uses `releaseInterrupted` too**, in place of its `markInterrupted` call (`tasks.go:320`). Jobs cut off by shutdown already go through `interruptJob` (`background_jobs.go:226-230`, `238-241`). So helpers and jobs cut off because the last app closed are claimable by this same engine process when the project reopens.
- **Claim:** `claimInterrupted` gets a new first case, before line 148: `s.Owner == 0 && !s.Interrupted.IsZero()` means claim it (still within the 24 h window, line 150). It must come before line 148, because `taskProcessAlive(0)` reports owner 0 as alive (Current code facts). `claimInterruptedJobs` gets the same first case, before line 113.
- **Resume:** in `ResumeInterruptedTasks`, `resumeOnce sync.Once` becomes `resumeMu sync.Mutex`, and every call runs again. Claims keep this idempotent, because a claimed helper gets `Owner = self` (`tasks_resume.go:153`) under the file lock that every Crush shares (`tasks_resume.go:78-94`), and a claimed job leaves the file (`background_jobs_resume.go:117-118`).
- **Hub teardown** (`stopAll`, `tasks.go:307-324`) runs in this order:
  1. Take `hubsMu`, remove `h` from `hubs`, and release `hubsMu`. No other lock is held. From here on, no new copy of `hubs` holds this hub, so `ResumeInterruptedTasks`, `StopTask` and the session scans no longer find it.
  2. Remove `h.dir`, as today (line 308).
  3. Under `h.mu`: set `closing`, stop the running tasks, release them, and cancel the session runs. This is today's work, with the release.
  4. After `h.mu` is released, close `h.reaper` when it is set, then set `h.reaper` to nil.
  - **Twice per teardown.** `stopAll` runs twice for one teardown (`backend/workspace.go:139`, then `app/app.go:326`), so every step must be safe the second time: step 1 finds `h` already gone and does nothing, step 3 finds nothing running, and step 4 skips the nil reaper. Test: `TestStopAllTwiceIsHarmless` (agent): `stopAll` called twice on one hub with an injected reaper pipe → no panic, the pipe is closed once, and the saved helpers keep owner 0.
  - **Known short window (accepted).** Between step 1 and step 3, a new hub's first `InitAgent` still sees this PID as the owner and skips those entries (`tasks_resume.go:148`). They are released a moment later and wait for the next interactive `InitAgent` or the next engine. Microseconds wide; no work is lost.
  - **Lock order.** `stopAll` never holds `hubsMu` and `h.mu` together. Every other holder of both takes `hubsMu` first and `h.mu` inside it, or takes only one: `StopTask` (`tasks.go:193-204`), and `tasks.go:214`, `238`, `255`, `background.go:94`, `background_jobs.go:159`, `286`, `300`, `tree.go:195` and `tasks_resume.go:51`. So no cycle is possible.
  - **`h.root`** needs no extra close: `watch` closes it once `h.dir` is gone (`agent/tasks_inbox.go:16-30`), and `stopAll` removes `h.dir` in step 2.
  - **The reaper.** Closing it starts reaping the hub's leftover processes (`agent/reaper_linux.go:33-35`). Today that happens when the process exits. In an engine that lives on, it now happens at teardown.
  - A removed hub is never scanned again, so a new hub for the reopened project starts clean, and its task IDs cannot be shadowed.
- **A closing hub never claims or starts work.** A copy of `hubs` taken before step 1 can still reach the dying hub (Current code facts). Three checks close that window:
  - **Check and claim in one step.** `resumeInterrupted` takes `h.mu` first. If `closing` is set, it releases `h.mu` and returns: it claims nothing and reports no jobs. Otherwise it keeps `h.mu` while it runs `claimInterrupted` and `claimInterruptedJobs`, releases it, and only then resumes the claimed helpers and reports the claimed jobs. Step 3 sets `closing` and releases helpers under the same `h.mu`. So a claim runs either before the release, when this hub's own helpers still carry this PID and are skipped (`tasks_resume.go:148`), or not at all.
    - `stopAll` already edits the saved file under `h.mu` today (`markInterrupted`, `tasks.go:309-320`). No saved-file edit takes `h.mu` inside the file lock, so the order `h.mu`, then file lock, has no cycle.
  - **Jobs are claimed with the helpers.** `reportInterruptedJobs` takes the claimed jobs as a parameter, instead of claiming them itself (`background_jobs_resume.go:128-129`).
  - **No start on a closing hub.** `start` (`tasks_spawn.go:196-230`) checks `closing` under the `h.mu` it already takes (line 197). On a closing hub it registers nothing, calls `release()`, and returns the new `errHubClosing` ("Crush is closing"). Its result becomes `(Task, error)`.
    - `spawn` (`tasks_spawn.go:107`) and `continueTask` (`tasks_continue.go:105-159`, call at line 156) return that error to the agent. The test call at `subagent_limit_test.go:374` discards the results and still compiles.
    - `resume` returns it, and `resumeInterrupted` then saves that entry as released again (owner 0), instead of `forget`ting it (`tasks_resume.go:171`).
    - This covers a helper claimed just before `closing` was set. It waits for the next claim: the next app's interactive `InitAgent`, or the next engine.

B adds the backend side (`backend/live.go`):

```go
type liveState struct {
    mu         sync.Mutex
    prompts    map[string]promptInfo // submission ID → its app and chat; removed at its RunComplete
    seq        uint64                // acceptance counter for promptInfo.seq
    lastSender map[string]string     // chat root → client ID of the app that last sent in it (only for processes no hook saw)
    owners     map[workKey]string    // helper or job → client ID
    lastInit   string                // client ID of the latest interactive InitAgent
    // 5.9 state also lives here: shell-output coalescers and the update-check cache.
}

type promptInfo struct {
    client    string // the app that sent it
    sessionID string // the chat it was sent to
    root      string // that chat's root
    seq       uint64 // acceptance order in this workspace
}

type workKey struct {
    kind agent.WorkKind
    id   string
}
```

- **Every prompt gets a submission ID.** `SendMessage` (B, `backend/agent.go:31-62`) sets `msg.SubmissionID = uuid.NewString()` when it is empty, before acceptance. Effects on today's behavior:
  - The saved user message's text part carries that ID (`agent/prompt_messages.go:71`).
  - A queued prompt dropped by a cancel now publishes a cancelled `RunComplete` (`scheduler.go:308`, `422-439`).
  - An error before the run starts publishes an error `RunComplete` (`backend/agent.go:117-130`).
  - No caller waits on an ID it did not send, so nothing reacts to these extra events (inferred).
- **`messageSent`:** with a client ID, it stores `prompts[msg.SubmissionID]` and sets `lastSender[root]`. An empty client ID stores nothing. It runs in `SendMessage` after the closing check (`backend/agent.go:51-58`) and before `go b.runAgent` (line 60), so a prompt is in `prompts` before it can reach the scheduler.
- **The completion watcher:** started at the first interactive `agentInit` and stopped at teardown. It subscribes to `ws.RunCompletions()` (`app/app.go:260`) and deletes `prompts[rc.SubmissionID]` for each `RunComplete`. The map holds at most 4,096 entries; when it is full, the entry with the lowest `seq` is dropped and logged at debug level.
- **A chat's running prompts** are computed, never stored. They are the submission IDs in `prompts` for that chat, minus every ID that `QueuedPromptSummaries(sessionID)` lists (`agent/queue_summary.go:17-30`), counted only while `IsSessionBusy(sessionID)` is true. Prompts handed to an agent CLI are listed there too, so they count as waiting; `StopOwned` handles them.
- **The running turn's owner** is the client of the running prompt with the lowest `seq`, the prompt that started the turn. It is "" when the turn has no known prompt (a helper's result, a job notice, a summary, a CLI continuation).
- **`agentInit` (interactive):** sets `lastInit`. On the first such call it installs the work hook; teardown removes it.
- **The work hook:** for a resumed helper the owner is `lastInit`. Otherwise it is the running turn's owner in the chat `SessionID`. The hook records the owner in `owners` and returns it. A turn with no owner gives "" (no app).
- **`gone(ws, L, reason, workspaceEmpty)`:**
  - If `workspaceEmpty` is true, it does nothing more: teardown does today's full stop.
  - Otherwise it runs `applyQ2(ws, L)` in a new goroutine, never under `b.mu`.

`applyQ2(ws, L)` runs these steps:

1. **Find the viewers.** For every other registered app V (attached, or inside its own grace), map `root(ClientCurrentSession(V))` → V. When two apps view the same root, the lowest client ID wins.
2. **Helpers.** For each running helper owned by L (from `owners`), take its root chat:
   - If an app views it, transfer: update `owners`, then call `agent.SetTaskStartedBy`, which sends a `helper` event.
   - Otherwise call `agent.InterruptTask(id)`.
3. **Jobs.** A job in `BackgroundProcesses()` is L's when `owners` says so. When `owners` has no entry for it, it is L's if its chat's running turn is L's, or, with no running turn, if `lastSender[root(job.Session)] == L`. For each of L's jobs:
   - If an app views its chat, transfer it.
   - Otherwise call `agent.InterruptJob(job)`.
4. **Prompts.** For each chat C that holds prompts of L (from `prompts`):
   - **If an app V views C's root:** transfer them. Every `prompts` entry of L in C gets `client = V`. The queue and the running turn are untouched.
   - **Otherwise:** call `StopOwned(C, ownedBy(L), ownsTurn)`, through `OwnedStopper`. `ownsTurn(queued)` takes `liveState.mu` and returns true when C has at least one running prompt (a `prompts` entry for C whose ID is not in `queued`) and every running prompt is L's.
     - L's waiting prompts are dropped. The other apps' prompts keep their places.
     - L's running turn is interrupted. When another app's prompts wait, or were accepted a moment earlier, they run next.
     - A turn that runs another app's prompt keeps going.
   - Never the plain cancel route, because `Cancel` clears every app's queued prompts (Current code facts) and poisons every accepted prompt (`scheduler.go:529-537`).
   - **Lock order.** `applyQ2` holds no lock when it calls `StopOwned`. The order is the session's dispatch mutex, then `liveState.mu`. `messageSent` and the completion watcher never call the agent while they hold `liveState.mu`.
   - Delete L's dropped entries from `prompts`. Their cancelled `RunComplete` events would delete them anyway.

Helpers and jobs go first, so a helper or job ending cannot start a new turn in a chat whose turn was just stopped.

### Rules

- **Open app:** an app registered on the workspace. This includes an app inside its 10 s grace.
- **The 10-second grace:**
  - Reconnecting within the grace fires no `gone`, so nothing stops.
  - A drop past the grace fires `gone(GoneExpired)`, then Q2.
  - A clean close fires `gone(GoneRetired)`, then Q2 at once.
- **Viewing:** an app views a piece of work when the root of its current chat equals the root of the work's chat. The current chat may be one of the work's helper chats.
- **Its work, not the chat's.** When an app leaves, only its own prompts, helpers and jobs are stopped. Another open app's queued prompts in the same chat survive and run next, and so does one it sent a moment earlier that is not queued yet. A turn started by another open app's prompt keeps running, even with nobody viewing it.
- **Q2 interrupts; it never cancels.** The leaving app's turn ends the way the terminal's interrupt ends one. The plain cancel is never used, because it ends every app's queued and accepted prompts.
- **One case cannot follow both rules.** A prompt of the leaving app may have been handed to an AI tool inside a turn that another open app started. That prompt can only be withdrawn by ending the other app's turn. The built default keeps the other app's turn running, so the tool still reads that one prompt (open question 3).
- **Work with no owner.** Examples: a helper-result turn, a job notice, a summary, or a CLI continuation. Q2 does not stop it (one narrow timing exception is under Risks). It stops at teardown, as today.
- **Helpers stopped by Q2 resume:**
  - at the next interactive `InitAgent` on this engine, that is, the next app that opens the project
  - or in the next engine, which claims them because they have owner 0 or a dead PID
  - See open question 1.
- **The last app:** Q2 never runs when the last app leaves. Teardown does today's full stop. `stopAll` now takes the hub out of `hubs` first, then saves running helpers and jobs as interrupted **and released** (owner 0). A closing hub never claims or starts work. So when the project opens again, the helpers resume exactly once, and the cut-off jobs are reported once. That holds inside the same engine process, within its 10 s linger, while the old workspace is still shutting down, and in a later engine alike.

### Tests

| Test (package) | Proves | Why it can fail |
|---|---|---|
| `TestQ2ReconnectWithinGraceKeepsRun` (backend; grace 200 ms) | A sends; A's stream drops; A reattaches at 100 ms → the run never cancels | Q2 fires on detach |
| `TestQ2DropPastGraceStopsUnviewedWork` (backend) | A owns a running prompt, a queued prompt, a helper and a job in chat C; B is open on chat D; A drops; at 300 ms: A's turn ended, A's queued prompt dropped with a cancelled `RunComplete`, helper saved interrupted with owner 0, job killed with no "job ended" turn; B's work untouched | wrong scope, or the job end starts a turn |
| `TestQ2QueuedPromptsOfOtherOpenAppSurvive` (backend + agent) | A's prompt runs in C; B, open on chat D, queues a prompt in C; A queues one more; A leaves → A's turn ends and A's queued prompt is dropped; B's prompt runs next, in its place | the whole queue is cleared (the plain cancel) |
| `TestQ2TurnOfOtherOpenAppKeepsRunning` (backend + agent) | B's prompt runs in C, nobody views C, A has a prompt queued in C; A leaves → B's turn runs to its end; only A's prompt is dropped | the turn is stopped because the latest sender left |
| `TestStopOwnedNeverCancels` (agent, fake CLI with one of L's prompts handed to it) | nothing else waiting → L's turn ends, the handed prompt is dropped with a cancelled `RunComplete`, and the chat ends idle; B's prompt queued too → B's prompt runs next and L's handed prompt does not | the owned handed prompt runs in the next turn, or B's queued prompt is cleared with the turn (the plain cancel) |
| `TestStopOwnedKeepsAcceptedPromptOfOtherApp` (agent) | L's turn runs; B's prompt has been accepted (`BeginAccepted`) but its goroutine has not reached `Run` when `StopOwned` stops L's turn; B's goroutine then calls `Run` with that acceptance → B's prompt runs, and its `RunComplete` is not cancelled | the cancel path's mark makes B's prompt cancel on entry (`turn.go:146-155`) |
| `TestStopOwnedLeavesUnscheduledPrimaryAlone` (agent, as `queue_handoff_test.go:251-261` sets it up) | a primary acceptance that has not reached the scheduler, and an active turn; `StopOwned` with `ownsTurn` true → the acceptance is not cancelled and the active turn keeps running; only owned waiting prompts are dropped | the interrupt cancels the unscheduled primary (`queue_handoff.go:69-70`) |
| `TestStopOwnedDecidesUnderDispatchLock` (agent) | `ownsTurn` runs while the chat's dispatch mutex is held (a `TryLock` from inside it fails), and gets the IDs queued at that moment | the decision is taken before the lock, so a prompt queued or taken into the turn in between is missed |
| `TestEveryPromptGetsSubmissionID` (backend) | a `SendMessage` without `submission_id` is stored under a new UUID, and its `RunComplete` removes it | ownerless prompts, or entries never removed |
| `TestQ2KeepsWorkAnotherAppViews` (backend) | B views C → all keep going; A's prompt and helper owners become B; a `helper` event shows `started_by` = B. Then B retires as the last app → teardown stops everything as today | viewers ignored |
| `TestQ2RetireActsAtOnce` (backend) | `RetireClient` triggers Q2 without the grace | waits for the grace |
| `TestQ2DetachWithZeroGrace` (backend; `SetDetachGrace(0)`) | A's last stream drops → `gone(GoneExpired)` fires from `detachStream`, and Q2 stops A's unviewed work; after an explicit release, the reason is `GoneReleased` | the delete branch fires no hook, so A's work runs on |
| `TestQ2InterruptedHelperResumesOnNextJoin` (backend + agent) | after a Q2 interrupt, app E joins and runs `InitAgent(interactive)` → the helper restarts on its child chat with the same task ID, owned by E | `resumeOnce` or the owner rule blocks it |
| `TestQ2NoOwnerWorkUnaffected` (backend) | a turn with no owner keeps running when A leaves | stopped by mistake |
| `TestTeardownRecreateResumesOnce` (backend + agent, one process) | a helper runs; the last app leaves, so teardown runs; a new app opens the same project in the same process and runs `InitAgent(interactive)` twice → the helper starts exactly once, on the new hub, with its task ID; the old hub is no longer in `hubs`; the helper stays in `tasks.json` until it ends, and is removed then | the old hub stays listed and claims or forgets the entry, or the owner is still this PID so nothing resumes |
| `TestRecreateWhileTeardownRuns` (backend + agent, one process; the old workspace's `shutdownFn` waits on a gate, `backend/workspace.go:94-111`; 20 rounds) | a helper runs; the last app leaves, so teardown starts and stops at the gate; a new app creates the same project and runs `InitAgent(interactive)`; the gate opens and teardown finishes; the new app runs `InitAgent(interactive)` again → the helper starts exactly once, on the new hub, and stays in `tasks.json` until it ends | the dying hub claims the released helper and forgets it or runs it, or nothing resumes it |
| `TestClosingHubNeverClaims` (agent) | hub H1 has a running helper and a released job entry, and runs `stopAll`; then `H1.resumeInterrupted` runs, as a copy of `hubs` taken before the teardown would make it → no report turn runs on H1, the job entry stays in `background-jobs.json`, and the helper keeps owner 0; a second hub of the same project then resumes the helper exactly once and reports the job once | no `closing` check, so H1 claims the job (it leaves the file and is reported on a closing hub) and the helper |
| `TestTeardownRacingResumeKeepsWork` (agent; one running helper and one running job; 200 rounds, each starting `stopAll` and `resumeInterrupted` on one hub at once, then ending the job; run with `-race`) | after every round the closed hub has claimed nothing: the helper and the job are saved with owner 0, and no job report ran | `closing` is checked under `h.mu` but the claims run after the lock is released, so a job released in between is claimed and reported on a closing hub |
| `TestStartRefusedOnClosingHub` (agent) | on a closing hub, `start` registers nothing, calls `release` once, and returns `errHubClosing`; a resumed entry refused this way is saved again with owner 0, not forgotten | a task starts on a closing hub, or the refused entry leaves `tasks.json` |
| `TestStopAllUnregistersHub` (agent; the test sets `h.reaper` to the write end of an `os.Pipe()`, because tests start no reaper, `tasks.go:170-174`) | after `stopAll`, `SessionTasks` and `StopTask` no longer see the old hub's tasks, and a read on the pipe's read end returns `io.EOF` within 1 s | dead hubs still scanned, or the reaper is left open (the read times out) |
| `TestResumeTwiceIsIdempotent` (agent) | two resume calls start the helper once | double start |
| `TestClaimReleasedOwnerZero` (agent) | owner-0 helpers and jobs are claimed by the same process, even though `taskProcessAlive(0)` is true | case missing, or placed after `tasks_resume.go:148` or `background_jobs_resume.go:113` |
| `TestInterruptJobNoHandoff` (agent) | a killed managed job is saved as interrupted; `RunAccepted` never called | the job's end starts a turn |

Race detector: `go test -race ./internal/backend/ ./internal/agent/ -run 'Q2|Resume|Interrupt|Claim|Teardown|StopAll|StopOwned|SubmissionID|Recreate|ClosingHub|StartRefused'`.

### Risks

- **A process with no recorded owner** (a command the AI tool sent to the background itself). The fallback to the chat's running turn, then to its last sender, covers it.
- **Interrupt racing a helper's own finish.** `InterruptTask` does nothing to an ended helper, and both paths take `h.mu`.
- **Lost fan-in events** never decide ownership: the hook is synchronous, and `RunComplete` is a must-deliver event (`agent/agent.go:400`).
- **A prompt accepted a moment before Q2 runs** has not reached the scheduler, and its acceptance carries no submission ID (`scheduler.go:147`).
  - If it is another app's prompt, it still runs. As a follow-up it keeps its place through the interrupt (`queue_handoff.go:66-71`). As a new primary it stops Q2 from interrupting at all (`StopOwned`, step 2).
  - If it is the leaving app's own prompt, `StopOwned` cannot see it, so that one prompt may still run once.
  - Q2 never uses the plain cancel, whose cancel mark would poison every acceptance in flight (`scheduler.go:529-537`, `turn.go:146-155`).
- **A finished prompt counts as running for a moment.** The completion watcher removes a prompt when its `RunComplete` arrives, which is a little after the turn ends. If a turn with no owner starts in that moment and the leaving app's finished prompt is the only one left in `prompts`, `ownsTurn` returns true and Q2 interrupts that turn. This is the one way Q2 can stop work it does not own. It needs the leaving app's turn to end, and another turn to start, inside the few milliseconds before the watcher catches up.
- **Deadlocks.**
  - Q2 runs after `b.mu` is released, in its own goroutine.
  - `StopOwned` takes the session's dispatch mutex, as `Cancel` and `Interrupt` do, and then `liveState.mu` inside `ownsTurn`. Nothing takes them the other way round.
  - `stopAll` never holds `hubsMu` and `h.mu` together.
  - `resumeInterrupted` holds `h.mu` while it edits the saved files, as `stopAll` already does. While another Crush holds a file lock, the UI's helper list waits on `h.mu` for that long; file edits are a single read and write.

---

## 5.6 Tree actions

### Current code facts

- **Only the in-process app can do tree actions.** `workspace.TreeManagement` (`workspace/tree.go:12-19`) and `FileRestoration` (`workspace/file_restore.go:11-15`) are implemented only by `AppWorkspace` (`tree.go:21-69`, `file_restore.go:25-69`). The comment says they stay optional "until the server and phone protocols gain branch revision checks" (`tree.go:10-11`).
- **Storage.**
  - `TreeNode{MessageID, ParentID, Label, Active}` (`db/tree.go:12-17`)
  - `TreeEntry{TreeNode; Message}` (`message/tree.go:22-25`)
  - `TreeRevision(ctx, sid) (int64, error)` (`db/tree.go:61`; `message/tree.go:35`)
  - `SwitchTree` (`db/tree.go:72`), `LabelTree` (`db/tree.go:117`)
  - `SwitchTreeNote` bumps the revision (`db/tree.go:82-115`)
- **The idle guard.** `SwitchTree`, `NavigateTree` and `CopyTree` run inside `withIdleTree` (`agent/tree.go:14-18`, `71-132`, `160-167`). It:
  - holds the chat lock
  - refuses while a reply, an accepted prompt or a queued prompt exists (`tree.go:29-31`)
  - refuses while helpers run or wait to deliver (`tree.go:33-37`)
  - refuses while background work exists (`tree.go:38-46`)
  - closes the chat's links to AI tools (`tree.go:47`)
  - Its errors are plain text, so over HTTP they would come back as 500.
- **Copy.** `message.CopyTree` returns `(newID, prompt)`. It flushes pending writes. A fork moves to the user prompt's parent. It restores files and moves the undo record (`message/tree.go:103-143`).
- **The copy is invisible to other windows.** The copy is written with raw SQL (`db/tree.go:179-251`), so no session event is published for the new chat.
- **File restore types.**
  - `filehistory.Plan{SessionID, Target, Source, Revision, Entries, UndoID}` (`filehistory/restore.go:33-40`)
  - `Entry`, already with JSON tags (`filehistory/restore.go:21-32`)
  - `Result{Restored, Skipped, UndoID}` plus `Notice()` (`filehistory/restore.go:41-55`)
  - `UndoFiles` needs the agent coordinator (`file_restore.go:61-69`).

### Change

Proto (C, `proto/tree.go`):

```go
type TreeEntry struct {
    MessageID string  `json:"message_id"`
    ParentID  string  `json:"parent_id"`
    Label     string  `json:"label,omitempty"`
    Active    bool    `json:"active"`  // the chat's current head
    Message   Message `json:"message"` // proto.MessageFromDomain of the preview: ID, role and one text part (message/tree.go:48-50)
}
type Tree struct {
    Revision int64       `json:"revision"`
    Entries  []TreeEntry `json:"entries"`
}
type TreeLabelRequest struct {
    ExpectedRevision int64  `json:"expected_revision"`
    Target           string `json:"target"`
    Label            string `json:"label"` // "" removes the label
}
type TreeMoveRequest struct {
    ExpectedRevision int64  `json:"expected_revision"`
    Target           string `json:"target"`
    Summarize        bool   `json:"summarize,omitempty"` // navigate only
}
type TreeCopyRequest struct {
    ExpectedRevision int64  `json:"expected_revision"`
    Target           string `json:"target"`
    Fork             bool   `json:"fork"` // true = fork, false = clone
}
type TreeResult struct {
    Revision int64 `json:"revision"`
}
type TreeCopyResult struct {
    SessionID string `json:"session_id"` // the new chat
    Prompt    string `json:"prompt"`     // fork: the prompt for the typing box
    Revision  int64  `json:"revision"`   // the source chat's revision afterwards
}
type TreeChanged struct {
    SessionID string `json:"session_id"`
    Revision  int64  `json:"revision"`
}
type FilePreviewRequest struct {
    Target string `json:"target"`
    Fork   bool   `json:"fork"`
}
type FilePlan struct {
    PlanID    string          `json:"plan_id"` // the engine's handle for this plan
    SessionID string          `json:"session_id"`
    Target    string          `json:"target"`
    Source    string          `json:"source"`
    Revision  int64           `json:"revision"`
    UndoID    string          `json:"undo_id,omitempty"`
    Entries   []FilePlanEntry `json:"entries"`
}
type FilePlanEntry struct { // same JSON names as filehistory.Entry
    Path        string          `json:"path"`
    Before      json.RawMessage `json:"before"` // filehistory.State, encoded as today
    After       json.RawMessage `json:"after"`
    Action      string          `json:"action"`
    Adds        int             `json:"adds"`
    Dels        int             `json:"dels"`
    Conflict    string          `json:"conflict,omitempty"`
    Unavailable string          `json:"unavailable,omitempty"`
    GitHead     string          `json:"git_head,omitempty"`
    GitMoved    bool            `json:"git_moved,omitempty"`
}
type FileUndoRequest struct {
    PlanID string `json:"plan_id"`
}
type FileRestoreResult struct {
    Restored int      `json:"restored"`
    Skipped  []string `json:"skipped,omitempty"`
    UndoID   string   `json:"undo_id,omitempty"`
    Notice   string   `json:"notice"` // filehistory.Result.Notice()
}
```

Routes (C). The prefix `…` stands for `/v1/workspaces/{id}/sessions/{sid}`.

| Op | Method and path | id | Request | Response |
|---|---|---|---|---|
| `rpc.GetTree` | GET `…/tree` | none | `rpc.Empty` | `proto.Tree` |
| `rpc.GetTreeMessage` | GET `…/tree/messages/{mid}` | none | `rpc.Empty` | `proto.Message` (404 when `mid` is not in `{sid}`) |
| `rpc.LabelTree` | POST `…/tree/label` | optional | `proto.TreeLabelRequest` | `proto.TreeResult` |
| `rpc.SwitchTree` | POST `…/tree/switch` | optional | `proto.TreeMoveRequest` | `proto.TreeResult` |
| `rpc.NavigateTree` | POST `…/tree/navigate` | optional | `proto.TreeMoveRequest` | `proto.TreeResult` |
| `rpc.CopyTree` | POST `…/tree/copy` | optional | `proto.TreeCopyRequest` | `proto.TreeCopyResult` |
| `rpc.PreviewFiles` | POST `…/files/preview` | optional | `proto.FilePreviewRequest` | `proto.FilePlan` |
| `rpc.PreviewUndoFiles` | POST `…/files/undo-preview` | optional | `rpc.Empty` | `proto.FilePlan` |
| `rpc.UndoFiles` | POST `…/files/undo` | optional | `proto.FileUndoRequest` | `proto.FileRestoreResult` |

Go signatures (C):

```go
// agent/tree.go
var ErrTreeRevisionChanged = errors.New("the chat's branches changed")
var ErrTreeBusy = errors.New("chat busy")
// WithTreeRevision makes withIdleTree refuse with ErrTreeRevisionChanged when,
// with the chat lock held, the chat's revision differs from rev.
func WithTreeRevision(ctx context.Context, rev int64) context.Context
// withIdleTree's busy errors become treeBusyError{msg}. Its Error() is today's
// exact text, and Is(ErrTreeBusy) is true.

// session (optional interface, type-asserted on App.Sessions; no change to session.Service)
// Announce publishes a CreatedEvent for a chat written directly to the
// database (copies and branches).
func (s *service) Announce(ctx context.Context, id string) error

// backend/tree.go
func (b *Backend) Tree(ctx context.Context, workspaceID, sessionID string) (proto.Tree, error)
func (b *Backend) TreeMessage(ctx context.Context, workspaceID, sessionID, messageID string) (proto.Message, error)
func (b *Backend) LabelTree(ctx context.Context, workspaceID, sessionID string, req proto.TreeLabelRequest) (proto.TreeResult, error)
func (b *Backend) SwitchTree(ctx context.Context, workspaceID, sessionID string, req proto.TreeMoveRequest) (proto.TreeResult, error)
func (b *Backend) NavigateTree(ctx context.Context, workspaceID, sessionID string, req proto.TreeMoveRequest) (proto.TreeResult, error)
func (b *Backend) CopyTree(ctx context.Context, workspaceID, sessionID string, req proto.TreeCopyRequest) (proto.TreeCopyResult, error)
func (b *Backend) PreviewFiles(ctx context.Context, workspaceID, sessionID string, req proto.FilePreviewRequest) (proto.FilePlan, error)
func (b *Backend) PreviewUndoFiles(ctx context.Context, workspaceID, sessionID string) (proto.FilePlan, error)
func (b *Backend) UndoFiles(ctx context.Context, workspaceID, sessionID, planID string) (proto.FileRestoreResult, error)

// workspace/client_tree.go
var _ TreeManagement = (*ClientWorkspace)(nil)
var _ FileRestoration = (*ClientWorkspace)(nil)
```

- The backend implements tree actions through the existing `AppWorkspace` methods of the engine's workspace (`workspace.NewAppWorkspace(ws.App, ws.Cfg)`, `workspace/app_workspace.go:40`), with `WithTreeRevision(ctx, req.ExpectedRevision)`.
- `LabelTree` does not run under the chat lock. The backend compares the revision just before labelling.
- After each success, the backend reads the new revision and calls `SendAll(TreeChanged{…})`.
- For a copy, it also calls `Announce(newID)`, so every app sees the new chat as a `session` created event.
- `ClientWorkspace` remembers the revision from its last `Tree` read of each chat and sends it as the expected revision. If it has none, it reads the tree first.
- `ClientWorkspace` maps every plan it receives to its `plan_id`, so `UndoFiles(ctx, plan)` can send it.

### Rules

- **Revision check.** Every route that changes the tree carries `expected_revision`. On a mismatch it answers 409 `revision_conflict`, and nothing changes.
- **Busy rules** stay as today, scoped to the chat (5.4). A busy chat answers 409 `tree_busy` with today's text.
- **Fork and clone** still refuse while the AI replies, as today. Copying while the AI replies is the branch copy (5.11).
- **File plans.**
  - The engine keeps them in memory per workspace: up to 16, for 10 minutes each.
  - An unknown or expired `plan_id` answers 409 `plan_expired`.
  - A plan whose `Revision` no longer matches the chat answers 409 `revision_conflict`.
- **Change events.** `tree_changed` goes to every app after a switch, navigate, label or copy (for the source chat).
- **Phone.** The phone protocol gets no tree actions. It has none today.

### Tests

| Test (package) | Proves | Why it can fail |
|---|---|---|
| `TestTreeRoutesRoundTrip` (server + client) | list, label, switch, navigate (with and without summary, using a fake model), fork, clone | route or type drift |
| `TestTreeStaleRevisionRefused` (server) | 409 `revision_conflict`; head unchanged | check outside the lock or missing |
| `TestTreeBusyIs409` (server) | a reply in progress gives 409 `tree_busy` with today's text | plain error mapped to 500 |
| `TestTreeChangedEvent` (server) | the stream gets `tree_changed` with the new revision | not sent |
| `TestCopyAnnouncesNewChat` (server) | the stream gets a `session` created event for the copy | raw insert stays silent |
| `TestFilePreviewUndoByPlanID` (server) | preview, undo, restored count; an expired plan gives 409 `plan_expired` | plan lost or reused |
| `TestTreeMessageOtherChat404` (server) | a message from another chat is refused | unscoped read |
| `TestClientWorkspaceTreeAndFiles` (workspace) | the compile assertions above, plus one switch through a test server | interfaces not met |

### Risks

- **Large trees.** `Tree` returns one text preview per message, not whole messages (`message/tree.go:38-53`, built from `TreePreviews`). A full message comes from `GET …/tree/messages/{mid}`. A chat with thousands of messages still sends thousands of previews, which is acceptable for phase 1. A trimmed tree read is not in scope.
- **Race between check and write.** Contained: the check runs with the chat lock held, except for labels, which do not move the head.

---

## 5.7 Phone sharing (`/remote`)

**Changed 2026-10-21 (owner: "One remote per chat"; PLAN.md 15):** a share now belongs to a chat, not to an app. The engine keeps one share per chat (an app's unsent new chat has its own, which moves to the first chat that app shows); any app starts the share of the chat it shows (`remote/start` `{session_id, launch_id}`), every app hears of it in `remote_status` (which names `session_id`), any app stops it (`remote/stop` `{session_id}`), and `GET remote` lists the shares an app sees. Every app reports what its window shows (`remote/presence`) whether or not anything is shared. When the owning app leaves, the share passes to an app showing the chat, or to another app while the chat stays open; it ends with the last app. The rules below that say "one share per app" or "the share follows the owner's window" describe the earlier design (`internal/backend/share.go` has the current one).

### Current code facts

- **Starting a share.** `remote.StartContext(ctx, src)` checks Tailscale and starts one share (`remote/remote.go:112-133`, `135-170`).
  - The share offers `Actions()`, `PhonesChanged()`, `Stopped()`, `SetPresence`, `Status` and `Stop` (`remote.go:172-198`).
  - The phone's routes are at `remote.go:200-219`.
- **Phone actions run in terminal code.**
  - `handleRemoteAction` and `runRemoteAction` (`ui/model/remote.go:359-443`) do the work.
  - An action for another chat is refused with "the Crush window switched to another chat" twice: by the hub when it reads the request (`remote/remote.go:367-370`), and by the window (`ui/model/remote.go:375-377`).
  - Presence comes from the window (`ui/model/remote.go:341-353`).
  - The kinds are `remote.ActSend` … `remote.ActSummarize` (`remote/remote.go:42-56`). A phone body may carry attachments of up to 64 MiB in total (`maxBody`, `remote/remote.go:358`).
- **The hub's timing.** `dispatch` gives the window 5 s to take an action, else answers 503 "the Crush window is not responding". It then waits 30 s for the outcome, else answers 504 "the Crush window did not answer" (`remote/remote.go:375-397`).
- **What each action does in the terminal** (`ui/model/remote.go:378-442`):
  - `send`: needs the agent ready (line 381); `sendMessage` creates the window's new chat when there is none (`ui/model/send.go:79-86`, `ensureSession` at 37-55), saves skills picked before the chat existed (`send.go:139-143`), then calls `AgentRun`.
  - `cancel` is the same as `interrupt` (`send.go:309-316`): `AgentInterrupt` plus the window's own clean-up (`send.go:245-302`).
  - `background`: `AgentBackground` on the shown chat, then the window marks the tool group (lines 388-394).
  - `clear_queue`: `AgentClearQueue` (lines 395-399; `send.go:319-326`).
  - `yolo`: the window's YOLO toggle (lines 400-404; `input_mode.go:126-134`). In plan mode it also leaves plan mode (`switchPlanToYolo`, `input_mode.go:43-54`). The toggle writes the engine setting and the window's own cycle flag (`workspace_cache.go:352-367`).
  - `mode`: the window's code and plan cycle (line 434; `input_mode.go:16-36`, `62-114`). The window owns the mode, the editor prompt and the plan hand-off.
  - These are refused while the window is busy (lines 407-410): `model` (line 412), `effort` (414-424), `think` (425), `fast` (427-431), `ultracode` (432), `mode` (434) and `summarize` (436-440).
  - `model`, `effort`, `think`, `fast` and `ultracode` change the coder model's saved preference with `UpdatePreferredModel(ScopeGlobal, …)`, then call `UpdateAgentModel` (`model_select.go:403-475`, `fast_mode.go:12-77`; `remoteSelectModel` checks the provider and model first, `ui/model/remote.go:459-494`).
  - `summarize` calls `AgentSummarize` (`send.go:329-338`).
- **Client mode cannot share.** `remote.NewSource` needs a `*workspace.AppWorkspace` (`remote/source.go:53-57`), and the terminal refuses `/remote` in client mode (`ui/model/remote.go:176-179`).
- **Permission and question answers** go straight to the app's services through the Source (`source.go:21-51`, `85-91`).
- **The launch ID is process-wide.** `SetLaunchID` and `LaunchID` (`remote/launcher.go:47-56`). It is set from `CRUSH_REMOTE_LAUNCH` at start (`ui/model/ui.go:564-571`) and reported in every share as `Share.Launch` (`remote/hub_state.go:224-235`).
- **Setup steps** run as `crush remote setup <action>` in the foreground, so password prompts and the QR code reach the owner (`ui/model/remote.go:260-283`).
- **The engine has what a Source needs.** The engine's workspace embeds `*app.App` and holds `Cfg *config.ConfigStore` (`backend/workspace.go:52-56`). `func NewAppWorkspace(a *app.App, store *config.ConfigStore) *AppWorkspace` takes exactly those (`workspace/app_workspace.go:40-46`).

### Change

Proto (D, `proto/share.go`):

```go
type RemoteStart struct {
    LaunchID string `json:"launch_id,omitempty"` // the window's CRUSH_REMOTE_LAUNCH value, if the launcher opened it
}
type RemoteSetup struct { // remote.SetupInfo (remote/setup.go:32-38), as the terminal's setup dialog shows it
    State  string `json:"state"` // the remote.SetupState string: "ready" | "missing" | "unavailable" | "login" | "stopped" | "approval" | "waiting" | "unsupported" (remote/setup.go:19-30)
    Title  string `json:"title"`
    Detail string `json:"detail"`
    Action string `json:"action,omitempty"` // fix with `crush remote setup <action>`
    Button string `json:"button,omitempty"` // the main button's label, such as "Install Tailscale"; the dialog's first button (ui/dialog/remotecontrol/setup.go:52)
}
type RemoteStatus struct {
    State  string       `json:"state"` // "off" | "on" | "setup"
    Host   string       `json:"host,omitempty"`
    Port   int          `json:"port,omitempty"`
    Phones []string     `json:"phones,omitempty"`
    Setup  *RemoteSetup `json:"setup,omitempty"`
    Error  string       `json:"error,omitempty"` // a failed start that is not a setup issue
}
type RemotePresence struct {
    SessionID string `json:"session_id"` // "" on the start screen
    Focused   bool   `json:"focused"`
    Plan      bool   `json:"plan"`
}
type RemoteAction struct { // only the actions the window must run (see "Who runs each action")
    ActionID  string         `json:"action_id"`
    Kind      string         `json:"kind"` // "new_chat_send" | "yolo" | "mode"
    SessionID string         `json:"session_id"`        // the chat the phone saw; "" for new_chat_send
    Text      string         `json:"text,omitempty"`    // new_chat_send: the prompt the engine sends once the chat exists; the window may show it at once
    Uploads   []RemoteUpload `json:"uploads,omitempty"` // new_chat_send: what the engine keeps for this action
}
type RemoteUpload struct { // a reference only; the bytes stay in the engine
    UploadID string `json:"upload_id"`
    Name     string `json:"name"`
    Mime     string `json:"mime"`
    Size     int    `json:"size"`
}
type RemoteActionDone struct {
    Error     string `json:"error,omitempty"`      // "" means done
    SessionID string `json:"session_id,omitempty"` // new_chat_send: the chat the window created and now shows
}
```

Routes (D):

| Op | Method and path | id | Request | Response |
|---|---|---|---|---|
| `rpc.RemoteStart` | POST `/v1/workspaces/{id}/remote/start` | required | `proto.RemoteStart` | `proto.RemoteStatus` |
| `rpc.RemoteStop` | POST `/v1/workspaces/{id}/remote/stop` | required | `rpc.Empty` | `rpc.Empty` |
| `rpc.RemoteGet` | GET `/v1/workspaces/{id}/remote` | required | `rpc.Empty` | `proto.RemoteStatus` |
| `rpc.RemotePresence` | POST `/v1/workspaces/{id}/remote/presence` | required | `proto.RemotePresence` | `rpc.Empty` |
| `rpc.RemoteActionDone` | POST `/v1/workspaces/{id}/remote/actions/{aid}/done` | required | `proto.RemoteActionDone` | `rpc.Empty` |

Go signatures (D):

```go
// remote/remote.go
// StartShare is StartContext with the share's own launch ID. The hub stores
// it, and shareLocked reports h.launch instead of LaunchID() (hub_state.go:234).
// StartContext becomes StartShare(ctx, src, LaunchID()), so the terminal is unchanged.
func StartShare(ctx context.Context, src Source, launchID string) (*Server, error)
// NewTestAction builds an Action with a reply channel, for tests in other packages.
func NewTestAction(a Action) (*Action, <-chan error)

// backend/share.go
type shareServer interface {
    Actions() <-chan *remote.Action
    PhonesChanged() <-chan struct{}
    Stopped() <-chan struct{}
    SetPresence(remote.Presence)
    Status() remote.Status
    Stop()
}
var startShare = func(ctx context.Context, src remote.Source, launchID string) (shareServer, error) {
    return remote.StartShare(ctx, src, launchID)
}
type shareState struct { // field of backend.Workspace (added by S)
    mu     sync.Mutex
    shares map[string]*share // by owner client ID; one share per app
    forms  map[string]*secureForm // 5.8
    detach func()                 // 5.8
}
type share struct {
    owner   string
    srv     shareServer
    pending map[string]*pendingAction // action ID → waiting action, its timer and, for new_chat_send, its kept uploads
}
func (b *Backend) StartRemote(ctx context.Context, workspaceID, clientID, launchID string) (proto.RemoteStatus, error)
func (b *Backend) StopRemote(workspaceID, clientID string) error
func (b *Backend) RemoteStatus(workspaceID, clientID string) (proto.RemoteStatus, error)
func (b *Backend) SetRemotePresence(workspaceID, clientID string, p proto.RemotePresence) error
func (b *Backend) FinishRemoteAction(workspaceID, clientID, actionID string, done proto.RemoteActionDone) error
```

The engine builds the Source with `remote.NewSource(workspace.NewAppWorkspace(ws.App, ws.Cfg))`. The types fit: `backend.Workspace` embeds `*app.App` and holds `Cfg *config.ConfigStore` (`backend/workspace.go:52-56`), which is what `NewAppWorkspace(a *app.App, store *config.ConfigStore)` takes (`workspace/app_workspace.go:40`).

**Who runs each action.** The engine runs every action that does not need the window's own state, with the share owner's client ID, through the same backend calls the routes use. It forwards to the owner only the actions that change window state.

| Kind | Runs in | How |
|---|---|---|
| `send` to the shown chat | engine | `SendMessage(ws, owner, …)` with the phone's text and attachments, a new submission ID and the owner's client ID, so 5.5 counts it as the owner's prompt |
| `send` with `session:""` (the window's new chat) | window, then engine | forwarded as `new_chat_send`. The window creates its new chat as typing would (`send.go:79-86`, `139-143`), shows it, and answers done with its `session_id`. The engine then sends the text and the kept uploads to that chat, as for an ordinary send |
| `cancel`, `interrupt` | engine | `Interrupt(ws, sid)` (`backend/agent.go:288-297`). The terminal treats cancel as interrupt (`send.go:309-316`) |
| `background` | engine | B's `MoveToBackground(ws, sid)` (5.4). `moved:false` replies `no running command can move to the background` (`ui/model/remote.go:391`). The window's tool group follows from the tool's own result (inferred) |
| `clear_queue` | engine | `ClearQueue(ws, sid)`; a new chat is a no-op (`ui/model/remote.go:395-399`) |
| `model` | engine | the checks of `remoteSelectModel` (`ui/model/remote.go:459-494`), then `UpdatePreferredModel(ScopeGlobal, <coder model type>, selected)` and `UpdateAgent`. This matches `selectModel`'s path for a provider that is already set up (inferred; the builder confirms against `model_select.go:120` onward) |
| `effort`, `think`, `fast`, `ultracode` | engine | the same preference change and checks as `setReasoningEffort`, `toggleThinking`, `toggleFastMode` and `toggleUltracode` (`model_select.go:403-475`, `fast_mode.go:12-77`), then `UpdateAgent` |
| `summarize` | engine | `SummarizeSession(ws, sid)`, started in its own goroutine. The phone hears back at once, as from the terminal (`send.go:329-338`) |
| `yolo` | window | forwarded: it depends on the window's plan mode and cycle flag (`input_mode.go:43-54`, `126-134`; `workspace_cache.go:352-367`) |
| `mode` | window | forwarded: the window owns the input mode (`input_mode.go:16-36`, `62-114`) |

**Engine-run actions keep the terminal's checks and texts:**

- The chat check: the action's `session` must be the owner's current chat (`ClientCurrentSession`), with "the Crush window switched to another chat" (`ui/model/remote.go:375-377`). The hub has already checked it against the presence the owner sent (`remote/remote.go:367-370`).
- `send` with no agent: "the agent is not ready yet" (`ui/model/remote.go:380-382`).
- The busy refusal for `model`, `effort`, `think`, `fast`, `ultracode` and `summarize`: `errAgentBusy`'s text "Agent is busy, please wait..." (`ui/model/remote.go:45`, `407-410`). The engine's test is `AgentCoordinator.IsBusy()`, the same test `SetMainAgent` uses (`backend/ops_agent.go:79-87`). The engine cannot see the window's own `!` command or a prompt still being submitted (`ui/model/send.go:23-31`). That is the only difference from the terminal.
- `fast` without a Codex model: "FAST mode requires a Codex model" (`ui/model/remote.go:428-430`).
- `summarize` in a new chat: "a new chat has nothing to summarize yet" (`ui/model/remote.go:437-439`).
- The model errors: "that CLI is not set up on the computer" and "that model is not available on the computer" (`ui/model/remote.go:466`, `477`).
- An unknown kind: "unknown request <n>" (`ui/model/remote.go:442`).

**Uploads stay in the engine.** A phone attachment can be up to 64 MiB (`remote/remote.go:358`). An engine-run send passes the attachments straight to `SendMessage`. A forwarded `new_chat_send` keeps them in `pendingAction` and sends only `RemoteUpload` references in the event. They are freed when the action ends: done, failed or timed out. No route returns them, and no event carries their bytes.

### Rules

- **Start.**
  - One share per app. A second start from the same app returns the running share's status.
  - A setup problem (`*remote.SetupError`) answers `state:"setup"` with its info. Any other failure answers `state:"off"` with `error`.
  - The desktop runs `crush remote setup <action>` in a terminal window, because it needs password prompts and the QR code. This is desktop work (inferred need).
- **Action pump.** One goroutine per share takes each action from `srv.Actions()` **at once** and never waits on an app or on the work. The hub answers 503 when an action is not taken within 5 s (`remote/remote.go:377-381`).
  - An engine-run action starts in its own goroutine, which calls `a.Done` with the outcome.
  - A forwarded action:
    1. gets a UUID
    2. goes to the owner as a `remote_action` event, through `SendTo`
    3. If `SendTo` returns 0 (no stream, or every stream full), it is not taken: the pump replies at once with `a.Done(errors.New("the Crush window is not responding"))`, the hub's own 503 text (`remote/remote.go:380`).
    4. Otherwise it waits in `pending` with a 25 s timer. That is under the hub's 30 s wait (`remote/remote.go:393-394`), so the phone gets the engine's answer, not the hub's 504. When the timer runs out, the reply is `the Crush window did not answer`, the hub's 504 text.
  - Every action, engine-run or forwarded, gets exactly one `a.Done` within 25 s. An engine-run action that has not finished by then replies `the Crush engine did not answer`, and its result is only logged.
- **A forwarded `new_chat_send`.** The window answers done with the new chat's `session_id`, and must already show that chat (its presence names it). The engine then runs the send there, as for an engine-run send, and replies with that result. When `session_id` is missing, or is not the owner's current chat, the reply is "the Crush window switched to another chat".
- **Status changes.**
  - `PhonesChanged` sends `remote_status` to the owner.
  - `Stopped` removes the share, sends `remote_status` with `state:"off"` to the owner, and fails every pending action with `Remote Control is off`.
- **Owner-only routes.**
  - Presence, stop and done are owner-only. Others get 403 `not_owner`.
  - When the calling app has no running share, stop, presence and done answer 404 `share_off`. `GET …/remote` answers `state:"off"`.
  - Done for an unknown action answers 404 `action_not_found`.
  - In a done call, an empty `error` means `a.Done(nil)`; anything else means `a.Done(errors.New(error))`.
- **Following the chat.** The share follows the chat of the app that started it, through that app's presence calls. That is the plan's rule (`PLAN.md:83`).
  - The window runs only `new_chat_send`, `yolo` and `mode`, with the terminal's rules and texts for those (`ui/model/remote.go:375-404`, `434`).
- **Owner gone.** When the owner leaves (any `gone` reason, or teardown), the engine stops the share and fails pending actions with `the Crush window closed`.
- **Launcher (section 7).** A desktop window that the launcher opened reads `CRUSH_REMOTE_LAUNCH` from its environment and sends it as `launch_id`. That share reports it in `Share.Launch`, which is how the launcher finds the new window. Terminal shares keep the process-wide value.
- **Secure entry never reaches the phone** (5.8).

### Tests

| Test (package) | Proves | Why it can fail |
|---|---|---|
| `TestRemoteStartOneSharePerApp` (backend, fake `startShare`) | a second start returns the same share | duplicate listeners |
| `TestRemoteOwnerOnlyRoutes` (server) | stop, presence and done from another app → 403 | no owner check |
| `TestRemoteActionGoesToOwnerOnly` (server, `remote.NewTestAction`) | a forwarded `yolo` reaches the owner's stream as `remote_action`, the other app's stream gets nothing; done → reply nil | broadcast |
| `TestRemoteEngineRunsActions` (backend, fake coordinator) | `send`, `cancel`, `interrupt`, `clear_queue`, `summarize` and `effort` run in the engine and reply without any `remote_action` event; the send's prompt is stored as the owner's (5.5) | an action forwarded, or the prompt left ownerless |
| `TestRemoteEngineActionTexts` (backend) | another chat, agent not ready, busy, FAST without Codex, summarize in a new chat, and an unknown kind each reply the terminal's exact text | text drift |
| `TestRemoteNewChatSendKeepsUploadsInEngine` (server; a 5 MiB attachment) | the `remote_action` event carries only `uploads` references; after done with a `session_id`, the prompt with the attachment is sent to that chat; the event bytes on the stream stay under 4 KiB | attachment bytes put on the stream |
| `TestRemoteActionNotTakenFailsFast` (backend; the owner has no stream, then a full one) | the reply is "the Crush window is not responding" within 100 ms | the action waits for its timer |
| `TestRemoteActionTimeout` (backend, timer injected) | a forwarded action with no done gets "the Crush window did not answer" before 30 s; an engine-run action that hangs gets "the Crush engine did not answer" | the hub's 504 reaches the phone first |
| `TestRemotePumpNeverBlocks` (backend; a fake share that offers 3 actions at once, the first one slow) | all three are taken from `Actions()` within 100 ms | the pump runs actions inline, so the hub answers 503 |
| `TestRemoteShareStopsWhenOwnerLeaves` (backend) | retire → `Stop` called; pending failed | share outlives its app |
| `TestRemoteLaunchIDPerShare` (remote) | two shares report their own launch IDs; `StartContext` still reports `LaunchID()` | process-wide value |
| `TestRemoteStatusEvents` (server) | phones-changed and stopped reach the owner | the events go to every app, or `Stopped` leaves the share listed |

### Risks

- **Copying the terminal's action rules.** The engine now holds most of them (the table above). The window keeps `new_chat_send`, `yolo` and `mode` (desktop work). The texts are listed in CHECKLIST REM rows.
- **Workspace-wide settings.** Actions that change shared settings (model, YOLO) affect every app on the project. Today the terminal changes the same shared settings.
- **The busy test differs slightly** (the window's own `!` command is not seen). The worst case is a model change accepted while a window's `!` command runs, which the terminal would have refused. The command keeps running either way.

---

## 5.8 Secure entry

### Current code facts

- **Where forms come from.** `crush ask` with secure fields reaches `ask` (`agent/tasks_ask.go:32-128`), which calls `secureentry.OpenForm(req.Session, r, secure)` (line 67) and waits for the result. In a non-interactive run, a secure form is an error.
- **One form per process.** The process has one broker, with one deliverer and one open form (`secureentry/secureentry.go:33-38`). `OpenForm` fails:
  - with "secure entry requires the local Crush terminal" when no deliverer is attached
  - with "a secure entry is already open" while a form is open
  - (`form.go:110-129`)
- **No deliverer in the server.** Only the in-process terminal attaches one, through `app.Subscribe` → `secureentry.AttachForms` (`app/events.go:153`). So secure entry fails in the server today.
- **Forms hold metadata only.**
  - `Form{Session, Request}` holds only metadata (`form.go:14-29`).
  - `Field{Spec, QuestionID}` (`form.go:43-52`), with `Spec{File, Label, Placeholder, Occurrence}` (`secureentry.go:19-25`).
  - `Submit(answers, values map[string][]byte)` writes the values into their files (`form.go:232-299`). `Cancel` is at `form.go:354-360`.
  - `FormResult` never carries a value (`form.go:31-41`).
  - Each value is one line of at most 64 KiB, with no NUL, CR or LF (`secureentry.go:154-165`).
- **The phone cannot see forms.** The phone's Source has no secure-entry method (`remote/source.go:21-51`) and reads only the app fan-in (`source.go:63-65`). Forms never enter the fan-in.

### Change

Proto (D, `proto/share.go`):

```go
type SecureField struct {
    QuestionID string `json:"question_id"`
    Label      string `json:"label"`  // the field's name, e.g. "API key"; the only thing about it that travels (PLAN.md:84)
    Status     string `json:"status"` // "open" | "saved" | "unavailable", from Field.Saved and Field.Available (secureentry/form.go:57-67)
}
type SecureForm struct {
    FormID    string          `json:"form_id"`
    SessionID string          `json:"session_id"`
    Request   QuestionRequest `json:"request"` // existing proto.QuestionRequest; secure questions are metadata
    Fields    []SecureField   `json:"fields"`
}
type SecureFormClosed struct {
    FormID string `json:"form_id"`
    Reason string `json:"reason"` // "submitted" | "cancelled" | "answered_elsewhere" | "closed"
}
type SecureSubmit struct {
    Answers []QuestionAnswer  `json:"answers"` // existing proto.QuestionAnswer, for the ordinary questions
    Values  map[string]string `json:"values"`  // secure question ID → value
}
```

Routes (D):

| Op | Method and path | id | Request | Response |
|---|---|---|---|---|
| `rpc.ListSecureForms` | GET `/v1/workspaces/{id}/secure-forms` | required | `rpc.Empty` | `[]proto.SecureForm` (open forms sent to this app) |
| `rpc.SubmitSecureForm` | POST `/v1/workspaces/{id}/secure-forms/{fid}/submit` | required | `proto.SecureSubmit` | `rpc.Empty` (**hand-written handler**) |
| `rpc.CancelSecureForm` | POST `/v1/workspaces/{id}/secure-forms/{fid}/cancel` | required | `rpc.Empty` | `rpc.Empty` |

Go signatures (D):

```go
// secureentry/form.go
type FieldInfo struct { // no file path: the engine keeps it inside the form
    QuestionID, Label string
    Saved, Available  bool // Field.Saved and Field.Available (form.go:57-67)
}
func (f *Form) Fields() []FieldInfo        // in question order
// No new close accessor: the existing f.Done() <-chan struct{} (form.go:226) already
// returns closedSignal (form.go:28); Closed() bool (form.go:216) stays as it is.

// backend/secure.go
func (b *Backend) SecureForms(workspaceID, clientID string) ([]proto.SecureForm, error)
func (b *Backend) SubmitSecureForm(workspaceID, clientID, formID string, answers []question.Answer, values map[string][]byte) error
func (b *Backend) CancelSecureForm(workspaceID, clientID, formID string) error
```

Engine flow:

1. **Attach.** At the workspace's first interactive `agentInit`, the engine runs `detach := secureentry.AttachForms(func(f *secureentry.Form) { b.deliverSecureForm(ws, f) })`. Teardown calls `detach()`, which cancels an open form (`form.go:99-107`).
2. **Deliver.** `deliverSecureForm` gives the form a `form_id`. The recipients are every registered app whose current chat has the same root as `f.Session`. Each recipient gets `SendTo(SecureForm)`.
   - With no recipient, the form waits. The `viewChanged` hook delivers it when an app opens that chat.
   - A recipient whose `SendTo` returned 0 (no stream, or every stream full) did not get the form. It stays a recipient, and the form stays open. The app sees it in `ListSecureForms` after it reconnects, and `viewChanged` sends it again when the app opens the chat again.
3. **Close.** A goroutine waits on `f.Done()` and then sends `secure_form_closed` to every recipient. The recipient that acted gets `submitted` or `cancelled`. The others get `answered_elsewhere`.
4. **The submit handler** is hand-written, like the event stream route:
   - It reads the body through `http.MaxBytesReader(w, r.Body, 1<<20)`.
   - A decode failure answers 400 `bad_request` with the text `could not read the secure entry`. It is **not logged**, unlike `serve` (`server/rpc.go:73`), because a JSON error can quote a character of the body.
   - A caller that is not a recipient gets 403 `not_recipient`. An unknown or closed form gives 404 `secure_form_not_found`.
   - The handler converts the values to `map[string][]byte` and calls `form.Submit`. Afterwards it overwrites every byte slice with zeros.
   - A `*secureentry.FieldError` answers 422 `secure_field_error`, with `field` set to its `QuestionID` and its fixed text.
5. **Cancel:** recipients only; calls `form.Cancel()`.
6. **Unix socket only.** All three secure-form routes answer only on the engine's unix-socket listener. Each handler reads `r.Context().Value(http.LocalAddrContextKey)`. When that is not a `*net.UnixAddr`, the route answers 403 `local_only` (`backend.ErrLocalOnly`) and reads no body. The check runs before anything else, so the legacy server started with a TCP `--host` can never take a value over the network.

### Rules

- **The one path for values.** A value travels once: from the app to the engine, in the submit body, over the owner-only socket (5.1). There is no other way, because the engine is the process that writes the file (`Submit`, `form.go:232-299`).
- **Only labels travel.** A form's event and list answer name each field by its label. The destination path stays in the engine.
- **Field status.** Each field carries the status the terminal shows (`ui/dialog/questionform/secure.go:308-313`; CHECKLIST DLG-196, DLG-198): `saved` shows "Saved", `unavailable` shows "This file was already saved. Request this entry again to add it.", and `open` takes a value. After a submit fails with `secure_field_error`, the form stays open and is not resolved (`secureentry/form.go:228-231`), but files replaced before the failure stay saved (lines 277-288). The app then reads `ListSecureForms` again for the new statuses.
- **Values never appear in:**
  - events: forms carry metadata only
  - logs: the request log has no bodies, decode errors are not logged, and error texts are fixed
  - the phone: forms never enter the fan-in, and the Source has no secure entry
  - other apps: forms go only to recipients
  - any file except the destination file
- **One form at a time per engine** (the broker's rule). A second `crush ask` with secure fields gets today's "a secure entry is already open".
- **Several viewers.** When two apps view the chat, both get the form. The first submit or cancel wins.
- **Reconnect.** After reconnecting, the app calls `ListSecureForms`, because events sent while it was away are lost.
- **Cancelled turn.** If the turn that asked is cancelled, the asking code cancels the form (`agent/tasks_ask.go:84-87`), and the form-closed event follows.

### Tests

| Test (package) | Proves | Why it can fail |
|---|---|---|
| `TestSecureFormGoesOnlyToViewer` (server) | the viewer's stream gets `secure_form`; the other app's gets nothing | broadcast |
| `TestSecureFormEventHasNoPath` (server; the destination is `<temp>/deep/secret.env`) | the `secure_form` event bytes and the `ListSecureForms` answer contain the field's label, and neither the destination path nor its file name | the path or file name travels with the field |
| `TestSecureFieldStatus` (secureentry, inside the package; plus the backend mapping) | `Fields()` reports each field as `Field.Saved` and `Field.Available` do: a fresh field is open; once its file was written, a filled field is saved and an unfilled field of the same file is unavailable; `ListSecureForms` maps these to `open`, `saved` and `unavailable` | the statuses are not read from the form, so the app shows a saved field as still open |
| `TestSecureValueNeverLeaks` (server) | submit `S3CR3T-7f1c`; the string is in the destination file and absent from both apps' raw stream bytes, the captured `slog` output, and the stream of a phone hub built on the engine's Source | any leak |
| `TestSecureSubmitDecodeErrorNotLogged` (server) | malformed JSON containing the secret → 400; the log has no part of it | decoder error logged |
| `TestSecureFormDeliveredOnViewChange` (backend) | no viewer at open; an app opens the chat → it gets the form | pending forms lost |
| `TestSecureSubmitByNonRecipientRefused` (server) | 403 `not_recipient` | any app may submit |
| `TestSecureFieldErrorHasField` (server) | 422 with `field` | the error is mapped by its status only, so `field` is lost |
| `TestSecureEntryWorksInEngine` (backend) | `crush ask` with a secure field no longer fails with "requires the local Crush terminal" | deliverer not attached |
| `TestSecureFormClosedAnsweredElsewhere` (server) | two viewers; one submits; the other gets `answered_elsewhere` | the other viewer's form stays open, or gets `submitted` |
| `TestSecureRoutesRefuseNonUnixListener` (server) | the three routes served through an `httptest` TCP server answer 403 `local_only` and read no body; served on a unix-socket listener, submit works | the listener is not checked, or checked after the body is read |
| `TestSecureFormRedeliveredAfterFullStream` (backend; the recipient's stream full at open) | the form stays open and is listed by `ListSecureForms`; opening the chat again sends it | a dropped form is treated as delivered |

Every server test that submits or cancels a secure form serves on a unix-socket listener in a temporary folder, because a TCP test server gets `local_only` by design.

### Risks

- **Go strings cannot be wiped.** The decoded JSON string lives until garbage collection. The short lifetime, the absence of logging and the owner-only socket contain this.
- **Panics.** The panic handler logs panic values. `Submit` neither panics nor logs values (inferred). The secret-scan test also captures panic output, with the panic handler active.

---

## 5.9 Smaller gaps

### Current code facts

- **Live `!` output.**
  - The `RunShellCommand` route (`rpc/routes.go:111`) reaches a backend call that passes no `OnProgress` (`backend/agent.go:329-342`).
  - `ShellCommand.OnProgress` streams output when set (`backend/ops_agent.go:113-116`, `150-151`).
  - `progressWriter` hands each raw chunk to the callback (`shell/stream.go:13-27`), so a chunk can split a UTF-8 character.
  - `ClientWorkspace.AgentRunShellCommand` ignores `onProgress` (`workspace/client_workspace.go:238-242`).
- **`$VAR` check.** In client mode `Resolver()` is the identity resolver (`client_workspace.go:519-521`). The research note calls for a "has key" route on the engine side (`t8-connection.md:95`).
- **Memory warning.**
  - `memwatch.Default.Check()` runs every 10 s. It warns at 85 %, clears below 80 % and names the top 3 programs (`memwatch/memwatch.go:21-33`, `144-166`).
  - `Status{Percent, High, Episode, Top}` (`memwatch.go:70-80`) has `Message()` (`memwatch.go:83`).
  - One pop-up per 10 minutes across all windows, through `memwatch.ClaimNotify(config.GlobalCacheDir())` (`ui/model/memory_warning.go:55-78`; `memwatch_linux.go:72`). Linux only (`memwatch_other.go:7-12`).
  - The agent's memory note reads `memwatch.Default.Recent(memwatch.CheckEvery)` (`agent/memory_notice.go:13`).
- **Memory limit.**
  - `memlimit.Apply` and `memlimit.Usage` (`memlimit_linux.go:86`, `108`), and `memwatch.TotalRAM` (`memwatch.go:249`).
  - The terminal's dialog parses, saves and applies the limit (CHECKLIST DLG-267 to DLG-271 point at `internal/config/memory_limit.go:30` and `internal/ui/model/memory_limit.go:15-48`).
- **Update checks for the AI tools.**
  - `cliupdate.Check`, `Install`, `StartAutoCheck` (at most hourly), `WithoutDeclined` and `Decline` (`cliupdate/cliupdate.go:56`, `105`; `state.go:62`, `78`, `91`).
  - `Update{Name, Bin, Path, Current, Latest}` also has a private `args` field (`cliupdate.go:34-41`).
- **Update checks for Crush itself.**
  - `selfupdate.Dir`, `Check` and `Staged` (`selfupdate/selfupdate.go:43`, `72`, `268`).
  - A skipped Crush release is stored as a declined "Crush" update (`ui/model/selfupdate.go:43-45`, `89`).
  - The terminal's flows are at `ui/model/cliupdate.go:54-160` and `ui/model/selfupdate.go:49-108`.
  - The upstream notice already reaches clients as `update_available` (`server/events.go:99-107`).
  - Merge, build and install already exist as `crush self-update merge|build|install` (CHECKLIST UPD-005, UPD-007).
- **Usage limits.**
  - `Limit{Name, Used, ResetsAt, Model}` (`cliagent/limits.go:20-26`) lives in a process-wide store with `byKind`, `fetched` and `pending` (`limits.go:40-47`).
  - `setLimits` sets `fetched` (`limits.go:63-75`). A failed refresh sets it too, as a backoff (`limits.go:119-127`). So `fetched` does not prove that a reading exists.
  - Refreshes run at most every 3 minutes (`limits.go:38`, `101-104`). `RefreshLimitsNow` forces one (`limits.go:84-89`).
  - There are fetchers for Claude, Codex, AGY and Grok (`limits.go:133-138`), and turns also store readings (`claude_handlers.go:92`, `codex_handlers.go:149`).
  - Terminal callers: `composer_footer.go:237`, `sidebar.go:107`, `sidebar.go:119`, `usage.go:48`, `usage.go:67`.

### Change (all B)

**(a) Live `!` output.**

- `rpc.RunShellCommand` gains two optional query parameters: `client_id`, and `request_id` (at most 64 characters).
- With both set, the backend passes `OnProgress` and sends `shell_output` events to that one app.
- The backend method gains two parameters: `RunShellCommand(ctx, workspaceID, clientID, requestID string, …existing parameters)`.

```go
type ShellOutput struct {
    RequestID string `json:"request_id"`
    SessionID string `json:"session_id"`
    Seq       int    `json:"seq"`   // 0, 1, 2, … per request
    Chunk     string `json:"chunk"` // output since the previous event; whole UTF-8 characters only
}
```

- **Coalescing.** The engine joins chunks. Per request it sends at most one event per 33 ms, or sends at once when 16 KiB are waiting.
- **UTF-8.** The engine holds back an incomplete UTF-8 tail (at most 3 bytes) until the next chunk arrives or the command ends.
- **Source of truth.** The route's final `output` and `exit_code` stay authoritative. The events are best effort.
- **Client mode.** `ClientWorkspace.AgentRunShellCommand` sends a new UUID as `request_id` and feeds `onProgress` from the matching events.

**(b) "Is `$VAR` set".**

```go
type EnvCheck struct {
    Names []string `json:"names"` // bare names, e.g. "OPENAI_API_KEY"; at most 64
}
type EnvCheckResult struct {
    Set map[string]bool `json:"set"`
}
```

| Op | Method and path | id | Request | Response |
|---|---|---|---|---|
| `rpc.EnvIsSet` | POST `/v1/workspaces/{id}/env/is-set` | none | `proto.EnvCheck` | `proto.EnvCheckResult` |

- Each name must match `^[A-Za-z_][A-Za-z0-9_]{0,127}$`. Otherwise the route answers 400.
- "Set" means the name has a non-empty value in the environment the workspace's config resolver uses (inferred: the list the first app sent, `backend.Workspace.Env`, else the engine's own environment).
- The route never returns a value and never runs `$(…)`.
- `ClientWorkspace.Resolver()` keeps the identity resolver, because values must not cross the socket.

**(c) Memory.**

```go
type MemoryProgram struct {
    Name  string `json:"name"`
    Bytes uint64 `json:"bytes"`
}
type MemoryStatus struct {
    Supported     bool            `json:"supported"` // false where memory can't be read (not Linux)
    Percent       int             `json:"percent"`
    High          bool            `json:"high"`
    Episode       int             `json:"episode"`
    Top           []MemoryProgram `json:"top,omitempty"`     // only while high
    Message       string          `json:"message,omitempty"` // Status.Message() while high
    LimitBytes    uint64          `json:"limit_bytes"`       // saved memory-box limit; 0 = none
    BoxUsedBytes  uint64          `json:"box_used_bytes"`    // memlimit.Usage(memlimit.Slice); 0 = unknown
    TotalRAMBytes uint64          `json:"total_ram_bytes"`
}
type ClaimResult struct {
    Claimed bool `json:"claimed"`
}
type MemoryLimitRequest struct {
    Value string `json:"value"` // as typed: "", "0", "8", "8 GB", "7.5"
}
type MemoryLimitResult struct {
    LimitBytes uint64 `json:"limit_bytes"`
    Applied    bool   `json:"applied"`
    Notice     string `json:"notice"` // the terminal's exact outcome text (DLG-270)
}
```

| Op | Method and path | id | Request | Response |
|---|---|---|---|---|
| `rpc.MemoryStatus` | GET `/v1/memory` | none | `rpc.Empty` | `proto.MemoryStatus` |
| `rpc.ClaimMemoryNotice` | POST `/v1/memory/claim-notice` | none | `rpc.Empty` | `proto.ClaimResult` |
| `rpc.SetMemoryLimit` | PUT `/v1/memory/limit` | none | `proto.MemoryLimitRequest` | `proto.MemoryLimitResult` (400 `bad_request` with the terminal's validation text) |

- **Checking.** While it has a workspace, the engine calls `memwatch.Default.Check()` every `memwatch.CheckEvery`. It sends `memory_status` to every app when `percent`, `high` or `episode` changes. The same checks keep the agent's memory note current.
- **The pop-up claim.** `ClaimMemoryNotice` calls `memwatch.ClaimNotify(config.GlobalCacheDir())`. Terminal windows use the same claim, so one pop-up shows across every Crush window.
- **Setting the limit.** `SetMemoryLimit` does what the terminal's dialog does: parse, save to the global config, then `memlimit.Apply`. B puts this in a new shared function, `config.SaveMemoryLimit(value string) (limitBytes uint64, applyErr error, err error)`. The terminal keeps its own code until phase 7. These routes are listed under "Small additions that serve approved items".

**(d) Update checks.**

```go
type CLIUpdate struct {
    Name    string `json:"name"`
    Bin     string `json:"bin"`
    Path    string `json:"path"`
    Current string `json:"current"`
    Latest  string `json:"latest"`
}
type CLIUpdateCheck struct {
    Skipped bool        `json:"skipped"` // automatic check not due yet (hourly)
    Updates []CLIUpdate `json:"updates"`
    Errors  []string    `json:"errors,omitempty"`
}
type CLIUpdateBins struct {
    Bins []string `json:"bins"`
}
type CLIUpdateResult struct {
    Name   string `json:"name"`
    Latest string `json:"latest"`
    Error  string `json:"error,omitempty"`
}
type CLIUpdatesInstalled struct {
    Results []CLIUpdateResult `json:"results"`
}
type CrushRelease struct {
    Tag     string `json:"tag"`
    Current string `json:"current"`
    Dir     string `json:"dir"`
}
type CrushUpdateCheck struct {
    HasCheckout bool          `json:"has_checkout"`      // selfupdate.Dir() != ""
    Release     *CrushRelease `json:"release,omitempty"` // nil: on the latest stable release
    Staged      string        `json:"staged,omitempty"`  // already built (selfupdate.Staged)
    Declined    bool          `json:"declined"`          // skipped before
    Error       string        `json:"error,omitempty"`
}
type CrushTag struct {
    Tag string `json:"tag"`
}
```

| Op | Method and path | id | Request | Response |
|---|---|---|---|---|
| `rpc.CheckCLIUpdates` | GET `/v1/updates/clis` (query `manual=true\|false`) | none | `rpc.Empty` | `proto.CLIUpdateCheck` |
| `rpc.DeclineCLIUpdates` | POST `/v1/updates/clis/decline` | none | `proto.CLIUpdateBins` | `rpc.Empty` |
| `rpc.InstallCLIUpdates` | POST `/v1/updates/clis/install` | required | `proto.CLIUpdateBins` | `rpc.Empty`, Status 202 |
| `rpc.CheckCrushUpdate` | GET `/v1/updates/crush` (query `manual=true\|false`) | none | `rpc.Empty` | `proto.CrushUpdateCheck` |
| `rpc.DeclineCrushUpdate` | POST `/v1/updates/crush/decline` | none | `proto.CrushTag` | `rpc.Empty` |

- **Check modes.** An automatic check (`manual=false`) passes the `StartAutoCheck()` gate first; when the gate refuses, the answer is `skipped:true`. It then filters with `WithoutDeclined`. A manual check runs `Check` straight away. Both use a 1-minute timeout, as the terminal does (`ui/model/cliupdate.go:65`).
- **Install.**
  - The engine keeps the last check's `cliupdate.Update` values, including their private `args`, for 10 minutes. Install uses those values, and checks again first when it has none.
  - All updates install at the same time, as the terminal does (`ui/model/cliupdate.go:144-160`).
  - The results go to the requesting app as `cli_updates_installed`.
- **Decline.** Decline uses `{Name, Bin, Latest}` from the last check. Bins that were not in the last check are ignored.
- **Crush release.**
  - `declined` is `len(cliupdate.WithoutDeclined([]cliupdate.Update{{Name: "Crush", Bin: "crush", Latest: tag}})) == 0`.
  - Decline declines that same record.
  - Merging, building and installing a new Crush stay in the existing `crush self-update merge|build|install` commands, which the desktop runs itself.
- **Computer-wide.** These routes are about the computer, not the project. The state files are shared (`cliupdate/state.go`), so every engine gives the same answers.

**(e) Usage limits.**

```go
type LimitWindow struct {
    Name     string    `json:"name"` // "5h", "Weekly" or a model name
    Used     float64   `json:"used"` // percent, 0-100
    ResetsAt time.Time `json:"resets_at,omitzero"`
    Model    string    `json:"model,omitempty"`
}
type CLILimits struct {
    Kind    string        `json:"kind"`             // catwalk type string of the AI tool
    State   string        `json:"state"`            // "checking" | "ready" | "error"
    ReadAt  time.Time     `json:"read_at,omitzero"` // when this engine got the reading (UTC); zero until the first
    Error   string        `json:"error,omitempty"`
    Windows []LimitWindow `json:"windows"`
}
type LimitsSnapshot struct {
    CLIs []CLILimits `json:"clis"`
}
```

| Op | Method and path | id | Request | Response |
|---|---|---|---|---|
| `rpc.Limits` | GET `/v1/limits` | none | `rpc.Empty` | `proto.LimitsSnapshot` |
| `rpc.RefreshLimits` | POST `/v1/limits/{kind}/refresh` | none | `rpc.Empty` | `rpc.Empty`, Status 202 |

```go
// cliagent/limits.go
type KindLimits struct {
    Kind    catwalk.Type
    Limits  []Limit
    ReadAt  time.Time // set only by setLimits
    Err     string    // last refresh error since the last reading
    Pending bool
}
func Snapshot() []KindLimits                  // one per kind in limitFetchers, sorted by kind
func SetLimitsHook(fn func(kind catwalk.Type)) // called after setLimits and after a failed refresh
```

- **Store changes.** `limitStore` gains two maps:
  - `readAt`: set by `setLimits`, which also clears `lastErr`
  - `lastErr`: set by a failed refresh
- **State.**
  - `ready` when `ReadAt` is set
  - `error` when there is no reading and `lastErr` is set
  - `checking` otherwise
- **Refresh at start.** At its first workspace creation, the engine runs `go cliagent.RefreshLimitsNow(ctx, kind)` for every kind in `limitFetchers`. An AI tool that is not installed ends in `error`.
- **Event.** The hook sends `limits` (with that kind's `CLILimits`) to every app.
- **Refresh route.** It starts `RefreshLimitsNow` and answers 202 at once. The result arrives as a `limits` event.
- **Per account, newest wins.** Limits belong to the account, not the project. The desktop keeps one set of bars per kind and shows the reading with the newest `read_at` from any engine it is connected to. Until a reading exists, the bars show "checking…", never 0.

### Rules

- **No values.** None of these routes or events carries an environment value, a file's contents or a secret.
- **Best effort.** `shell_output` and `memory_status` are best effort; the app falls back to the route results.

### Tests

| Test (package) | Proves | Why it can fail |
|---|---|---|
| `TestShellOutputStreamsToRequesterOnly` (server) | `printf` in chunks → ordered `seq`, joined chunks equal the final output; the other app gets nothing | broadcast or lost order |
| `TestShellOutputSplitsOnRuneBoundaries` (backend) | a 3-byte character split across two writes arrives whole | raw chunk forwarding |
| `TestEnvIsSetNeverReturnsValues` (server) | set and unset names; the body holds booleans only; `$(id)` and `A=B` are refused | value or expansion leaks |
| `TestMemoryStatusRouteAndEvent` (backend, injected watcher) | the route mirrors the watcher; a change of `high` sends one event | an event per check instead of per change, or none at all |
| `TestClaimMemoryNoticeOncePerWindow` (server, temp cache folder) | the first claim is true, the second false | the claim is kept per engine, so every app shows the pop-up |
| `TestSetMemoryLimitMatchesTerminal` (server) | same validation texts and notices as DLG-269 and DLG-270 | text drift |
| `TestCLIUpdateAutoGate` (server, fake checker) | automatic check skipped within the hour; manual never skipped; declined filtered | the gate is bypassed, so every app start checks again |
| `TestCLIUpdateInstallUsesLastCheck` (backend) | install of a bin from the last check calls `Install` with its private args; the result event reaches the requester | args lost |
| `TestCrushUpdateDeclined` (server) | a declined tag reports `declined:true` | `declined` is computed from another record than the one `Decline` writes |
| `TestLimitsCheckingThenReady` (server, fake fetchers) | `checking` until the first reading, then `ready` with `read_at`; an event on each change | 0 shown as a reading |
| `TestLimitsErrorState` (server) | a failing fetcher gives `error` with its message; the backoff timestamp is not a reading | `fetched` used as `read_at` |
| `TestEngineRefreshesLimitsAtStart` (backend, fake fetchers) | one forced refresh per kind at the first workspace | no refresh at start, so the bars show "checking…" until a turn reports usage |

### Risks

- **Several engines each refresh at start.** That is one fetch per open project per account. It is acceptable, because each engine then backs off for 3 minutes.
- **Wall clock.** `read_at` uses each engine's wall clock. All engines run on the same computer, so the clocks agree.

---

## 5.10 Project lock

### Current code facts

- **Only exclusive locks exist.** `lock.File(ctx, path)` and `lock.TryFile(path)` (`lock/lock.go`) both take an exclusive lock: `unix.Flock(fd, LOCK_EX|LOCK_NB)` (`lock/lock_unix.go`), or `LockFileEx` exclusive (`lock/lock_windows.go`).
- **The lock file** (`db/datadirlock.go`):
  - `ErrDataDirLocked` (line 19)
  - file `crush.lock` (line 23)
  - owner info `{pid, version, started_at}` written into it (lines 29-33)
  - `acquireDataDirLock` with `TryFile` (lines 51-80)
  - `CRUSH_SKIP_DATADIR_LOCK` (lines 83-86)
  - `contendedLockError` (lines 120-131)
- **Only the server takes it.** The lock is opt-in through `WithDataDirLock` (`db/connect.go:74-76`; used at `backend/create.go:186`).
- **The terminal takes no lock.** The in-process terminal opens the database without one (`cmd/workspace_setup.go:128`). So today a terminal window and a server can both run AI work on one project.
- **Other commands.** `crush run` in-process uses the same setup (`setupLocalApp`, `cmd/run.go:176`). `crush session` and `crush stats` read the database directly (CHECKLIST MISC-014, MISC-017).
- **When "work alone" exists.** PLAN 5.10 speaks of a terminal window "set to work alone" (`PLAN.md:90`). That setting arrives with the switch-over in phase 7 (`PLAN.md:93`; 5.13). Until then, the brief's "one or both open at the same time" (`~/.cache/crush-gui-briefs/brief.md:28`) must keep working.

### Change (A)

```go
// lock/lock.go, lock_unix.go, lock_windows.go
// TryFileShared takes a shared lock without waiting; same return type as TryFile.
// unix: unix.Flock(fd, unix.LOCK_SH|unix.LOCK_NB)
// windows: LockFileEx with LOCKFILE_FAIL_IMMEDIATELY and without LOCKFILE_EXCLUSIVE_LOCK
func TryFileShared(path string) (func(), error) // like TryFile (lock.go:59-75): the func drops the lock and closes the file

// db/projectlock.go
type ProjectLockMode int

const (
    ProjectLockEngine ProjectLockMode = iota // exclusive, for the engine's whole life
    ProjectLockAlone                         // shared, for a terminal window that works alone
)

// LockProject takes the lock on <dataDir>/crush.lock. The engine waits up to
// wait for it; a lone window passes 0. On conflict it returns an error
// wrapping ErrDataDirLocked whose text is one of the messages below.
func LockProject(ctx context.Context, dataDir string, mode ProjectLockMode, wait time.Duration) (release func() error, err error)

const (
    MsgEngineHoldsProject      = "This project is open in the Crush engine (PID %d) that the desktop app and shared terminal windows use, so Crush can't also work alone here. Close those windows, then try again."
    MsgLoneWindowHoldsProject  = "This project is open in a Crush terminal window that works alone, so the shared engine can't start. Close that window, then try again."
    MsgOtherEngineHoldsProject = "This project is already open in another Crush engine (PID %d). Close its windows, or wait a minute, then try again."
)
```

- **Owner info.** The owner info gains `"kind":"engine"`. Only the engine writes it, after it holds the exclusive lock. Lone windows never write the file, because several shared holders would overwrite each other.
- **Engine.** `crush engine serve` calls `LockProject(ctx, D, ProjectLockEngine, 5*time.Second)` before it listens.
  - If the exclusive lock fails, it tries `TryFileShared` on a new descriptor.
  - If the shared lock succeeds, the holders are lone windows: it releases the shared lock and reports `MsgLoneWindowHoldsProject`.
  - If the shared lock also fails, another engine holds the project: it reports `MsgOtherEngineHoldsProject` with the PID from the owner info.
  - Either way it writes `start-error.json` with code `project_locked` and exits 4.
- **Lone window, from phase 7 only.** Phase 1 builds and unit-tests `TryFileShared` and `LockProject(…, ProjectLockAlone, …)`, but `setupLocalApp` does not call them yet. In phases 1 to 6 it takes no lock, as today, so a terminal window and the desktop app can both open a project.
  - When the phase-7 setting `options.engine` exists (5.13), `setupLocalApp` (`cmd/workspace_setup.go:91-163`) calls `LockProject(ctx, dataDir, ProjectLockAlone, 0)` when the setting is `alone`. It does so before `db.Connect` (line 128) and releases the lock at shutdown.
  - On conflict, startup fails and the message is printed as setup errors are today (inferred).
- **Legacy server.** Its exclusive `WithDataDirLock(true)` (`create.go:186`) stays as it is. For this rule it counts as an engine. Since 5.2, only an explicit `--host` reaches it.
- **Test override.** `CRUSH_SKIP_DATADIR_LOCK` skips both modes.
- **Commands without a lock.** `crush session`, `crush stats`, `crush logs` and `crush projects` take no lock, as today.
- **`crush logout`.** It joins the project's live engine when there is one (5.2), so it never meets the engine's lock. With no engine running it uses the legacy server, as today.

### Rules

- **Who excludes whom.** From phase 7, the engine and lone windows exclude each other. Lone windows can run side by side, because their locks are shared. There is one engine per project, from phase 1.
- **How long the engine holds it.** The engine holds the lock for its whole process life, including the 10 s linger.
- **Handover.** A new engine waits up to 5 s for an exiting engine to let go.
- **Crashes.** The lock is a `flock`, so it dies with the process. A crash never leaves the project locked.
- **Messages.** Each side shows its message exactly as above:
  - The desktop app shows the `message` from `crush engine ensure`'s error JSON (code `project_locked`).
  - The terminal prints it at startup (from phase 7, when it works alone).
- **`crush run`.** From phase 7, `crush run` goes through the engine (5.13). In shared mode it never takes the shared lock.

### Tests

| Test (package) | Proves | Why it can fail |
|---|---|---|
| `TestSharedLocksCoexist` (lock, Linux and Mac) | two shared holders on two descriptors | wrong flag |
| `TestExclusiveBlocksShared` / `TestSharedBlocksExclusive` (lock) | each way refuses at once | `LOCK_NB` missing, so the call waits instead of refusing |
| `TestEngineRefusedWhileLoneWindowOpen` (serverctl, engine subprocess) | the test itself holds `LockProject(…, ProjectLockAlone, 0)`, as a phase-7 lone window will; ensure exits 4 with `project_locked` and `MsgLoneWindowHoldsProject` | the engine cannot tell a shared holder from another engine |
| `TestLoneLockRefusedWhileEngineRuns` (db) | while an engine holds the lock, `LockProject(…, ProjectLockAlone, 0)` fails with `MsgEngineHoldsProject` and the engine's PID | the owner info is not read, so the PID is missing |
| `TestSetupLocalAppTakesNoLockBeforePhase7` (cmd) | with no `options.engine` setting, `setupLocalApp` opens a project while an engine holds it | the lone lock taken before its setting exists |
| `TestEngineWaitsForPreviousEngine` (db) | a holder that releases after 1 s → the second engine gets the lock within 5 s | no wait |
| `TestSkipLockEnvSkipsBoth` (db) | the test override still works | only the exclusive mode honours the override |

### Risks

- **Phase 1 to phase 7.** Until phase 7, a terminal window takes no lock, as today. So a lone terminal window and the engine can both run AI work on one project, just as a terminal window and `crush server` can today. Phase 7's setting closes this gap (see "Decisions made").
- **Network file systems** may ignore `flock`. That is the same as today's lock.

---

## 5.11 Branch copy

### Current code facts

- **`db.CopyTree(ctx, source, target, newID)`** (`db/tree.go:177-251`):
  - It names the new chat with the title plus " (copy)" (line 202) and checks the target belongs to the source (lines 193-201).
  - It copies the ancestry of `target`, in order (lines 205-225), as new messages with the columns id, session_id, role, parts, model, provider, is_summary_message, created_at, updated_at and finished_at (lines 228-229).
  - It does **not** copy the four `prism_*` columns, which migration `20260902000000_add_prism_model_to_messages.sql` added.
  - It copies file history points and changes, tree labels and branch summaries (lines 233-244), and sets `summary_message_id` (line 246).
  - It does not copy review details, kept skills, the context budget, read-file records or to-dos.
  - "Native CLI links are deliberately absent" (lines 177-178).
  - Each insert passes through the `tree_append` trigger, which hangs the new message under the new chat's head and makes it the head (`20261003120000_session_tree.sql:25-30`).
- **The copy flow today.** `message.CopyTree` flushes and restores files (`message/tree.go:103-143`). `agent.CopyTree` refuses while the chat is busy (`agent/tree.go:29-31`, `160-167`).
- **Tables involved:**
  - `messages` with the `prism_*` columns (`20260902000000`)
  - `message_review_details(message_id PK → messages ON DELETE CASCADE, payload)` (`20261003000000`)
  - `session_context_budgets(session_id PK, provider, model, token_limit, estimated)` (`20261006000000`)
  - `session_kept_skills(session_id, skill_id, name, position, turned_off, by_agent)` (`20261011000000`, `20261012000000`)
  - `read_files(session_id, path, read_at)` (`20260127000000`)
  - `sessions.todos` (`20250812000000`)
  - Migration style: plain `-- +goose Up` / `-- +goose Down` (`20261011000000_session_kept_skills.sql`).
- **Auto-title.** `sessionAgent.GenerateTitle` renames the chat, falling back to a default name on failure (`agent/title.go:27-43`). It runs for a chat's first message (inferred from `IsFirstMessage`, `backend/ops_agent.go:117`, `166-168`).
  - It is a method of `sessionAgent`, which holds `sessions session.Service` and `messages message.Service` (`agent/agent.go:178-200`). It has no database handle.
  - `BranchOrigin` (below) lives on `db.Queries`. The message service reaches its queries through `s.q`, with optional interfaces checked by type assertion (`treeStorage`, `message/tree.go:26-36`, `39-42`).
- **Message IDs during a turn.**
  - The assistant message ID goes into the step context (`agent/turn_stream.go:118`, `122`), but the CLI step loop throws that context away (`agent/cli_steps.go:241-247`).
  - Tool messages are created in `turnFileReview.save` (`agent/file_review.go:96-127`), called from `turn_stream.go:249-265` and `turn_finish.go:133`. `save` keeps the new message's ID in `r.results` only for tracked file tools (`file_review.go:123-125`).
  - `cliStream` builds the CLI step driver (`agent/cli_turn.go:57`, `118`). The turn calls it at `turn.go:338` and sets its `fileReview` at `turn.go:341`.
- **The AI tools' own IDs.**
  - Claude lines carry `uuid`, but `claudeLine` does not read it (`cliagent/claude.go:17-48`; `--resume` at line 298).
  - Codex turn IDs pass `codex_handlers.go:85`.
  - OpenCode and Grok run over ACP (`cliagent.go:160-188`; `acp.go:194`, `263-265`, `282`).
  - AGY uses `--conversation` (`agy.go:68`).
  - `cliagent.Event` has no field for any of these IDs (`cliagent.go:64-79`).
  - The link record keeps `native`, `through` and the tree revision (`cliagent.go:287-300`; saved at `agent/cli_turn.go:211-228`).

### Change (all C)

**Migration** `internal/db/migrations/20261018000000_chat_branches.sql` (full file):

```sql
-- +goose Up
-- A link from highlighted words in a message to the branch chat they opened.
-- Each branch has exactly one origin link (copied_from IS NULL): its parent
-- chat and source message. Links inside a copied part of a chat come along
-- into the copy as extra rows with copied_from set, so sibling branches stay
-- reachable. copied_from has no foreign key on purpose: a copied link stays
-- a copy even after the link it came from is gone.
CREATE TABLE chat_branches (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    parent_session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    source_message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    highlight_text TEXT NOT NULL,
    -- Offsets in UTF-16 code units into the source message's text.
    highlight_start INTEGER NOT NULL,
    highlight_end INTEGER NOT NULL,
    copied_from TEXT,
    created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX chat_branches_origin ON chat_branches(session_id) WHERE copied_from IS NULL;
CREATE INDEX chat_branches_parent ON chat_branches(parent_session_id);
CREATE INDEX chat_branches_message ON chat_branches(source_message_id);

-- Each AI tool's own ID for a Crush message, recorded as replies arrive, so
-- a branch can fork the tool's memory at the cut.
CREATE TABLE cli_message_markers (
    message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    cli TEXT NOT NULL,            -- catwalk type of the AI tool
    native_session TEXT NOT NULL, -- the tool's own session or thread ID
    marker TEXT NOT NULL,         -- the tool's own message ID, turn ID or prompt index
    marker_kind TEXT NOT NULL,    -- 'message' | 'turn' | 'prompt_index' | 'step'
    created_at INTEGER NOT NULL,
    PRIMARY KEY (message_id, cli)
);
CREATE INDEX cli_message_markers_session ON cli_message_markers(session_id, created_at);

-- +goose Down
DROP TABLE cli_message_markers;
DROP TABLE chat_branches;
```

`created_at` uses the same unit as `sessions.created_at`: Unix seconds (`db/migrations/20250424200609_initial.sql:13`).

**Database code**, hand-written like `db/tree.go` and `db/review_details.go`. There are no sqlc changes, so `db/querier.go` and `db/models.go` stay untouched.

```go
// db/branches.go
type ChatLink struct {
    ID, SessionID, ParentSessionID, SourceMessageID, HighlightText string
    HighlightStart, HighlightEnd int
    CopiedFrom string // "" for the origin link
    CreatedAt  int64
}
type BranchCopy struct {
    Source     string   // source chat
    MessageIDs []string // the cut: ancestry from the root through the last copied message, in order
    NewID      string
    Title      string
    Origin     ChatLink // filled except ID, SessionID and CreatedAt
}
// CopyBranch copies MessageIDs into a new top-level chat in one transaction,
// with the extras listed below, and inserts the origin link. It returns the
// map from old to new message IDs.
func (q *Queries) CopyBranch(ctx context.Context, c BranchCopy) (map[string]string, error)
func (q *Queries) ChatLinks(ctx context.Context) ([]ChatLink, error)
func (q *Queries) BranchOrigin(ctx context.Context, sessionID string) (ChatLink, bool, error)
func (q *Queries) LinksOnMessage(ctx context.Context, messageID string) ([]ChatLink, error)

// db/markers.go
type MessageMarker struct {
    MessageID, SessionID, CLI, NativeSession, Marker, MarkerKind string
    CreatedAt int64
}
func (q *Queries) RecordMarker(ctx context.Context, m MessageMarker) error // INSERT OR REPLACE
func (q *Queries) MarkersForSession(ctx context.Context, sessionID string) ([]MessageMarker, error)
```

**Shared copy extras.** `CopyBranch` and today's `CopyTree` both use the same helpers, so fork and clone gain these too.

- **Per copied message** (old → new):
  - the four `prism_*` columns, added to the message insert
  - its `message_review_details` row
  - its `cli_message_markers` rows
  - its `chat_branches` rows, as copies: a new `id`, the same `session_id`, `parent_session_id` = the new chat, `source_message_id` = the new message, and `copied_from` = the old row's `id`
- **Per chat:**
  - all `session_kept_skills` rows
  - the `session_context_budgets` row
  - all `read_files` rows
  - `sessions.todos`
- **Not copied:**
  - `cost`, `prompt_tokens` and `completion_tokens` start at 0, so usage reports do not count the source twice (open question 2).
  - `parent_session_id` stays empty.
- **The tree trigger needs no change.** `CopyBranch` inserts the messages in path order, and `tree_append` links each one under the one before it (`20261003120000_session_tree.sql:25-30`).

**The branch call** (new `message/branch.go`; an optional interface type-asserted on the message service):

```go
type BranchRequest struct {
    SessionID      string // source chat
    MessageID      string // the message holding the highlight (user or assistant)
    HighlightText  string
    HighlightStart int // UTF-16 code units into the message's text
    HighlightEnd   int
    Title          string
}
type BranchResult struct {
    SessionID     string // the new chat
    LinkID        string
    CopiedThrough string // last source message copied
    Copied        int
}
type BranchService interface {
    Branch(ctx context.Context, req BranchRequest) (BranchResult, error)
}
var (
    ErrBadCut                = errors.New("bad cut")
    ErrCutNotFinished        = errors.New("cut not finished")
    ErrHighlightOverlapsLink = errors.New("highlight overlaps a link")
)
```

`Branch` runs these steps in order:

1. **Flush.** Call `FlushAll(ctx)`. This writes the pending updates of finished messages. It must not wait for the running reply (inferred; `TestBranchWhileReplying` proves it).
2. **Check the message.** Load M.
   - M must belong to `SessionID`. Otherwise fail with `ErrBadCut`: `the message belongs to another chat`.
   - M must be a user or assistant message. Otherwise fail with `ErrBadCut`: `branch from message text, not tool output`.
   - Let `t` be `M.Content().Text`, measured in UTF-16 code units. The highlight needs `0 <= start < end <= len(t)` and `t[start:end] == HighlightText`. Otherwise fail with `ErrBadCut`: `the highlight doesn't match the message`.
3. **M must be finished.** An assistant M must have finished: `M.IsFinished()`, true once M has a `Finish` part (`message/content.go:301-308`). Otherwise fail with `ErrCutNotFinished`: `this reply is still being written`. This matches the greyed-out "Branch from here".
4. **No overlap.** A link on M whose `[s, e)` overlaps `[start, end)` fails with `ErrHighlightOverlapsLink`.
5. **Path.** Collect M's ancestry from `tree_nodes`, with the same recursive query as `db/tree.go:205-208`.
6. **Tool results.** If M ran tools, add the tool messages under M whose results answer M's tool calls.
   - When M has more than one such child, take the newest.
   - If any of M's calls has no result yet, fail with `ErrCutNotFinished`: `this reply's tools are still running`.
   - Everything before M on the path is finished, because a reply in progress is always below M.
7. **Title.** Trim the title. An empty title fails with `ErrBadCut`: `the branch needs a title`. Store at most 200 characters.
8. **Copy.** `db.CopyBranch` writes the new chat with a new UUID, in one transaction.
9. **Announce.** Announce the new chat (5.6 `Announce`). The backend then sends `chat_link` created events.

**Leaving the source untouched.** The branch path never:

- restores files or moves the undo record
- writes to the source chat's rows, head or revision
- touches any link to an AI tool
- checks whether the source chat is busy

**Auto-title guard** (C). An optional interface on the message service, in `message/branch.go`:

```go
// BranchOrigins reports whether a chat is a branch. The message service
// implements it by type-asserting its queries (s.q) to an interface with
// db.Queries.BranchOrigin, as Tree does with treeStorage (message/tree.go:39-42).
type BranchOrigins interface {
    BranchOrigin(ctx context.Context, sessionID string) (link db.ChatLink, ok bool, err error)
}
```

In `GenerateTitle` (`agent/title.go:27`), right after the empty-prompt check (lines 28-30) and before the deferred fallback (lines 32-43):

```go
if bo, ok := a.messages.(message.BranchOrigins); ok {
    if _, isBranch, err := bo.BranchOrigin(ctx, sessionID); err == nil && isBranch {
        return
    }
}
```

So neither a generated title nor the fallback name ever replaces a branch title. A lookup error keeps today's behavior (a title is generated). A message service without the interface (upstream stores, mocks) also keeps today's behavior.

**Markers.**

```go
// cliagent/cliagent.go: new field on Event
Marker *Marker

type Marker struct {
    Native string // the tool's session or thread ID
    ID     string // the tool's message ID, turn ID or prompt index
    Kind   string // "message" | "turn" | "prompt_index" | "step"
    For    string // "assistant" (the reply message) | "tool" (the latest tool-result message)
}
```

Where each tool's markers come from:

| Tool | Where | Marker |
|---|---|---|
| Claude | `claudeLine` gains a `UUID string` field with JSON name `uuid` | assistant lines → `{Native: session ID, ID: uuid, Kind: "message", For: "assistant"}`; user lines with tool results → same with `For: "tool"` |
| Codex | turn completion (`codex_handlers.go:85` has the turn ID) | `{Native: thread ID, ID: turn ID, Kind: "turn", For: "assistant"}` |
| OpenCode | ACP updates (inferred that they carry the message ID; the builder confirms in `acp.go`) | `{Kind: "message"}` per assistant message |
| Grok | ACP `session/prompt` count (`acp.go:194`, `282`) | `{Kind: "prompt_index"}` at the end of each prompt |
| AGY | step index, when present (inferred) | `{Kind: "step"}`; recorded only, because AGY uses the written copy (Q3) |

How markers are recorded:

- `cliSteps.begin()` keeps the assistant message ID from the step context, instead of discarding it (`cli_steps.go:241-247`).
- **The last tool message, with no signature change.** `turnFileReview` gains a field `lastTool string`. `save` sets it to the new message's ID whenever the create succeeds, tracked tool or not (`file_review.go:120-125`).
  - `r.results` alone is not enough, because it holds only tracked file tools (`file_review.go:123-125`), and a tool marker can follow any tool.
  - `save` keeps its `error` result, so its two callers stay as they are.
- **Getting it to the step driver.** `cliStream` gains a last parameter, `lastToolMessage func() string`, which it stores on `cliSteps` at `cli_turn.go:118`. The call at `turn.go:338` passes `func() string { if t.fileReview == nil { return "" }; return t.fileReview.lastTool }`. The closure reads the field only when it is called, so it works although `fileReview` is set three lines later (`turn.go:341`).
- `cliSteps.handle` (`cli_steps.go:293-331`) writes `RecordMarker` for every event that carries a `Marker`, after the event's own handler has returned without an error. Every case of its switch returns at once (for a tool result, `return s.handleToolResult(e)` at lines 327-328), so C moves the switch into a helper and records the marker after calling it. A `For: "assistant"` marker goes on the current assistant message. A `For: "tool"` marker goes on `lastToolMessage()`.
- **The tool message exists by then.** The whole tool-result path is synchronous, inside that one call of `handle`: `handleToolResult` ends with `return s.sc.OnToolResult(tr)` (`cli_steps.go:486`). `OnToolResult` is `t.onToolResult` (`turn.go:495`), which calls `t.fileReview.save` (`turn_stream.go:249-265`, line 260), and `save` creates the tool message before it returns (`file_review.go:120-122`). `handle` runs on the CLI's read loop (`cli_steps.go:54`), the same goroutine as `save`, so `lastTool` needs no lock.
- A failed write is logged and never fails the turn.

**Fork seam (built in phase 4, fixed now).**

- The first send in a branch with an AI tool reads the branch's origin and the markers at the cut, then asks that tool's forker.
- Until phase 4, every tool takes today's written-copy handoff. That is what a chat without a tool link already gets (inferred).
- Phase 4 starts by re-checking each tool live (PLAN section 6).

```go
// cliagent/fork.go (phase 4)
type ForkPoint struct{ Native, Marker, MarkerKind string }
type Forker interface {
    // Fork makes a new native session from p, or returns ErrNoFork to use the written copy.
    Fork(ctx context.Context, p ForkPoint) (native string, err error)
}
var ErrNoFork = errors.New("fork not available")
```

**Proto and routes** (C, `proto/tree.go`):

```go
type BranchRequest struct {
    MessageID      string `json:"message_id"`
    HighlightText  string `json:"highlight_text"`
    HighlightStart int    `json:"highlight_start"` // UTF-16 code units into the message text
    HighlightEnd   int    `json:"highlight_end"`
    Title          string `json:"title"` // already shortened by the app, as in the demo
}
type ChatLink struct {
    ID              string `json:"id"`
    SessionID       string `json:"session_id"`        // the branch the link opens
    ParentSessionID string `json:"parent_session_id"` // the chat that shows the link
    SourceMessageID string `json:"source_message_id"`
    HighlightText   string `json:"highlight_text"`
    HighlightStart  int    `json:"highlight_start"`
    HighlightEnd    int    `json:"highlight_end"`
    Origin          bool   `json:"origin"` // true: the branch's own parent link (used for nesting)
    CreatedAt       int64  `json:"created_at"`
}
type Branch struct {
    Session       Session  `json:"session"` // the new chat
    Link          ChatLink `json:"link"`    // its origin link
    CopiedThrough string   `json:"copied_through"`
    Copied        int      `json:"copied"`
}
```

| Op | Method and path | id | Request | Response |
|---|---|---|---|---|
| `rpc.CreateBranch` | POST `/v1/workspaces/{id}/sessions/{sid}/branches` | optional | `proto.BranchRequest` | `proto.Branch` |
| `rpc.ListChatLinks` | GET `/v1/workspaces/{id}/chat-links` | none | `rpc.Empty` | `[]proto.ChatLink` |

```go
// backend/branch.go
func (b *Backend) CreateBranch(ctx context.Context, workspaceID, sessionID string, req proto.BranchRequest) (proto.Branch, error)
func (b *Backend) ChatLinks(ctx context.Context, workspaceID string) ([]proto.ChatLink, error)
```

**Event.** `chat_link` with inner type `created` goes to every app, for the origin link and for each copied link. Links disappear only through database cascades, when a chat or message is deleted. So the app reads the list again after any `session` deleted event.

### Rules

- **Busy source.** The copy works while the AI replies in the source chat, and that reply carries on undisturbed.
- **The cut.**
  - The copy runs through the message with the highlight.
  - An AI reply that ran tools brings its tool results along.
  - A user message ends the branch, so the AI sees it unanswered before the next prompt.
- **Finished messages only.**
- **What comes along:** review details, kept skills, context budget, read-file records, to-dos, price fields, links in the copied part and tool markers. Fork and clone get these too.
- **Nesting.** `parent_session_id` stays empty. Lists nest branches using the origin links.
- **Title.** The title is the request's title, and auto-titling never replaces it.
- **Overlapping highlights** are refused.
- **Deleting a parent.** Its links cascade away, so its branches have no origin link left and become top-level.
- **Closing a chat** is desktop state, kept in the desktop settings file. It needs no engine state.
- **Branches of branches** work the same way, because a branch is a normal chat.
- **The terminal app** lists branches as normal chats. It needs no change.

### Tests

| Test (package) | Proves | Why it can fail |
|---|---|---|
| `TestCopyBranchThroughCutWithToolResults` (db) | the path ends with M plus the tool messages answering M's calls | cut too short or too long |
| `TestCopyBranchCopiesExtras` (db) | review details, kept skills, budget, read files, to-dos, `prism_*`, markers and links in the cut all present; cost and tokens 0 | a table missed |
| `TestCopyTreeNowCopiesExtras` (db) | fork and clone get the same extras | only the new path updated |
| `TestCopyBranchLeavesSourceUntouched` (db) | source rows, head and revision byte-identical before and after | source changed |
| `TestCopyBranchLinksComeAlong` (db) | a link on a copied message is copied with `copied_from`; the branch has one origin | lost sibling links |
| `TestDeletingParentMakesBranchTopLevel` (db) | the origin row is gone; the branch survives | cascade wrong |
| `TestChatBranchesMigrationUpDown` (db) | goose up, down, up | DDL error |
| `TestBranchRefusesToolOutputAndBadHighlight` (message) | `ErrBadCut` cases, including a highlight after an emoji (UTF-16 offsets) | offsets measured in bytes, so a highlight after a non-ASCII character is refused or misplaced |
| `TestBranchRefusesUnfinished` (message) | a reply in progress or pending tool results → `ErrCutNotFinished` | partial copy |
| `TestHighlightOverlapRefused` (message) | 409 `highlight_overlaps_link` over HTTP; touching ranges `[0,5)` and `[5,9)` are allowed | the overlap test uses closed ranges, or the error maps to 500 |
| `TestBranchWhileReplying` (backend, fake streaming agent; a 1,000-message chat) | a branch made mid-reply holds only finished messages; the reply finishes in the source; the call returns while the reply streams; the copy takes under the time bound below | busy guard or lock reused, or the copy holds the database too long |
| `TestBranchTitleNeverAutoReplaced` (agent) | the first send in a branch keeps the title, even when title generation fails | fallback rename |
| `TestBranchParentSessionIDEmpty` (server) | the new session has `parent_session_id` "" and a `chat_link` origin | the copy fills the helper-chat column, so chat lists hide the branch |
| `TestUntrackedToolGetsItsMarker` (agent, fake CLI) | the result of a tool with no file tracker (a shell command) gets its `For: "tool"` marker, on that tool's own message | the marker reads `r.results`, which only tracked tools fill (`file_review.go:123-125`), or it is recorded before the tool message exists |
| `TestClaudeMarkersRecorded` (agent, fake Claude stream) | assistant and tool markers on the right Crush messages | context dropped |
| `TestCodexTurnMarkers`, `TestOpenCodeMessageMarkers`, `TestGrokPromptIndexMarkers` (agent, fake tools) | one marker per turn, message or prompt | stream format drift |

These are the "fake-tool tests for fork cut points" that PLAN section 8 asks for. Phase 4 adds the fork tests themselves.

### Risks

- **One database connection.** The copy transaction briefly blocks the reply's writes. It is kept to one transaction with no network work inside.
  - **The bound.** In `TestBranchWhileReplying`, the copy of a 1,000-message chat made mid-reply must take under 500 ms. During the copy, the fake reply's writes must not stall for longer than that either.
  - **A slow machine.** The test first copies the same chat with no reply running, as a baseline, in the same run. When the baseline itself takes over 250 ms, the bound becomes twice the baseline. So a slow machine moves the bound, while a copy that blocks the reply still fails.
- **A branch ending with a user message.** API providers that require turns to alternate may refuse the next request (inferred). `TestBranchWhileReplying` also sends once in such a branch through the fake provider. If a real provider refuses, the agent's request builder needs to merge the two turns (see "Decisions made").
- **Marker formats depend on each tool.** The fake-tool tests pin them, and phase 4 re-checks them live.

---

## 5.12 Event batching (desktop, Rust side)

### Current code facts

- **The Rust crate is a skeleton.** It holds only `desktop/src-tauri/src/main.rs`.
- **The engine writes one line per event.** Each event is one `data: <json>\n\n` line (`server/handlers_workspace.go:121-181`), using the envelope from section 0.1.
- **Several event types carry the whole object every time:**
  - `message` (`server/events.go:72-76`)
  - `session` (`events.go:77-81`)
  - `file` (`events.go:82-86`)
  - LSP and MCP states (`events.go:28-47`)
  - `helper`, `limits`, `memory_status`, `tree_changed` and `prompt_pending` (this document)
- **The speed bar (PLAN section 8):** a scripted 2-minute stream, run 3 times. 95 % of frames must take 20 ms or less. A frame over 50 ms fails the run if it happens more than once in a run, or at the same moment in two runs.

### Change: the batching contract

The batcher is a pure module, `desktop/src-tauri/src/batch.rs` (piece D), with unit tests in the same file. Phase 3's engine client feeds it. The screens pull from it.

```rust
pub enum Kind { Created, Updated, Deleted }

pub struct Event {
    pub ty: String,               // envelope "type"
    pub kind: Kind,               // inner "type"
    pub payload: serde_json::Value,
}

pub enum Entry { Event(Event), Resync }

pub struct Batcher { /* … */ }

impl Batcher {
    pub fn new(max_entries: usize) -> Self; // the app uses 5_000
    pub fn push(&mut self, ev: Event);
    pub fn take(&mut self) -> Vec<Entry>;   // everything since the last take; leaves it empty
}

/// Parses one SSE line. Lines without "data:" and bad JSON give None.
pub fn parse_sse_line(line: &str) -> Option<Event>;
```

**Event types that may be merged.** Events with the same key are merged:

| `type` | Key | How they merge |
|---|---|---|
| `message` | `payload.id` | the newer payload replaces the older |
| `session` | `payload.id` | replace |
| `file` | `payload.id` | replace |
| `helper` | `payload.id` | replace |
| `lsp_event` | `payload.name` + `payload.type` | replace |
| `mcp_event` | `payload.name` + `payload.type` | replace |
| `limits` | `payload.kind` | replace |
| `memory_status` | one key for all | replace |
| `tree_changed` | `payload.session_id` | replace |
| `prompt_pending` | `payload.session_id` + `payload.kind` | replace |
| `shell_output` | `payload.request_id` | append `chunk` in order; keep the newest `seq` |

Every other type is a **barrier**. A barrier is never merged and never dropped. Unknown types are barriers. Today's barriers:

- `permission_request`, `permission_notification`
- `question_batch_request`, `question_batch_notification`
- `agent_event`, `run_complete`
- `config_changed`, `skills_event`, `update_available`
- `chat_link`
- `remote_action`, `remote_status`
- `secure_form`, `secure_form_closed`
- `cli_updates_installed`
- the app's own connection-state events

**How `push` works:**

- A barrier is appended. It ends the current segment, so later events can never merge into entries before it.
- A mergeable event whose key already has an entry in the current segment updates that entry where it stands:
  - The newer payload replaces the old one. For `shell_output`, the chunk is appended instead.
  - The kind becomes:
    - `created` when the first was `created`
    - the entry is removed when the first was `created` and the latest is `deleted`
    - `deleted` when the latest is `deleted`
    - otherwise `updated`
- Any other mergeable event is appended as a new entry.

**How `take` and the screens work:**

- `take()` hands over everything since the last call.
- The window calls it exactly once per screen frame (`requestAnimationFrame`), through one Tauri command. That gives one flush per frame.

**Guarantees:**

1. Barriers keep their exact order, relative to each other and to the merged groups between them.
2. Within a segment, entries keep the order in which each key first appeared.
3. Applying `take()` results to a keyed state gives the same final state as applying every raw event one by one. "Applying" means: created and updated insert or replace by key; deleted removes; shell chunks append.

**Overflow:**

- An overflow happens when more than `max_entries` entries wait, for example while the window is hidden and no frames run.
- On overflow, the batcher drops every mergeable entry and puts one `Resync` entry first. Barriers stay, in order.
- On `Resync`, the screens read everything again through the routes (section "What the desktop app calls").

### Rules

- **The engine sends every event.** The engine sends every change. Only the Rust side merges, so other clients (the terminal, tests) still see each event.
- **Nothing is dropped except on overflow,** and then `Resync` covers the loss.

### Tests (`cargo test` in `desktop/src-tauri`)

| Test | Proves | Why it can fail |
|---|---|---|
| `merges_message_updates_keeping_latest` | 50 updates of one message become one entry with the last payload | no merge |
| `created_then_updated_stays_created` | the kind rule | the new chat is never added |
| `created_then_deleted_vanishes` | nothing for a short-lived item | ghost row |
| `barrier_stops_merging_across` | update A, permission, update A → 3 entries in that order | a prompt overtaken |
| `keeps_first_appearance_order` | A, B, A → A, B | entries re-sorted by their latest update |
| `shell_output_appends_chunks_in_order` | the chunks joined equal the raw stream | lost or reordered output |
| `unknown_type_is_barrier` | a future type passes through unmerged | dropped |
| `overflow_becomes_resync_keeping_barriers` | `Resync` first, barriers kept | prompt lost on overflow |
| `same_final_state_as_unbatched` | 1,000 random events over 20 keys and 5 barrier kinds give equal final maps both ways | the core invariant |
| `parse_sse_line_accepts_only_data_lines` | `data:` prefix, blank lines, bad JSON | an `event:` or `:` comment line parsed as data, or bad JSON panics |
| `prompt_pending_merges_per_chat_and_kind` | `true` then `false` for one chat and kind becomes one entry with `false`; another kind stays separate | a mark left on, or two kinds merged into one |
| `hundred_thousand_updates_fifty_keys` | the batch holds 50 entries | per-event work leaks to frames |

### Risks

- **Slow screens.** Frames that are slow for other reasons (the speed bar) are phase 3 to 5 work. The batcher only promises at most one flush per frame and a bounded batch.

---

## 5.13 Switch-over (built in phase 7)

This item is specified here and **built in phase 7**. Phase 1 provides the pieces it uses:

- 5.2: ensure, and the terminal's switch `CRUSH_CLIENT_SERVER=1`, which already runs the terminal in shared-engine mode
- 5.10: the shared lock, built and unit-tested but not yet taken
- 5.14: recovery
- 5.3 to 5.11: the client methods and interfaces

### Current code facts

- **When the terminal uses a server today.** Only when `CRUSH_CLIENT_SERVER` is set (`cmd/workspace_setup.go:44-49`) or `--host` is given (`cmd/root.go:40`, `229-230`). Otherwise it runs in-process (`setupLocalApp`, `cmd/workspace_setup.go:91-163`). From phase 1, the switch reaches the project's engine (5.2).
- **The terminal UI calls agent globals directly:**
  - helpers and jobs (`ui/model/session.go:412`, `ui/model/tasks.go:165`, `ui/dialog/background.go:174`)
  - limits (`composer_footer.go:237`, `sidebar.go:107`, `sidebar.go:119`, `usage.go:48`, `usage.go:67`)
  - Ctrl+B capability (`workspace_cache.go:215`)
  - unfinished work (`session_host.go:64`)
  - remote, which refuses in client mode (`ui/model/remote.go:176-179`)
  - secure forms (`app/events.go:153`)

### Change (phase 7)

- **The setting.** `options.engine` in `crush.json`, with the values `"shared"` and `"alone"` ("work alone").
  - The default is `"alone"` until phase 7 and `"shared"` from phase 7.
  - `CRUSH_ENGINE=shared|alone` overrides it, for tests.
  - The new field in `internal/config/options.go` is `Engine string` with JSON name `engine`, `omitempty`, and a schema enum of shared and alone.
  - An explicit `--host` keeps the legacy server path.
  - `CRUSH_CLIENT_SERVER=1` keeps working as an override that means `shared`. Its meaning does not change in phase 7, because phase 1 already points it at the engine (5.2).
- **Startup in shared mode.** The engine path that phase 1 built for the switch (5.2): `EnsureEngine`, then `connectToServer`'s engine branch, and `SetEnsureEngine` (5.14). Phase 7 only makes the setting choose it.
- **Startup in alone mode.** Today's in-process path, plus the shared lock (5.10), taken from here on.
- **`crush run`** follows the setting. `crush session` and `crush stats` stay as they are.
- **UI call sites.** The sites listed above move to the workspace interfaces from phase 1 (`LiveWork`, `TreeManagement`, `FileRestoration`, and the remote and secure-entry client methods). Both modes then share one code path.
- **Session host.** Each hosted chat window follows the setting (inferred: the host's child windows run the normal setup).

### Rules

- **"Go back"** (`alone`) restores today's behavior from start to finish: the app runs in-process and never starts an engine. The only difference from today is the shared project lock that 5.10 requires.

### Tests (phase 7)

| Test | Proves | Why it can fail |
|---|---|---|
| Whole terminal suite twice, with `CRUSH_ENGINE=alone` and `CRUSH_ENGINE=shared` (two runs of `desktop/tests/go-test.sh`) | same behavior in both modes, including chat-list tabs and `crush run` | a UI call site still uses an agent global that is empty in shared mode |
| `TestAloneModeStartsNoEngine` | no engine process and no engine socket in alone mode | alone mode still runs ensure |
| `TestAloneModeTakesSharedLock` | an alone window holds the shared project lock, and an engine start is refused with `MsgLoneWindowHoldsProject` | the lone lock not wired to the setting |
| `TestSharedModeUsesEnsure` | the terminal joins the same workspace as the desktop app | the setting is read but the legacy path is taken |
| `TestClientServerEnvStillMeansShared` | `CRUSH_CLIENT_SERVER=1` with `options.engine: "alone"` still takes the engine path | the override is dropped in phase 7 |

### Risks

- **Two code paths during phases 1 to 6.** Contained: the phase-1 interfaces exist from the start, and phase 7 only moves call sites.

---

## 5.14 Engine crash recovery

### Current code facts

- **Reconnect today.** `runSubscription` (`workspace/client_events.go:64-134`):
  - On any error other than 404, it retries the same address with doubling waits from 250 ms up to 10 s (`sseReconnectInitialBackoff` and `sseReconnectMaxBackoff`, `workspace/client_workspace.go:68-69`; doubled at `client_events.go:107`, `132`).
  - On a 404, it runs `recoverWorkspace` (`client_events.go:151-180`). That creates the workspace again from the cached snapshot and starts the agent again.
  - `afterReconnect` re-sends the current chat and reports `ConnectionRecovered` (`client_events.go:205-215`).
  - Nothing ever starts a new engine.
- **Terminal notices** for a lost connection exist (CHECKLIST SIDE-030).
- **Helpers after a crash.** A crashed engine's saved helpers keep its PID and no `Interrupted` time. `claimInterrupted` claims them once that PID is dead (`tasks_resume.go:141-161`), within 24 h (`tasks_resume.go:31`). The next interactive agent start triggers this (`backend/agent.go:154-168`).
- **The reaper** that cleans up helper processes after a crash is started at `agent/tasks.go:170-174`. It is Linux-only: elsewhere `startReaper` does nothing (`agent/reaper_other.go:1-8`; plan Q11).

### Change (A; S adds the field)

```go
// workspace/client_workspace.go (field added by S)
ensureEngine func(ctx context.Context) error // set only for engine connections

// workspace/client_ensure.go (A)
func (w *ClientWorkspace) SetEnsureEngine(fn func(context.Context) error)

// client/unreachable.go (A)
// IsUnreachable reports a dead engine: a dial error with ECONNREFUSED or
// ENOENT, server.IsStaleSocketErr(err), or HTTP 503 with code shutting_down.
func IsUnreachable(err error) bool
```

`runSubscription` (`client_events.go:84-109`) changes:

- **When `SubscribeEvents` fails with `IsUnreachable`** and `ensureEngine` is set:
  1. Call it with a 15 s limit.
  2. On success, `continue` at once. The fresh engine answers 404, which leads to `recoverWorkspace`: a new workspace with an interactive agent start, so helpers resume.
  3. On failure, call `markDegraded(err, stuck)`. `stuck` becomes true after 3 failures in a row. The wait between tries grows as today.
- **A closed stream** is followed by the next subscribe. That subscribe fails as unreachable, so the same path runs.
- **The Go ensure function** for the terminal is `func(ctx) error { _, err := serverctl.EnsureEngine(ctx, opts); return err }`. `setupClientServerWorkspace` sets it on the engine path, from phase 1 (5.2).

### Rules

- **Both apps recover on their own.** Each runs ensure, and `start.lock` makes exactly one of them start the engine.
- **What a recovering app does:**
  1. It creates its workspace again, with a new ID and the same folder.
  2. It starts its agent in interactive mode.
  3. It re-sends its current chat.
  4. It reads its state again: the chat list, the open chat's messages, helpers, jobs, open secure forms and the share status.
  - The first app to rejoin decides the folder again (first-wins).
- **Helpers resume by today's rules.** The new engine claims a crashed engine's running helpers at the first interactive agent start, within 24 hours. They restart on their own child chats and show again through `helper` events. Jobs that were cut off are reported to the agent.
- **What is lost.** A phone share lived inside the dead engine, so it stops. The owning app shows Remote Control as off. A secure form that was open is gone, along with the turn that asked for it.
- **An engine that keeps crashing.** After 3 failed ensures in a row the app shows "stuck", with the engine log tail from the ensure error.

### Tests

| Test (package) | Proves | Why it can fail |
|---|---|---|
| `TestIsUnreachable` (client) | each error class | a 404 treated as dead, or the reverse |
| `TestClientRecoversWhenEngineKilled` (workspace + serverctl, real engine subprocess) | SIGKILL → the client ensures, gets a new workspace ID, sends `ConnectionRecovered` | retries the dead address forever |
| `TestTwoAppsRecoverAndHelperResumes` (integration in `serverctl`; two `ClientWorkspace`s on one chat; a fake helper CLI script that sleeps) | after SIGKILL both apps recover; there is one engine PID; the helper runs again with the same task ID; both apps get its `helper` event | the plan's 5.14 test |
| `TestEnsureFailureMarksStuckAfterThree` (workspace) | the stuck flag after 3 failed ensures | silent loop |

Race detector: `go test -race ./internal/workspace/ -run Recover`.

### Risks

- **A kill during a write.** SQLite in WAL mode survives it.
- **Mac has no reaper** (Q11). An orphaned helper process may keep running, and the resumed helper then starts a second copy. This is a known Mac limit.
- **Recovery storms.** The wait between tries is bounded, from 250 ms up to 10 s.

---

## Build pieces

There are four build pieces, A to D. Each is built in its own Git worktree branched from `desktop`, after the coordinator commits the scaffold S. The hot spots are `internal/rpc/routes.go`, `internal/server/endpoints.go`, `pubsub/events.go`, the shared proto files, the client's event decoders, the workspace event translator and the backend's client code. S edits each of them once, so that afterwards every piece adds routes, events, errors, hooks and state in **its own new files**.

### S. Scaffold (committed first by the coordinator; no behavior change)

| # | File | Exact content |
|---|---|---|
| S1 | `internal/proto/protocol.go` (new) | `const ProtocolVersion = 1` |
| S2 | `internal/proto/version.go`, `internal/backend/backend.go:228-236` | the field `VersionInfo.Protocol int` with JSON name `protocol`, filled with `proto.ProtocolVersion` |
| S3 | `internal/proto/proto.go` | the field `Workspace.Protocol int` with JSON name `protocol` (`omitempty`); `Error.Code` and `Error.Field` (section 0.1). The type `proto.Error` is in this file |
| S4 | `internal/pubsub/events.go` | the 12 `PayloadType` constants of section 0.1, `PayloadTypePromptPending` included |
| S5 | `internal/backend/client_streams.go` (new) | `EngineEvent{Type pubsub.PayloadType; Kind pubsub.EventType; Payload any}`, where `pubsub.EventType` is the created/updated/deleted type; `SubscribeClient`, `SendTo`, `SendAll` (below). Each stream is a `chan any` with capacity 256. A full stream drops the event and logs only its `Type`. |
| S6 | `internal/backend/workspace.go:52-99` | new `Workspace` fields: `streamMu sync.Mutex`, `streams map[*clientStream]struct{}`, `live liveState`, `tree treeState`, `share shareState` |
| S7 | `internal/server/events.go` | `wrapEvent` gets a new case `backend.EngineEvent` → `envelope(e.Type, pubsub.Event[any]{Type: e.Kind, Payload: e.Payload})`. Its default case calls `wrapPieceEvent(ev)` (new `server/events_pieces.go`), which tries `wrapLifecycleEvent`, `wrapLiveEvent`, `wrapTreeEvent` and `wrapShareEvent` in turn, then logs today's warning |
| S8 | `internal/server/handlers_workspace.go:121-181` | the SSE handler also runs `SubscribeClient(ctx, id, clientID)`, before `AttachClient`, and its write loop reads both channels. Engine events skip `deliverToClient` |
| S9 | `internal/server/errors.go` (new), `internal/server/rpc.go:110-152` | `type apiError struct{ Match func(error) bool; Status int; Code string; Field func(error) string }`; `pieceErrors() = slices.Concat(lifecycleErrors(), liveErrors(), treeErrors(), shareErrors())`; `errorInfo(err) (status int, code, field string)` checks `pieceErrors()` first, then today's switch with the codes from section 0.1. `handleError` writes `proto.Error{Message, Code, Field}`. Direct `jsonError` calls get codes by status: 400 `bad_request`, 404 `not_found`, 409 `conflict`, 500 `internal`, 503 `shutting_down` |
| S10 | `internal/server/endpoints.go:16-26` | `endpoints()` also includes `c.lifecycleEndpoints(), c.liveEndpoints(), c.treeEndpoints(), c.shareEndpoints()` |
| S11 | `internal/server/endpoints_{lifecycle,live,tree,share}.go` (new, one per piece) | each holds stubs: `func (c *controllerV1) <p>Endpoints() []apigen.Endpoint { return nil }`, `func wrap<P>Event(any) (*pubsub.Payload, bool) { return nil, false }`, `func <p>Errors() []apiError { return nil }` |
| S12 | `internal/backend/hooks.go` (new) | `GoneReason` (`GoneRetired`, `GoneExpired`, `GoneReleased`); `clientHooks{joined, viewChanged, messageSent, agentInit, gone, teardown}` with the signatures in 5.5; `allHooks()` = lifecycle, live, tree and share hooks; `runHooks(func(clientHooks))`, never called with `b.mu` held |
| S13 | `internal/backend/{lifecycle_hooks,live,tree,share}.go` (new, one per piece) | stubs: `func (b *Backend) <p>Hooks() clientHooks { return clientHooks{} }`. The last three also declare `type liveState struct{}`, `type treeState struct{}` and `type shareState struct{}`. `live.go` also declares `func (b *Backend) MoveToBackground(workspaceID, sessionID string) (bool, error) { return false, nil }`, which B fills; D calls it for the phone's `background` (5.7), so D does not wait for B |
| S14 | `internal/backend/clients.go`, `internal/backend/agent.go`, `internal/backend/shutdown.go` | hook calls, each after `b.mu` is released: `joined` in `registerClient`; `viewChanged` in `SetCurrentSession`; `gone` in `RetireClient`, `expireHold`, `releaseHoldLocked` and the delete branch of `detachStream` (`clients.go:249-252`; reason `GoneReleased` when `cs.released`, else `GoneExpired`); `messageSent` in `SendMessage` after the closing check (`backend/agent.go:51-58`) and before `go b.runAgent` (line 60); `agentInit` at the end of `InitAgent`; `teardown` in `teardown` before `Shutdown`. Signature changes: `SendMessage(workspaceID, clientID string, msg proto.AgentMessage)` and `InitAgent(ctx, workspaceID, clientID string, interactive bool)` |
| S15 | `internal/server/handlers_agent.go`, `internal/server/handlers_workspace.go`, `internal/server/endpoints.go` (docs for SendMessage, InitAgent and RunShellCommand) | `optionalClientID(r) (string, error)` returns "" when absent and 400 when malformed. `sendMessage` and `initAgent` pass it on. SendMessage and InitAgent document `.QueryParam("client_id", "Client ID (UUID); records which app started the work")`. RunShellCommand documents `.QueryParam("client_id", …)` and `.QueryParam("request_id", "Live output events for this request go to client_id")`; B adds the handler side |
| S16 | `internal/client/agent.go` (SendMessage and InitAgent methods) | send `client_id` |
| S17 | `internal/client/events_pieces.go` (new), `internal/client/workspaces.go:179-204` | `eventDecoders` = today's map plus `pieceEventDecoders()`, which merges `lifecycleDecoders`, `liveDecoders`, `treeDecoders` and `shareDecoders`. These are declared empty in new `client/{lifecycle,live,tree,share}_events.go` |
| S18 | `internal/workspace/client_events_pieces.go` (new), `internal/workspace/client_events.go:390-393` | the default case of `translateEvent` calls `w.translatePieceEvent(ev)`, which tries `translateLifecycleEvent`, `translateLiveEvent`, `translateTreeEvent` and `translateShareEvent` (stubs returning `nil, false` in new `workspace/client_{lifecycle,live,tree,share}_events.go`), then logs today's warning |
| S19 | `internal/workspace/client_workspace.go` | the field `ensureEngine func(context.Context) error` (unused until A) |
| S20 | `internal/agent/session_work.go` (new), `internal/agent/tree.go:38-46` | `func SessionBackgroundBlock(sessionID string) error` holds today's two checks, moved word for word with the same errors. `withIdleTree` calls it |
| S21 | `desktop/src-tauri/src/batch.rs` (new), `desktop/src-tauri/src/main.rs` | `batch.rs` holds only `//! Event batching (engine-design.md 5.12).` and `#![allow(dead_code)]`; `main.rs` gets the line `mod batch;` |
| S22 | `internal/proto/prototest/prototest.go` (new), plus the test edits listed below | `func CreateRequest(w proto.Workspace) proto.Workspace` returns `w` with `Protocol` set to `proto.ProtocolVersion`. It is a normal (non-test) file, so tests in every package can import it. S makes every existing test that builds a create request use it. Every new test in A to D that builds one uses it too |

The stream functions from S5:

```go
// SubscribeClient registers one SSE stream of clientID for engine-sent events.
// The channel closes when ctx ends.
func (b *Backend) SubscribeClient(ctx context.Context, workspaceID, clientID string) (<-chan any, error)
// SendTo queues ev on every live stream of clientID and returns how many took it.
func (b *Backend) SendTo(workspaceID, clientID string, ev EngineEvent) int
// SendAll queues ev on every live stream of the workspace.
func (b *Backend) SendAll(workspaceID string, ev EngineEvent) int
```

**S22's test edits.** These are the create requests in today's tests. S wraps each one in `prototest.CreateRequest(…)`. Two of them are helpers that cover their callers:

| File | Lines | What |
|---|---|---|
| `internal/backend/backend_test.go` | 642 | the `protoWS` helper, used by every `b.CreateWorkspace(protoWS(…))` in this file and in `config_test.go:55` |
| `internal/backend/backend_skills_test.go` | 52, 60 | `b.CreateWorkspace(proto.Workspace{…})` |
| `internal/backend/skill_management_test.go` | 27 | the same |
| `internal/server/e2e_test.go` | 146-162 | the `postWorkspace` helper wraps `args` before it encodes them. That covers `e2e_test.go:386`, `390`, `git_test.go:42` and `session_catalog_test.go:21-22` |
| `internal/server/multiclient_test.go` | 46, 64 | raw JSON bodies posted to the create handler |
| `internal/serverctl/restart_stale_test.go` | 212, 244 | `CreateWorkspace(…, proto.Workspace{…}, …)` against a fake server; wrapped too, so the rule has no exceptions |
| `internal/workspace/multiclient_integration_test.go` | 81, 85, 158, 205, 207, 230, 232, 256, 280, 321, 327 | `c.CreateWorkspace(ctx, proto.Workspace{…})` against a real server |

These are not create requests and stay unchanged: `NewClientWorkspace(c, proto.Workspace{ID: …})` (`workspace/client_workspace_test.go`, `workspace/queue_recall_test.go`), fake-server answers such as `Encode(proto.Workspace{…})` (`client_workspace_test.go:588`, `938`; `restart_stale_test.go:203`), and the slice type at `session_catalog_test.go:23`.

S checks the list again with two searches, keeping only create requests:

- `grep -rn 'proto.Workspace{' --include='*_test.go' internal`
- `grep -rn 'CreateWorkspace(\|postWorkspace(\|"/v1/workspaces"' --include='*_test.go' internal`

**S done-checks.** All of these must pass:

- `go build ./...` and `GOOS=darwin go build ./...`
- `go vet ./...`
- today's whole suite through `desktop/tests/go-test.sh`, unchanged except for S22's wrapping
- `cargo check` in `desktop/src-tauri`
- `git diff --name-only` shows only the files in this table and S22's test files
- no new route answers (`/v1/docs` lists the same routes as before)

### Piece A. Engine lifecycle and wire (5.1, 5.2, 5.10, 5.14; the ensure function and the terminal's switch of 5.13)

"Created by S" marks a file that does not exist at 6b2146f1: S creates it as a stub, and the piece then owns it.

- **Owns** (after S):
  - `proto`: `session.go`, `convert_session.go`; new `engine.go` (`EngineInfo`, `PromptPending`)
  - `backend`: `backend.go`, `create.go`, `clients.go`, `shutdown.go`, `session.go`, `lifecycle_hooks.go` (created by S); new `engine.go`, `pending_prompts.go` (not `prompts.go`: `prompts_test.go` already exists and stays frozen)
  - `server`: `server.go`, `net_other.go`, `handlers_session.go`, `handlers_workspace.go`, `endpoints_lifecycle.go` (created by S); new `socketdir.go`, `listener_peer.go`, `peercred_linux.go`, `peercred_darwin.go`, `peercred_other.go`, `handlers_lifecycle.go`
  - `rpc`: new `routes_lifecycle.go`
  - `serverctl`: all files, tests included; new `engine.go`
  - `cmd`: `server.go`, `workspace_setup.go` (the terminal's switch, 5.2); new `engine.go`
  - `config`: new `datadir.go`
  - `db`: `datadirlock.go`, `connect.go`; new `projectlock.go`
  - `lock`: `lock.go`, `lock_unix.go`, `lock_windows.go`
  - `client`: `client.go`, `workspaces.go`, `lifecycle_events.go` (created by S); new `unreachable.go`, `lifecycle.go`
  - `workspace`: `client_events.go`, `client_lifecycle_events.go` (created by S); new `client_ensure.go`
  - test files, by the test-file rule below
- **Must not touch:** any file owned by B, C or D, or the frozen files listed below.
- **Depends on:** S only.

### Piece B. Live work, ownership, smaller gaps (5.3, 5.4, 5.5, 5.9)

- **Owns:**
  - `agent`: `tasks.go` (hub teardown), `tasks_spawn.go` (`start` refuses a closing hub), `tasks_continue.go` (only the `start` call at line 156), `tasks_resume.go` (release, claim, the closing check), `background.go`, `background_jobs.go`, `background_jobs_resume.go` (job release and claim), `session_work.go` (created by S); new `ownership.go`, `prompt_owner.go` (`StopOwned`)
  - `agent/cliagent`: `limits.go`
  - `backend`: `agent.go` (the submission ID fill), `ops_agent.go`, `live.go` (created by S); new `live_*.go`
  - `server`: `handlers_agent.go`, `endpoints_live.go` (created by S); new `handlers_live.go`
  - `rpc`: new `routes_live.go`
  - `proto`: new `live.go`
  - `client`: `agent.go`, `live_events.go` (created by S); new `live.go`
  - `workspace`: `client_workspace.go`, `client_live_events.go` (created by S); new `live.go`, `app_live.go`, `client_live.go`
  - `config`: new `memory_limit_save.go`
  - test files, by the test-file rule below
  - B touches no other agent file. In particular `scheduler.go`, `agent.go` and `coordinator.go` stay as they are, and `turn.go` is C's.
- **Must not touch:** any file owned by A, C or D, or the frozen files.
- **Depends on:** S only. C uses B's `SessionBackgroundBlock` through the S seam, so neither waits for the other.

### Piece C. Tree actions and branch copy (5.6, 5.11)

- **Owns:**
  - `agent`: `tree.go`, `title.go` (the branch guard), `cli_steps.go`, `cli_turn.go`, `turn.go` (only the `cliStream` call at line 338), `turn_stream.go`, `file_review.go` (`lastTool`)
  - `agent/cliagent`: `cliagent.go`, `claude.go`, `claude_handlers.go`, `codex_handlers.go`, `acp.go`, `agy.go`; new `fork.go` (the interface only)
  - `db`: `tree.go`; new `branches.go`, `markers.go`, `migrations/20261018000000_chat_branches.sql`
  - `message`: `tree.go`; new `branch.go` (`BranchService`, `BranchOrigins`)
  - `session`: new `announce.go`
  - `backend`: `tree.go` (new: created by S as a stub; there is no `backend/tree.go` at 6b2146f1); new `branch.go`
  - `server`: `endpoints_tree.go` (created by S); new `handlers_tree.go`
  - `rpc`: new `routes_tree.go`
  - `proto`: new `tree.go`
  - `client`: `tree_events.go` (created by S); new `tree.go`
  - `workspace`: `client_tree_events.go` (created by S); new `client_tree.go`
  - test files, by the test-file rule below
- **Must not touch:** any file owned by A, B or D, the frozen files, or `db/querier.go` and `db/models.go` (no sqlc changes).
- **Depends on:** S only.

### Piece D. Phone sharing, secure entry, Rust batcher (5.7, 5.8, 5.12)

- **Owns:**
  - `remote`: `remote.go`, `hub.go` (`newHub` is at `hub.go:81`), `hub_state.go`; new `testing.go`
  - `secureentry`: `form.go`
  - `backend`: `share.go` (created by S; the action pump, engine-run actions, kept uploads); new `secure.go`
  - `server`: `endpoints_share.go` (created by S); new `handlers_share.go`, `handlers_secure.go` (the unix-only check)
  - `rpc`: new `routes_share.go`
  - `proto`: new `share.go`
  - `client`: `share_events.go` (created by S); new `share.go`
  - `workspace`: `client_share_events.go` (created by S); new `client_share.go`
  - `desktop/src-tauri/src/batch.rs` (created by S)
  - test files, by the test-file rule below
- **Must not touch:** any file owned by A, B or C, or the frozen files.
- **Depends on:** S only.

### Frozen after S (no piece edits them)

- `internal/rpc/routes.go`, `internal/rpc/rpc.go`
- `internal/server/endpoints.go`, `internal/server/events.go`, `internal/server/events_pieces.go`, `internal/server/errors.go`, `internal/server/rpc.go`
- `internal/pubsub/events.go`
- `internal/proto/proto.go`, `internal/proto/version.go`, `internal/proto/protocol.go`, `internal/proto/prototest/prototest.go`
- `internal/backend/hooks.go`, `internal/backend/client_streams.go`, `internal/backend/workspace.go`
- `internal/client/events_pieces.go`, `internal/workspace/client_events_pieces.go`, `internal/workspace/workspace.go`
- `internal/db/querier.go`, `internal/db/models.go`
- `desktop/src-tauri/src/main.rs`
- `go.mod`, `go.sum`: no piece needs a new module (`golang.org/x/sys/unix` is already there). If a piece does need one, it asks the coordinator.

### The test-file rule

Test files are owned the same way as other files, so done-check 6 can count them:

1. An existing `X_test.go` belongs to the piece that owns `X.go` in the same folder. For example, `backend/agent_test.go` is B's, because `backend/agent.go` is B's. In `serverctl`, A owns every file.
2. Every other existing test file is frozen after S. Examples: `backend/prompts_test.go`, `backend/backend_skills_test.go`, `server/e2e_test.go`, `server/multiclient_test.go` and `workspace/multiclient_integration_test.go`. If a piece's change breaks one, the piece stops and asks the coordinator.
3. A new test file belongs to the piece that creates it. Its name must start with the base name of a file that piece owns, for example `pending_prompts_test.go` (A), `prompt_owner_test.go` (B), `branch_test.go` (C) or `secure_test.go` (D). So two pieces can never create the same file.
4. A new test that builds a create request uses `prototest.CreateRequest` (S22).

### Done-checks for every piece

1. `go build ./...` and `GOOS=darwin go build ./...` pass.
2. `go vet ./...` passes.
3. The whole suite passes through `desktop/tests/go-test.sh`, as PLAN.md's progress record describes (run from the verify checkout, logs in `/var/tmp/crush-desktop-go-logs/`).
4. The race detector passes:
   - all pieces: `go test -race ./internal/server/... ./internal/backend/... ./internal/workspace/...`
   - A also: `./internal/serverctl/...` and `./internal/cmd/...`
   - B and C also: `./internal/agent/...`
5. Every test named in the piece's items exists and passes.
6. `git diff --name-only desktop...HEAD` lists only files the piece owns, with test files counted by the test-file rule above. A changed test file that the rule gives to no piece, or to another piece, fails the check. This is the check that catches a broken ownership rule.
7. D also: `cargo test` in `desktop/src-tauri` passes.

**Merge order:** S, then A, B, C and D in any order. The file sets do not overlap, so the merges are clean. After the last merge, the coordinator runs every done-check once more on `desktop`.

---

## What the desktop app (Rust side) calls

### 1. Commands

| When | Command | Use of the output |
|---|---|---|
| App start | `crush engine version` | It fails, or `protocol` is not the app's protocol (1): the "too old" message (PLAN section 3). The binary is `crush` on PATH, or the app setting, or `CRUSH_DESKTOP_CRUSH`. |
| Opening a project, and after a lost engine (5.14) | `crush engine ensure --cwd DIR [--data-dir D]` | Success: the `socket` to connect to. Error JSON plus exit 3/4/5/6/1: shown with its `message` (`project_locked` shows the 5.10 text). |
| Engines view, if any | `crush engine list` | `[]EngineInfo` |
| Phone setup step | `crush remote setup <action>` in a terminal window | 5.7 |
| Crush update steps | `crush self-update merge\|build\|install` | UPD-005, UPD-007 |

### 2. HTTP over the unix socket

The app speaks HTTP/1.1 with any `Host`. Every call that needs identity carries `?client_id=<the app's UUID>`. One UUID per window, for that window's life.

**Join and stay joined:**

1. `GET /v1/version`. Check that `protocol` equals 1.
2. `POST /v1/workspaces` with `{"path": DIR, "data_dir": "", "version": "<app version>", "protocol": 1, "client_id": "<uuid>", "yolo": <the app's setting, default true>, "env": [<login-shell environment>]}`. The answer is `proto.Workspace`.
   - If `path` differs from DIR, show the 5.2 folder notice.
   - If `yolo` differs from what the app sent, show the 5.2 YOLO notice.
   - On 503 `shutting_down`, run `crush engine ensure` again and create once more (5.2, step 7). A second 503 is shown as an error.
3. `GET /v1/workspaces/{id}/events?client_id=` and keep it open.
4. `POST /v1/workspaces/{id}/agent/init?client_id=` with today's `AgentInitRequest`, interactive: `{"interactive": true}` (`proto/requests.go:113-115`).
5. Whenever the shown chat changes: `POST /v1/workspaces/{id}/current-session?client_id=`.
6. To send: `POST /v1/workspaces/{id}/agent?client_id=` (202).
7. On close: `DELETE /v1/clients/{client_id}`.

**Today's routes** for chats, messages, queue, permissions, questions, config, skills and MCP (`rpc/routes.go`). When saving a chat, send back the whole record that was read (5.1).

**New routes:**

| Item | Routes |
|---|---|
| 5.2 | `GET /v1/engine` (with `closing`) |
| 5.3 | `GET …/sessions/{sid}/helpers`, `POST …/sessions/{sid}/helpers/{hid}/stop` |
| 5.4 | `GET …/sessions/{sid}/jobs` (every 1 s for the shown chat), `POST …/sessions/{sid}/jobs/kill`, `POST …/sessions/{sid}/background`, `GET …/agent/capabilities` |
| 5.6 | `GET …/sessions/{sid}/tree`, `GET …/tree/messages/{mid}`, `POST …/tree/label\|switch\|navigate\|copy`, `POST …/files/preview\|undo-preview\|undo` |
| 5.7 | `POST …/remote/start\|stop\|presence`, `GET …/remote`, `POST …/remote/actions/{aid}/done` |
| 5.8 | `GET …/secure-forms`, `POST …/secure-forms/{fid}/submit\|cancel` |
| 5.9 | `POST …/agent/sessions/{sid}/shell?client_id=&request_id=`, `POST …/env/is-set`, `GET /v1/memory`, `POST /v1/memory/claim-notice`, `PUT /v1/memory/limit`, `GET /v1/updates/clis`, `POST /v1/updates/clis/decline\|install`, `GET /v1/updates/crush`, `POST /v1/updates/crush/decline`, `GET /v1/limits`, `POST /v1/limits/{kind}/refresh` |
| 5.11 | `POST …/sessions/{sid}/branches`, `GET …/chat-links` |

Here `…` means `/v1/workspaces/{id}`.

### 3. Events

**Today's events:** `message`, `session`, `file`, `lsp_event`, `mcp_event`, `permission_request`, `permission_notification`, `question_batch_request`, `question_batch_notification`, `agent_event`, `run_complete`, `config_changed`, `skills_event`, `update_available`.

**New events:** `helper`, `shell_output`, `limits`, `memory_status`, `cli_updates_installed`, `tree_changed`, `chat_link`, `remote_action`, `remote_status`, `secure_form`, `secure_form_closed`, `prompt_pending`.

**Phone actions the window runs** (5.7): `remote_action` with kind `new_chat_send`, `yolo` or `mode`. The window answers each with `POST …/remote/actions/{aid}/done`, within 25 s. Every other phone action runs in the engine and never reaches the window.

Every event goes through the batcher (5.12). The screens call `take()` once per frame.

### 4. Losing the engine (5.14)

When the stream ends, or a call fails with refused, a missing socket or 503 `shutting_down`:

1. Run `crush engine ensure` again, waiting 250 ms up to 10 s between tries. After 3 failures in a row, show the error with Retry.
2. Reopen the event stream. A 404 there means a fresh engine.
3. Create the workspace again, then agent init, then `current-session`.
4. Read everything again: the chat list, the open chat's messages, helpers, jobs, `secure-forms`, `remote`, `limits` and `memory`.
5. Push one `Resync` to the screens.

---

## Checklist coverage (rows that need the engine)

| CHECKLIST rows | Item | Engine support |
|---|---|---|
| START-007 (startup tasks) | 5.1, 5.4, 5.9 | update checks, memory check, limits, Ctrl+B capability, unfinished-work flag |
| START-011 (client mode loses Remote Control, tree, secure entry) | 5.6, 5.7, 5.8 | all three work through the engine |
| SIDE-007, SIDE-014 (usage left) | 5.9 | `limits` route and event, "checking…" state |
| SIDE-015 (compaction limit from the chat's context budget) | 5.1 | `context_budget` on sessions |
| SIDE-030 (connection notices) | 5.14 | recovery steps and stuck state |
| LIST-012 (status marks) | 5.1, 5.3, 5.4 | `is_busy`, `has_unfinished_work` and `pending_prompt` on every chat read, and the all-apps `prompt_pending` event |
| LIST-018 | — | terminal session-host protocol, not engine work |
| LIVE-001, LIVE-002 (task row) | 5.3, 5.4 | helper and job lists |
| LIVE-003, LIVE-004 (Ctrl+B and its hint) | 5.4 | `background` route, `agent/capabilities` |
| LIVE-005 to LIVE-008, LIVE-021 (helper view) | 5.3 | helper list and events; the child chat through today's message routes |
| LIVE-009 to LIVE-011, LIVE-013 to LIVE-017 | 5.3 | today's behavior inside the engine; visible through `helper` events |
| LIVE-012 (interrupted work recovered) | 5.5, 5.14 | release (owner 0) at Q2 and at teardown, hub removal, and re-runnable resume |
| LIVE-018, LIVE-019 (memory warning) | 5.9 | memory route, event and claim |
| LIVE-020 (memory box) | 5.1 | `memlimit.Join` in the engine |
| DLG-089 to DLG-112 (tree and file restore) | 5.6 | tree and file routes |
| DLG-189 to DLG-201 (secure entry) | 5.8 | secure-form routes and events |
| DLG-223 to DLG-231 (background and helper dialogs) | 5.3, 5.4 | stop and kill routes |
| DLG-264 to DLG-266 (fallback compaction) | — | today's config routes (inferred enough) |
| DLG-267 to DLG-271 (memory limit) | 5.9 | `PUT /v1/memory/limit`, `MemoryStatus.limit_bytes` (see "Small additions that serve approved items") |
| DLG-272, DLG-273 (helper instructions file) | — | a desktop file, not engine work |
| DLG-281 to DLG-287 (usage) | 5.9 | `limits` and refresh |
| REM-001 to REM-019 | 5.7 | share routes and events; most phone actions run in the engine with the terminal's texts |
| UPD-001 | — | today's `update_available` event |
| UPD-002 to UPD-004, UPD-008 | 5.9 | update routes |
| UPD-005 to UPD-007 | — | the `crush self-update` commands |
| NOTE-002 (turn-finished alert) | — | today's `run_complete` and agent events, plus `is_busy` and the queue routes |
| MISC-011 to MISC-019 | — | CLI commands (PLAN section 7) |

---

## Open questions for the owner

Only questions whose default changes behavior you approved, or that you would notice, are here. Each shows the default that gets built until you answer.

1. **When do helpers stopped by Q2 resume?** The plan says helpers stopped this way "resume the next time the project opens". But while another app stays open, the engine never closes the project.
   - Built: they resume when the next app joins the project (its interactive agent start), or in the next engine.
   - Alternative: only after every app closes and the project opens again.
2. **What are "price fields"?**
   - Built: the four per-message Prism price columns. The copy's chat-level cost and token totals start at 0, so usage reports do not count the source twice.
   - Alternative: copy the totals too.
3. **A closing app's prompt inside another app's turn.** When an app closes, its own prompts stop (5.5). One case cannot follow both Q2 rules. Suppose the closing app's prompt was already handed to the AI tool inside a turn that another open app started. It can only be withdrawn by stopping that other app's turn (`agent/scheduler.go:579-580`; `cli_steps.go:99-111`).
   - Built: the other app's turn keeps running, so the AI tool still reads that one prompt.
   - Alternative: stop that turn too. The other app's queued prompts then run next.

The first draft's question about secure-entry events is gone. They now carry each field's label only, as the plan says (decision 12).

**A notice that needs your OK.** Decision 1 below moves part of an approved item from phase 1 to phase 7. The build follows it unless you say no.

## Decisions made

These were open questions in the first draft, or choices made since. Decision 1 moves part of approved item 5.10 from phase 1 to phase 7, so it needs your OK (the notice above). The others change no behavior you approved.

1. **The lone-window lock arrives in phase 7. Needs your OK.** Phase 1 builds and unit-tests the shared lock (`TryFileShared`, `LockProject`). `setupLocalApp` takes it only once the phase-7 "work alone" setting exists (5.10, 5.13).
   - What does not hold in phases 1 to 6: a terminal window and the desktop app can both run the AI on the same project at the same time, as two terminal windows can today. The plan's promise that they never both do (`PLAN.md:90`) starts in phase 7. Phase 1's done-line, "every engine gap in section 5 is closed" (`PLAN.md:63`), then counts 5.10 as closed with this lock built but not yet switched on.
   - Built: the lock is made and tested in phase 1, and switched on in phase 7 with the "work alone" setting.
   - Reason: PLAN 5.10 speaks of a window "set to work alone" (`PLAN.md:90`), and that setting arrives with the switch-over in phase 7 (`PLAN.md:93`). Until then, the brief's "one or both open at the same time" (`~/.cache/crush-gui-briefs/brief.md:28`) must keep working. The plan asks for your OK on any change to it (`PLAN.md:173`).
   - `TestEngineRefusedWhileLoneWindowOpen` drives the lock directly.
2. **The legacy `crush server` counts as an engine for the lock.** Its exclusive lock (`backend/create.go:186`) is today's lock, unchanged.
   - Reason: since 5.2, only an explicit `--host` reaches it, and the lone-window lock waits for phase 7. So nothing that runs side by side today is refused before phase 7.
3. **One phone share per app.** A second start from the same app returns the running share's status.
   - Reason: the plan presupposes it. The share "follows the chat of the app that started it" (`PLAN.md:83`), which names one app per share. Two terminal windows can each share today, too.
4. **A different `--channels` still refuses a join.** It now answers 409 `channel_mismatch` instead of 500.
   - Reason: this is today's rule (`backend/create.go:136-138`). The desktop app passes no channels, so it never meets it.
5. **Two user turns in a row in a branch.** A fake-provider test sends once in a branch that ends with the user's message (5.11).
   - Reason: the plan fixes that shape: "If it's your own message, the branch ends with it, and the AI sees it, unanswered, before your next message" (`PLAN.md:100`). If a real API provider refuses two user turns in a row, merging them is an agent change, and you hear of it before it is built.
6. **Most phone actions run in the engine** (5.7). The window runs only `new_chat_send`, `yolo` and `mode`, because those change window state. Uploads stay in the engine.
   - Reason: it keeps the plan's "follows the chat of the app that started it", and moves large uploads and most terminal rules out of the event stream and the app.
7. **A dropped `remote_action` or `secure_form` counts as not taken** (section 0.1). The action fails at once, and the form stays open and is listed again after a reconnect.
   - Reason: a full stream drops events by design (S5), and the phone must hear back before the hub's own 503 and 504 (`remote/remote.go:375-397`).
8. **Secure-form routes answer only on the unix-socket listener** (5.8, `local_only`).
   - Reason: secure entry "stays local" (`PLAN.md:84`), and the legacy server can listen on TCP (`server/server.go:48`, `131`).
9. **The branch copy's time bound** is under 500 ms for a 1,000-message chat, scaled by a baseline copy on a slow machine (5.11).
   - Reason: one database connection serves the copy and the running reply.
10. **Ensure waits for the old engine's project lock**, within its 15 s timeout, before it starts a new engine (5.2, step 5.3).
    - Reason: an exiting engine can have no socket and still hold the lock. Waiting on the socket alone could make the new engine fail its own lock wait.
11. **YOLO follows "the first app wins", with a notice** (5.2). The desktop sends `yolo` from its own setting, which defaults to true like the terminal's `--yolo` (`cmd/root.go:42`). When the create answer's `yolo` differs, the desktop shows a notice like the folder notice.
    - Reason: one workspace has one permission setting (`backend/create.go:174`), and the plan's rule for the folder is "the first one wins" (`PLAN.md:77`). The notice makes the rule visible, as the folder notice does.
12. **Secure-entry events carry each field's label only, never its file path** (5.8). The engine keeps the path and does every file check itself.
    - Reason: PLAN 5.8 says "only field names travel" (`PLAN.md:84`). The terminal's secure field never shows the path either: its only file texts are "Written to the file. The agent gets only this status." and "This file was already saved. Request this entry again to add it." (`ui/dialog/questionform/secure.go:310`, `313`). No checklist row needs the path: DLG-189 to DLG-201 name the label, the masked field, its keys and status texts, and file checks that run in the backend (DLG-201).

### Changes made during the build (coordinator, 2026-10-18)

13. **A fork through the engine carries the restore's undo ID** (5.6, piece C review). `proto.TreeCopyRequest` gains an optional `undo_id` (JSON `undo_id`, omitted when empty; an added optional field needs no protocol change, see 0.1). `ClientWorkspace.CopyTree` sends it from the restore result, and `Backend.CopyTree` moves that undo record to the new chat after a successful fork or clone, as `message.CopyTree` does in-process (`internal/message/tree.go:144-148`). The backend moves it only when the ID is the one its own `UndoFiles` last returned for the source chat (kept in memory, used once, cleared at teardown); any other ID is ignored with a warning, and after an engine restart the record stays with the source chat.
    - Reason: without it, "Undo Last File Restore" finds nothing in the new chat, although the restore notice tells the user to use it there.
14. **Over the engine, restore-then-move is two guarded calls** (5.6). The app applies the restore plan (`UndoFiles`), then makes the move. If another app moves the chat, or it turns busy, between the two calls, the move is refused and the files stay restored; the user can undo the restore.
    - Reason: in-process both run under one chat lock; doing the same over the engine would need a new combined route. The gap is two back-to-back requests, so it is rare and recoverable.
15. **An app keeps the tree revision its own action returns** (5.6). Tree action results carry the new revision; `ClientWorkspace` stores it, so two actions in a row do not need a tree read in between. Another app's change in between still gives 409 `revision_conflict`.
16. **An old file-restore preview answers 409 `plan_expired`** (5.6). A preview is made for the chat's head at that moment; a new message moves the head without changing the tree revision, and applying the old plan then answers 409 `plan_expired` (make a new preview), not a server error. The check runs inside the chat's guard together with the apply. A plan made before a branch change still answers 409 `revision_conflict`.
17. **Tree results carry the revision read under the chat's lock** (5.6): the revision a move, label or copy returns is the one its own change produced, never one that already includes another app's later change.

## Small additions that serve approved items

The plan does not name these, but an approved item or checklist row needs each one.

| Addition | Serves |
|---|---|
| `GET /v1/engine` (5.2), with `data_dir` and `closing` | PLAN 5.2's ensure and `crush engine list` (`PLAN.md:76`). Ensure needs `data_dir` to refuse a foreign engine, and `closing` to replace an engine on its way out |
| `crush engine version` (5.2) | PLAN section 3's "too old" message (`PLAN.md:51`), and 5.1's protocol check (`PLAN.md:75`): the desktop app learns the version and protocol before it starts an engine |
| Memory-limit routes: `PUT /v1/memory/limit` and `MemoryStatus.limit_bytes` (5.9) | the memory-limit dialog rows DLG-267 to DLG-271, next to PLAN 5.9's memory-warning routes (`PLAN.md:88`) |
| The `DefaultHost` move into the private folder (5.1 d) | PLAN 5.1: "No world-readable `/tmp` fallback" (`PLAN.md:75`) |
| `pending_prompt` on chat reads and the `prompt_pending` event (5.1 e) | CHECKLIST LIST-012, the status marks on every chat (`CHECKLIST.md:60`) |
| `serverctl.FindEngine` (5.2), used by `crush logout` | PLAN 5.2 and 5.10 together: the engine holds the project lock (`PLAN.md:90`), and `crush logout` must keep working while the desktop has the project open, as it does today |

---

## Where the code contradicts the plan

These are plan statements the current code makes impossible or wrong as written. Each one has the fix this design builds.

1. **5.1, "a mismatch is refused":**
   - Today a busy mismatched server is reused (`serverctl/stale.go:53-58`).
   - Refusing on any build difference would refuse after every rebuild, because the build ID changes each time (`stale.go:21-25`).
   - Fix: refuse on a protocol mismatch. A different build is restarted when idle and reused when busy.
2. **5.5, "helpers … resume the next time the project opens, as today":**
   - Today resume runs once per process, and it skips helpers owned by a live process (`agent/tasks_resume.go:50-57`, `141-161`). In a long-lived engine, those helpers would never resume.
   - A full stop does not help either. `stopAll` marks running helpers interrupted but leaves this PID as their owner (`agent/tasks.go:307-324`, called from `coordinator.go:272-277`), and so does `interruptJob` for jobs (`background_jobs_resume.go:82-101`). The hub also stays in the process-wide `hubs` list for good (`tasks.go:177-179`). So a workspace torn down and created again in the same engine never resumes them.
   - A teardown also runs outside `b.mu` (`backend/shutdown.go:31-43`), so the next app's resume can reach the dying hub through its copy of `hubs` (`tasks_resume.go:50-57`), and nothing on the resume path checks `closing` (`tasks_resume.go:163-226`).
   - Fix: take the hub out of `hubs` first, then release its helpers and jobs (owner 0), at Q2 and at teardown; make resume re-runnable; and let a closing hub never claim or start work.
3. **5.4, job lists "scoped to the chat":** managed jobs carry no chat (`agent/background_jobs.go:307`). Fixed.
4. **5.7, "/remote works from both apps":**
   - Phone actions are run by terminal UI code (`ui/model/remote.go:359-443`).
   - The Source needs an in-process workspace (`remote/source.go:53-57`).
   - The launch ID is process-wide (`remote/launcher.go:47-56`; `hub_state.go:234`).
   - Fix: the engine hosts the share and runs most actions itself, with the terminal's texts. It forwards only the actions that change window state (`new_chat_send`, `yolo`, `mode`) to the app that started the share. Each share has its own launch ID.
5. **5.8, "values never appear in events or logs":**
   - The engine writes the file, so the value must reach it.
   - The normal handler wrapper logs decode errors (`server/rpc.go:73`), which can quote a character of the body.
   - Fix: the submit body over the owner-only socket is the only path, through a hand-written handler that logs nothing.
6. **5.10, "a terminal window set to work alone holds a shared lock":**
   - The lock package has no shared lock (`lock/lock.go`, `lock/lock_unix.go`).
   - The in-process terminal takes no lock at all today (`cmd/workspace_setup.go:128`).
   - Fix: the shared lock is added and unit-tested in phase 1. Lone windows take it from phase 7, when the "work alone" setting exists (see "Decisions made", item 1).
7. **5.11, "today's fork and clone get those too":** `CopyTree` also misses the `prism_*` price columns (`db/tree.go:228-229`). Fix: added with the other extras.
8. **5.2, "apps join by project":** today's join key is the folder (`backend/create.go:38`, `249-258`). Fix: the engine keys by data folder.
9. **5.1, "no world-readable /tmp fallback":**
   - `DefaultHost` falls back to bare `/tmp` (`server/server.go:82-84`).
   - `socketDir` trusts `os.TempDir()` without a check (`server/server.go:30-35`).
   - Fix: replaced by a checked private folder.
10. **5.9, bars show "checking…" until the first reading:** the limits store marks failures with the same timestamp as readings (`cliagent/limits.go:119-127`), so it cannot tell "no reading yet". Fix: `readAt` added.
11. **5.6, tree routes "with revision checks":** the tree guard's busy errors are plain text and would reach the app as 500 (`agent/tree.go:29-45`). Fix: they become 409 `tree_busy`.
12. **Phase 3 and 5.2, the terminal joins the shared engine "using today's switch":**
    - Today's switch, `CRUSH_CLIENT_SERVER=1`, dials one shared server at `--host`, which defaults to `server.DefaultHost()` (`cmd/workspace_setup.go:72-77`, `210-218`; `cmd/root.go:40`).
    - That server is not the project's engine, so the terminal and the desktop app would not share a workspace (`PLAN.md:65`, `76`).
    - Fix: with the switch on and no `--host`, the terminal runs `EnsureEngine` and joins the project's engine. An explicit `--host` keeps the legacy path.
13. **5.5, "everything else it started stops, like today":**
    - The running prompt has no recorded owner. The active request holds only a cancel function (`agent/agent.go:169-176`).
    - Cancel ends the running turn and clears the whole queue, whichever app sent each prompt (`agent/scheduler.go:498-540`).
    - So today's stop would also end work that another open app on the same chat started.
    - Fix: the backend records the owning app per prompt. The new `StopOwned` drops only the leaving app's prompts, and interrupts the running turn (never cancels it) only when its prompts are all the leaver's. One case remains (open question 3).

---

## Not in scope (optional ideas, not built)

- A `job` event. The desktop polls once a second, like the terminal.
- Restarting a phone share automatically after an engine crash.
- A trimmed tree read for very long chats.
- More than one project per engine.
- An engine for Windows.
- Moving the terminal UI off the agent globals before phase 7.
- Checking, from the client, which user owns the engine. The 0700 folder makes it unnecessary.
- The per-tool forkers. They are phase 4, after the live re-check.
- Removing the legacy `crush server` path.
- A `secure_form` value for `pending_prompt`. LIST-012's `!` mark covers permission and question prompts only (`CHECKLIST.md:60`); open secure forms are read through `secure-forms` (5.8).
