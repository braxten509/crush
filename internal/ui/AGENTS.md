# UI Development Instructions

## General Guidelines

- Never use commands to send messages when you can directly mutate children
  or state.
- Keep things simple; do not overcomplicate.
- Create files if needed to separate logic; do not nest models.
- Never do IO or expensive work in `Update`; always use a `tea.Cmd`. That
  includes every `Workspace` call that can block (config writes, preference
  and API key saves, permission answers, agent rebuilds — each is an HTTP
  round-trip in client/server mode) and local file reads and writes (theme
  files, attachments).
- Never change the model state inside of a command. Use messages and update
  the state in the main `Update` loop. See "Commands" below.
- Use the `github.com/charmbracelet/x/ansi` package for any string
  manipulation that might involve ANSI codes. Do not manipulate ANSI strings
  at byte level! Some useful functions:
  - `ansi.Cut`
  - `ansi.StringWidth`
  - `ansi.Strip`
  - `ansi.Truncate`

## Architecture

### Rendering Pipeline

The UI uses a **hybrid rendering** approach:

1. **Screen-based (Ultraviolet)**: The top-level `UI` model draws into a
   `uv.Screen` (`UI.Draw` in `model/draw.go`), and components draw into
   sub-regions using `uv.NewStyledString(str).Draw(scr, rect)`. Layout is
   rectangle-based via the `uiLayout` struct in `model/layout.go`
   (`layout.header`, `layout.main`, `layout.pills`, `layout.editor`,
   `layout.usage`, `layout.tasks`, `layout.sidebar`, `layout.status`,
   `layout.sessionDetails`).
2. **String-based**: Sub-components like `list.List` and `completions` render
   to strings, which are painted onto the screen buffer.
3. **`View()`** renders the frame, reusing the last view when nothing that
   affects it changed (`frameState` / `frameCache` in `model/framecache.go`).

### Main Model (`model/ui.go`)

The `UI` struct is the only Bubble Tea model. Key fields:

- `width`, `height` — terminal dimensions
- `layout uiLayout` — computed layout rectangles
- `state uiState` — `uiOnboarding | uiInitialize | uiLanding | uiChat`
- `focus uiFocusState` — `uiFocusNone | uiFocusEditor | uiFocusMain |
  uiFocusSidebar`
- `chat *Chat` — wraps `list.List` for the message view
- `composer` (embedded) — the prompt editor (`textarea`, bang mode, history,
  placeholders)
- `activeInline dialog.InlineEditor` — an inline editor (question form, plan
  handoff, ...) that replaces the textarea
- `dialog *dialog.Overlay` — stacked dialog system
- `attachments`, `mention` — sub-components

Most methods on `UI` are split into files per concern (`session.go`,
`sidebar.go`, `tasks.go`, `remote.go`, `lsp.go`, `mcp.go`, ...). State that
belongs together lives in small component structs held by `UI`:

| Component            | File               | Holds                                              |
| -------------------- | ------------------ | -------------------------------------------------- |
| `composer`           | `composer.go`      | textarea, bang mode, prompt history, placeholders  |
| `promptQueue`        | `prompt_queue.go`  | queued, pending and recalled prompts               |
| `frameState`         | `framecache.go`    | per-update frame memoization and the last view     |
| `remoteShare`        | `remote.go`        | the Remote Control server and its setup            |
| `themeController`    | `theme.go`         | applied theme key, user choice, preview backup     |
| `asyncProbe[T]`      | `async_probe.go`   | memoized LSP / MCP state fetched off `Update`      |
| `sidebarPane`        | `sidebar.go`       | sidebar logo, virtual scroll and scrollbar         |
| `taskRow`            | `tasks.go`         | background sub-agent / process row and selection   |
| `mentionCompletions` | `mention.go`       | the `@` mention popup                              |

Others with their own type: `Chat` (`chat.go` and `chat_*.go`), `Status`
(`status.go`), `header` (`header.go`) and `subagentView`
(`subagent_view.go`).

### Update Flow (`model/update*.go`)

`UI.Update` calls `update` (`model/update.go`), which:

1. Gives the message to the secure entry (`handleSecureEntryMsg`) and the
   sub-agent view (`handleSubagentView`) first.
2. Calls `routeMsg`, which sends input to its owner (keys to
   `handleKeyPressMsg`, pastes to `routePaste`, mouse to `handleMouse*`),
   then tries the per-concern handlers in order: `handleTerminalMsg`,
   `handleWorkspaceEvent`, `handleRemoteMsg`, `handleAgentMsg`,
   `handleSessionMsg`, `handleUIMsg`, `handleTickMsg`. Each returns
   `ok == false` for messages it does not handle. Anything left goes to the
   open dialogs, if any.
3. Runs `finishUpdate` (placeholder, stale-state refresh backstop,
   attachments, animation clock, frame bookkeeping) unless the handler ended
   the update early (`flowReturn` / `flowReplace`).

The handlers live in `update_ui.go` (terminal, Remote Control, UI and tick
messages), `update_workspace.go` (workspace pub/sub events),
`update_agent.go` (agent, model, shell and probe results) and
`update_session.go` (session, history and command loading). Dialog actions
are handled in `dialog_actions.go`: `handleDialogAction` tries the
per-concern action handlers in turn (theme, palette toggles, palette,
sessions, tree, remote, models, prompts) and falls back to
`handleGenericDialogAction`.

### Input Ownership

Key presses go to exactly one owner, computed by `inputOwner()` /
`paneOwner()` in `model/keypress.go`, in this precedence:

1. secure entry (a secret is held while something else has the keys)
2. sub-agent view (shown over the chat, no dialog open)
3. dialog (any dialog open)
4. inline editor (has the keys)
5. task row (selected)
6. onboarding (only quit works) / project initialization prompt
7. by focus: completions popup or editor, chat, sidebar
8. global keys

The quit key works everywhere but over the quit dialog, Tab toggles an
inline editor's focus, and while the agent runs the cancel and interrupt
keys beat the editor and the chat. Mouse and paste have their own routes in
`model/mouse.go`: clicks go to dialogs first, then an inline editor, the
composer and the chat; a paste goes to a focused inline editor first (even
over a dialog), then to the dialogs and the editor.

### Focus

Every focus change goes through `setFocus` (`model/focus.go`). It records
`m.focus` and keeps the textarea and chat focus in step; the
`keepChatFocus`, `keepTextareaFocus` and `focusRecordOnly` options leave one
or both alone. `openInline` replaces the textarea with an inline editor and
focuses it.

### Commands

A `tea.Cmd` runs on another goroutine, so its closure must not read or
write the model (`m`, `m.com`, dialog fields): capture what it needs into
locals first and return a message, then apply the result in `Update`.

- `doThen(work, then)` (`model/config_write.go`) runs `work` off the Update
  goroutine and delivers a `workDoneMsg`; `then` runs on the Update
  goroutine with the error, so it may change UI state. `writeConfigField`
  and `savePreferredModel` build on it.
- Keep optimistic UI changes (closing a dialog, switching layout) where the
  user expects them at once, and report a failed write when it lands.
- Results that may arrive out of order carry a sequence or generation
  number (theme previews, busy probes) and are dropped when stale.
- Workspace state shown every frame (busy, YOLO, agent model, LSP, MCP,
  prompt queue) is memoized and refreshed off `Update`
  (`model/workspace_cache.go`, `asyncProbe`); read the memoized values in
  `Update` and `Draw`, never the workspace.
- Tests in `model/cmd_state_test.go` and `model/offloop_test.go` run
  commands on another goroutine under `-race` to pin this.

### Chat View (`model/chat.go`)

The `Chat` struct wraps a `list.List` with an ID-to-index map, mouse
tracking (drag, double/triple click), animation management, and a `follow`
flag for auto-scroll. It bridges screen-based and string-based rendering:
`Chat.Draw` renders the list to a string and paints it, with a scrollbar
when needed.

Individual chat items in `chat/` should be simple renderers that cache their
output and invalidate when data changes (see `cachedMessageItem` in
`chat/messages.go`).

### Centralized Message Handling

Sub-components (`Chat`, `List`, `Attachments`, `Completions`, etc.) do not
participate in the standard Elm architecture message loop. They are
stateful structs with imperative methods that the main model calls
directly:

- **`Chat`** and **`List`** have no `Update` method at all. The main model
  calls targeted methods like `HandleMouseDown()`, `ScrollBy()`,
  `SetMessages()`, `EnsureAnimating()`.
- **`Attachments`** and **`Completions`** have non-standard `Update`
  signatures (e.g., returning `bool` for "consumed") that act as guards, not
  as full Bubble Tea models.
- **Sidebar** is drawn by `drawSidebar()` on `UI`; its scroll state is the
  `sidebarPane` component.

When writing new components, follow this pattern:

- Expose imperative methods for state changes (not `Update(tea.Msg)`).
- Return `tea.Cmd` from methods when side effects are needed.
- Handle rendering via `Render(width int) string` or
  `Draw(scr uv.Screen, area uv.Rectangle)`.
- Let the main `UI.Update()` decide when and how to call into the component.

## Key Patterns

### Composition Over Inheritance

Use struct embedding for shared behaviors. See `chat/messages.go` for
examples of reusable embedded structs for highlighting, caching, and focus.

### Interface Hierarchy

The chat message system uses layered interface composition:

- **`list.Item`** — base: `Render(width int) string`
- **`MessageItem`** — extends `list.Item` + `list.RawRenderable` +
  `Identifiable`
- **`ToolMessageItem`** — extends `MessageItem` with tool call/result/status
  methods
- **Opt-in capabilities**: `Focusable`, `Highlightable`, `Expandable`,
  `Animatable`, `Compactable`, `KeyEventHandler`

Key interface locations:

- List item interfaces: `list/item.go`
- Chat message interfaces: `chat/messages.go`
- Tool message interfaces: `chat/tools.go`
- Dialog interface: `dialog/dialog.go`

### Tool Renderers

Each tool has a dedicated renderer in `chat/`. The `ToolRenderer` interface
requires:

```go
RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string
```

`NewToolMessageItem` in `chat/tool_registry.go` is the central factory. It
looks the tool name up in `toolSpecs`, one entry per built-in tool holding
its item constructor and how it is copied to the clipboard (heading name,
parameter and result formatting); MCP tools and unknown tools fall back to
the MCP and generic items. Tools with the usual layout (spinner, header of
parameters, early states, body) describe it with a `standardTool` in
`chat/tool_standard.go`. Renderers live in:

| File                     | Tools rendered                                 |
| ------------------------ | ---------------------------------------------- |
| `chat/bash.go`           | Bash, JobOutput, JobKill                       |
| `chat/file.go`           | View, Write, Edit, MultiEdit, Download         |
| `chat/search.go`         | Glob, Grep, LS, Sourcegraph                    |
| `chat/fetch.go`          | Fetch, WebFetch, WebSearch                     |
| `chat/agent.go`          | Agent, AgenticFetch                            |
| `chat/diagnostics.go`    | Diagnostics                                    |
| `chat/references.go`     | References                                     |
| `chat/definition.go`     | Definition                                     |
| `chat/rename.go`         | Rename                                         |
| `chat/replace_symbol.go` | ReplaceSymbol                                  |
| `chat/call_hierarchy.go` | CallHierarchy                                  |
| `chat/symbols.go`        | Symbols                                        |
| `chat/lsp_restart.go`    | LSPRestart                                     |
| `chat/todos.go`          | Todos                                          |
| `chat/question.go`       | Question                                       |
| `chat/mcp.go`            | MCP tools (`mcp_` prefix)                      |
| `chat/docker_mcp.go`     | Docker MCP tools                               |
| `chat/generic.go`        | Fallback for unrecognized tools                |
| `chat/toolgroup.go`      | Groups of consecutive tool calls               |
| `chat/assistant.go`      | Assistant messages (thinking, content, errors) |
| `chat/user.go`           | User messages (input + attachments)            |
| `chat/shell.go`          | Bang-mode shell command results                |

### Styling

- All styles are defined in the `Styles` struct in `styles/styles.go`. Its
  groups (`HeaderStyles`, `PillsStyles`, `DialogStyles`, `ToolStyles`, etc.)
  are declared by area in `styles/styles_chat.go`, `styles_chrome.go`,
  `styles_dialog.go` and `styles_editor.go`.
- Access styles via `*common.Common` passed to components.
- Use semantic color fields rather than hardcoded colors.

### Dialogs

- Implement the `Dialog` interface in `dialog/dialog.go`:
  `ID()`, `HandleMsg()` returning an `Action`, `Draw()` onto `uv.Screen`.
- `Overlay` manages a stack of dialogs with push/pop/contains operations.
- Dialogs draw last and overlay everything else.
- Use `RenderContext` from `dialog/common.go` for consistent layout (title
  gradients, width, gap, cursor offset helpers).
- A dialog does its I/O in commands too. A command's result that only the
  dialog needs is a private message type it handles in `HandleMsg` (the
  overlay forwards unclaimed messages to the front dialog); see
  `apiKeySavedMsg` in `dialog/auth/api_key_input.go`.

#### Dialog rendering rules

These prevent the wrapping/overflow bugs that recur whenever a new dialog
is copy-pasted from an old one. In lipgloss v2 `Width(n)` is the **total**
box width — border and padding live *inside* it.

- Size content to the dialog's **content area**, not the outer width:
  `innerWidth := m.width - t.Dialog.View.GetHorizontalFrameSize()`. Sizing a
  block to the full `m.width` makes it 1–2 cols too wide, so the dialog
  frame re-wraps it (the classic "last few chars wrap" bug).
- Inset text with **`Padding`, never `Margin`**. Margin sits outside the
  width and pushes the block past the frame; padding is inside the width
  and applies to every wrapped line.
- Render styled text segments **individually** and concatenate the results
  (`styleA.Render(x) + styleB.Render(y)`), rather than concatenating raw
  strings and wrapping the whole thing in one style. An inner segment's
  reset code drops the outer color for everything after it.
- Use the shared helpers in `dialog/internal/kit` instead of re-deriving
  widths:
  - keybind hints → `kit.RenderDialogHelp(t, &m.help, m, innerWidth)` (sizes,
    pads, truncates — never `helpStyle.Render(m.help.View(m))` raw);
  - text inputs → `kit.DialogInputTextWidth(t, input, innerWidth)`
    (accounts for the `"> "` prompt);
  - titles → `common.DialogTitle` (truncates instead of wrapping);
  - list + scrollbar → `kit.JoinScrollbar` (size the list with
    `kit.SizeDialogList`);
  - hiding a crowded info column → `kit.ApplyInfoColumnVisibility`.
- Clamp width/height to the drawable `area` (`max(0, min(maxW, area.Dx()-frame))`)
  so dialogs stay inside small terminals.

### Shared Context

The `common.Common` struct holds the `workspace.Workspace` and
`*styles.Styles`. Thread it through all components that need access to app
state or styles.

## File Organization

- `model/` — Main UI model, its components and per-concern files (see
  above)
- `chat/` — Chat message item types and tool renderers
- `dialog/` — The `Dialog` interface, `Overlay`, actions, dialog IDs
  (`ids.go`), the shared `CommandItem` list item, the command palette and
  the smaller dialogs (file picker, reasoning, quit, tree, ...). Larger
  dialogs live in subpackages that import it:
  - `dialog/auth` — auth method, API key, OAuth flows, AWS SSO
  - `dialog/models`, `dialog/sessions`, `dialog/theme`,
    `dialog/permissions`, `dialog/mcpservers`, `dialog/remotecontrol`,
    `dialog/skillbrowser`
  - `dialog/questionform` — inline question forms and plan handoff
  - `dialog/internal/kit` — shared layout, help and list-item helpers;
    `dialog/internal/kittest` — shared test helpers
- `list/` — Generic lazy-rendered scrollable list with viewport tracking
- `common/` — Shared `Common` struct, layout helpers, markdown rendering,
  diff rendering, scrollbar
- `completions/` — Autocomplete popup with filterable list
- `attachments/` — File attachment management
- `styles/` — All style definitions, color tokens, icons
- `diffview/` — Unified and split diff rendering with syntax highlighting
- `anim/` — Animated spinnner
- `image/` — Terminal image rendering (Kitty graphics)
- `logo/` — Logo rendering
- `util/` — Small shared utilities and message types

## Common Gotchas

- Always account for padding/borders in width calculations.
- Use `tea.Batch()` when returning multiple commands.
- Pass `*common.Common` to components that need styles or app access.
- When writing tea.Cmd's prefer creating methods in the model instead of writing inline functions.
- The `list.List` only renders visible items (lazy). No render cache exists
  at the list level — items should cache internally if rendering is
  expensive.
- Rendering is the chat's hot path; a few invariants keep resize/scroll fast
  on large conversations:
  - Syntax highlighting and diff formatting build the chroma style from the
    theme, which is expensive — it is memoized in `common.ChromaStyle`, and
    lexer lookups in `xchroma.MatchLexer`. Don't call
    `chroma.MustNewStyle` / `lexers.Match` directly on a render path.
  - `list.TotalHeight` renders **every** item; it's only for exact scrollbar
    geometry. For "does it overflow?" use the bounded `list.Overflows`. Never
    call `TotalHeight` per frame during a resize — the chat suppresses the
    scrollbar mid-drag and warms the cache incrementally (`list.Prewarm`)
    on settle instead.
- Input routing follows `inputOwner()`, not ad hoc checks: add a new
  owner there rather than testing `HasDialogs()` or `activeInline` in a
  key handler.
