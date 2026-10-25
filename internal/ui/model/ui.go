package model

import (
	"context"
	"os"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/crush/internal/ui/dialog/questionform"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/commands"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/remote"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/charmbracelet/crush/internal/ui/attachments"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/completions"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/notification"
	"github.com/charmbracelet/crush/internal/ui/sessionhost"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/workspace"
)

// uiFocusState represents the current focus state of the UI.
type uiFocusState uint8

// Possible uiFocusState values.
const (
	uiFocusNone uiFocusState = iota
	uiFocusEditor
	uiFocusMain
	uiFocusSidebar
)

type uiState uint8

// Possible uiState values.
const (
	uiOnboarding uiState = iota
	uiInitialize
	uiLanding
	uiChat
)

type uiInputMode uint8

const (
	uiInputModeCode uiInputMode = iota
	uiInputModePlan
)

type openEditorMsg struct {
	Text string
}

type shellResultMsg struct {
	PendingID string // ID of the pending ShellItem to update.
	Command   string
	Output    string
	ExitCode  int
}

// shellStreamMsg carries incremental output from a streaming shell command.
type shellStreamMsg struct {
	PendingID string
	Chunk     string
	streamCh  <-chan string // unexported; used to continue draining
}

type (
	// userCommandsLoadedMsg is sent when user commands are loaded.
	userCommandsLoadedMsg struct {
		Commands []commands.CustomCommand
		// Skills are the visible skills, for $name mentions.
		Skills []skills.CatalogEntry
	}
	// mcpPromptsLoadedMsg is sent when mcp prompts are loaded.
	mcpPromptsLoadedMsg struct {
		Prompts []commands.MCPPrompt
	}
	// mcpStateChangedMsg is sent when there is a change in MCP client states.
	mcpStateChangedMsg struct {
		states map[string]mcp.ClientInfo
	}
	// sendMessageMsg is sent to send a message.
	// currently only used for mcp prompts.
	sendMessageMsg struct {
		Content     string
		Attachments []message.Attachment
	}

	// closeDialogMsg is sent to close the current dialog.
	closeDialogMsg struct{}

	// filePickerDoneMsg closes the file picker once its selection was
	// delivered.
	filePickerDoneMsg struct{}

	// chatHighlightCopiedMsg clears the chat's mouse selection after its
	// text reached the clipboard.
	chatHighlightCopiedMsg struct{}

	// chatReleaseCopyMsg fires a double-click interval after a mouse
	// release left a chat highlight; the highlight is copied unless another
	// click came in since.
	chatReleaseCopyMsg struct{}

	// hyperRefreshDoneMsg is sent after a silent Hyper OAuth refresh
	// finishes. It carries the original model-selection action so the
	// selection can be resumed.
	hyperRefreshDoneMsg struct {
		action dialog.ActionSelectModel
	}

	// copilotImportDoneMsg is sent after the GitHub Copilot token import
	// finishes. It carries the original model-selection action so the
	// selection can be resumed.
	copilotImportDoneMsg struct {
		action dialog.ActionSelectModel
	}

	// copyChatHighlightMsg is sent to copy the current chat highlight to clipboard.
	copyChatHighlightMsg struct{}

	// sessionFilesUpdatesMsg is sent when the files for this session have been updated
	sessionFilesUpdatesMsg struct {
		// sessionID is the chat the files were loaded for.
		sessionID    string
		sessionFiles []SessionFile
	}

	// creditsUpdatedMsg is sent when the remaining Hyper credits have been
	// fetched from the API. credits is nil when the team has hypercredit
	// display disabled.
	creditsUpdatedMsg struct {
		credits *int
	}

	// hyperCreditsPollMsg is sent by the Hyper credits poll timer.
	hyperCreditsPollMsg struct{}

	// cliLimitsMsg is sent when an agent CLI's usage limits were fetched.
	cliLimitsMsg struct{}
	// gitBranchUpdatedMsg carries the workspace's checked-out branch. branch
	// is empty when the workspace is not a Git repository or HEAD is
	// detached.
	gitBranchUpdatedMsg struct {
		branch string
	}

	// gitBranchPollMsg is sent by the git branch poll timer.
	gitBranchPollMsg struct{}
)

// UI represents the main user interface model.
type UI struct {
	com          *common.Common
	session      *session.Session
	sessionFiles []SessionFile
	// sessionLoad tracks the session being opened (see session_load.go).
	sessionLoad  sessionLoadState
	subagentView *subagentView

	// keeps track of read files while we don't have a session id
	sessionFileReads []string

	// initialSessionID is set when loading a specific session on startup.
	initialSessionID string
	// continueLastSession is set to continue the most recent session on startup.
	continueLastSession bool

	// remote is the window's Remote Control share; see remote.go.
	remote remoteShare

	// inSessionHost: this Crush is one session in the session list, and
	// reports its state there; see session_host.go.
	inSessionHost  bool
	lastHostStatus sessionhost.Status
	hostStatusSent bool

	// relaunchDir is set when the user opens another project; the caller
	// restarts Crush there after the program exits.
	relaunchDir       string
	relaunchSessionID string
	relaunchDataDir   string

	lastUserMessageTime int64

	// The width and height of the terminal in cells.
	width  int
	height int
	layout uiLayout

	isTransparent bool
	// resendBackground flips to make the renderer send the background
	// color again after the terminal dropped it.
	resendBackground bool
	// backgroundResends counts resends the terminal hasn't taken yet.
	backgroundResends int
	// watchBackground tells [WatchTerminalBackground] whether to check.
	watchBackground atomic.Bool

	// mouseEnabled controls whether Bubble Tea mouse reporting is active.
	// When false, the terminal emulator (or tmux) handles text selection,
	// copy/paste, right-click, and scrolling instead of Crush.
	mouseEnabled bool

	// theme tracks the applied theme and any preview; see theme.go.
	theme themeController

	// configRefreshGen counts config refreshes; a theme load or main
	// agent read started by an older one is dropped. See config_refresh.go.
	configRefreshGen uint64

	focus uiFocusState
	state uiState
	mode  uiInputMode

	// Frame memoization (see framecache.go): frames holds the memoized
	// frames, frame the per-update bookkeeping and the last view.
	frames *frameCache
	frame  frameState

	// taskRow lists the background sub-agents and processes under the
	// editor; see tasks.go.
	taskRow taskRow

	// planReadySessionID holds the session whose plan run emitted the
	// plan-ready marker but has not been confirmed for execution yet. It
	// lets the user reopen the handoff prompt after dismissing it.
	planReadySessionID string
	// modeSwitching is true while the async agent-model update kicked off
	// by setInputMode is still in flight; sending is blocked meanwhile.
	modeSwitching bool

	// cycleYolo is true while YOLO was enabled by the Shift+Tab input-mode
	// cycle, which is the only case where the cycle may disable it again.
	cycleYolo bool

	keyMap KeyMap
	keyenh tea.KeyboardEnhancementsMsg

	dialog *dialog.Overlay
	status *Status

	// pendingBangCommand holds a shell command that was issued before
	// the session finished loading. The loadSessionMsg handler creates
	// the pending UI item and starts execution once the chat list is
	// stable, eliminating races between session load and shell output.
	pendingBangCommand string

	// bangCancel cancels a running bang-mode shell command. Nil when no
	// bang command is in progress. Set by runShellCommand, cleared by
	// shellResultMsg. Checked by isAgentBusy and cancelAgent so that
	// Escape works for bang commands the same way it does for agent runs.
	bangCancel context.CancelFunc

	header *header

	// sendProgressBar instructs the TUI to send progress bar updates to the
	// terminal.
	sendProgressBar bool
	// widthProbePending is set while the terminal's answer to
	// common.WidthProbe is outstanding.
	widthProbePending  bool
	progressBarEnabled bool

	// turnOutcome is the result of the last agent turn reported to the
	// terminal as program status (done or error) until the user has seen
	// it, or empty.
	turnOutcome tea.ProgramState

	// caps hold different terminal capabilities that we query for.
	caps common.Capabilities
	// previewPlaced is the question form sketch on screen, if any.
	previewPlaced *questionform.PreviewPlacement

	// composer is the prompt editor; see composer.go.
	composer

	// Active inline editor replaces the textarea when non-nil.
	activeInline dialog.InlineEditor
	// cliUpdatePrompt is the CLI update form last opened, so agent
	// questions don't mistake it for one of theirs.
	cliUpdatePrompt *cliUpdatePrompt
	// selfUpdateRunning is set while a Crush update merges, builds and tests.
	selfUpdateRunning bool
	// inlineCursor stores the cursor from the last inline editor
	// Draw call, used by the cursor positioning logic below.
	inlineCursor *tea.Cursor

	// Attachment list
	attachments *attachments.Attachments
	imagePress  *imagePress
	// pendingKeptSkills are skills turned on before the chat exists. They
	// are saved to the chat when the first message creates it.
	pendingKeptSkills []session.KeptSkill

	// mention is the @ mention completions popup; see mention.go.
	mention mentionCompletions

	// Chat components
	chat *Chat

	// onboarding state
	onboarding struct {
		yesInitializeSelected bool
	}

	// lspProbe memoizes the workspace LSP state and per-server severity
	// counts (each probe behind them is a synchronous HTTP round-trip in
	// client/server mode, and the sidebar, landing view, and compact header
	// render them every frame). LSP events refresh them off-thread with a
	// TTL backstop; see lsp.go.
	lspProbe asyncProbe[lspSnapshot]

	// mcpProbe memoizes the workspace MCP state (a synchronous probe in
	// client/server mode). MCP state_changed events refresh it off-thread
	// with a TTL backstop and a retry loop while servers are starting; see
	// mcp.go.
	mcpProbe asyncProbe[map[string]mcp.ClientInfo]
	// mcpStartingRetries counts the consecutive starting-state re-probes
	// driving the retry loop's exponential backoff. Event-driven refreshes
	// renew the budget; settled states reset it.
	mcpStartingRetries int

	// skills
	skillStates []*skills.SkillState
	// skillCatalog is the visible skills, refreshed with the custom
	// commands; $name mentions in the prompt are matched against it.
	skillCatalog []skills.CatalogEntry

	// logoFrame is the wordmark's animation frame; see logoshine.go.
	// logoShineStart times its cycles.
	logoFrame      logoFrame
	logoShineStart time.Time

	// sidebar is the chat sidebar's logo and scroll state; see sidebar.go.
	sidebar sidebarPane

	// Notification state
	notifyBackend       notification.Backend
	notifyWindowFocused bool
	// memoryHigh is the last memory check's warning state; memoryWarning
	// is the status text it put up, so it can be told apart and cleared.
	memoryHigh bool
	// layoutAttachments is whether the last layout had attachments.
	layoutAttachments bool
	memoryWarning     string
	// custom commands & mcp commands
	customCommands []commands.CustomCommand
	mcpPrompts     []commands.MCPPrompt

	// forceCompactMode tracks whether compact mode is forced by user toggle
	forceCompactMode bool

	// isCompact tracks whether we're currently in compact layout mode (either
	// by user toggle or auto-switch based on window size)
	isCompact bool

	// detailsOpen tracks whether the details panel is open (in compact mode)
	detailsOpen bool

	// pills state
	pillsExpanded      bool
	pillsAutoExpanded  bool
	focusedPillSection pillSection
	// queue is the prompts sent but not answered yet: queued behind the
	// running turn, being submitted, recalled or held; see prompt_queue.go.
	queue promptQueue
	// agentBusyCache / yoloCache memoize the workspace busy and permission
	// probes (synchronous HTTP round-trips in client/server mode). Reads
	// never probe; refreshes happen off-thread (see workspace_cache.go).
	agentBusyCache    ttlCache
	yoloCache         ttlCache
	busyFetchInFlight bool
	// agentReady memoizes the coordinator readiness (AgentIsReady is a
	// synchronous HTTP GET in client/server mode, and modelInfo renders it
	// every frame). Seeded once at construction and refreshed by the same
	// off-thread probe as agentBusyCache.
	agentReady bool
	// limitsProvider is the provider whose usage limits were last fetched
	// for the shown chat's model.
	limitsProvider string
	// lastPick is the model a new chat starts on, shown on the landing
	// page; read once at construction and kept fresh by picks made here.
	lastPick config.SelectedModel
	// busyFetchGen is bumped by every busy/permission state transition;
	// like queue.gen it lets a stale in-flight probe result be
	// discarded and re-fetched instead of clobbering newer state.
	busyFetchGen uint64
	pillsView    string
	// skillTags are where the kept skills' × sit under the composer.
	skillTags []skillTag

	// Todo spinner
	todoSpinner    spinner.Model
	todoIsSpinning bool

	// mouse highlighting related state
	lastClickTime time.Time
	hoverX        int
	hoverY        int

	// hyperCredits is the remaining Hyper credits as last fetched from
	// the /v1/credits endpoint. It is nil when no fetch has reported a
	// balance yet, or when the team has hypercredit display disabled, and
	// no balance is rendered in either case.
	hyperCredits *int

	// gitBranch is the workspace's checked-out branch as of the last poll,
	// empty when there is none to show. Reading it costs a file read
	// locally and a request in client/server mode, so renders take it from
	// here rather than asking the workspace per frame.
	gitBranch string
}

// New creates a new instance of the [UI] model.
func New(com *common.Common, initialSessionID string, continueLast bool) *UI {
	// Editor components
	ta := textarea.New()
	ta.SetStyles(com.Styles.Editor.Textarea)
	ta.ShowLineNumbers = false
	ta.CharLimit = -1
	ta.SetVirtualCursor(false)
	ta.DynamicHeight = true
	ta.MinHeight = TextareaMinHeight
	ta.MaxHeight = TextareaMaxHeight
	// Keep "ctrl+a" for line-start (the textarea default); bind select-all
	// to "ctrl+shift+a" instead (line-start is also available via "home").
	ta.KeyMap.LineStart = key.NewBinding(
		key.WithKeys("home", "ctrl+a"),
		key.WithHelp("home", "line start"),
	)
	ta.KeyMap.SelectAll = key.NewBinding(
		key.WithKeys("ctrl+shift+a"),
		key.WithHelp("ctrl+shift+a", "select all"),
	)
	// Copying is handled by crush's keymap (Editor.CopySelection) so it can
	// use crush's clipboard backend and user feedback; disable the
	// textarea's built-in copy binding.
	ta.KeyMap.CopySelection = key.NewBinding()
	ta.Focus()

	ch := NewChat(com, com.Config().Options.TUI.Scrollbar)

	keyMap := DefaultKeyMap()

	// Completions component
	comp := completions.New(
		com.Styles.Completions.Normal,
		com.Styles.Completions.Focused,
		com.Styles.Completions.Match,
	)

	todoSpinner := spinner.New(
		spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(com.Styles.Pills.TodoSpinner),
	)

	// Attachments component
	attachments := attachments.New(
		attachments.NewRenderer(com.Styles.Attachments),
		attachments.Keymap{
			DeleteMode: keyMap.Editor.AttachmentDeleteMode,
			DeleteAll:  keyMap.Editor.DeleteAllAttachments,
			Escape:     keyMap.Editor.Escape,
		},
	)

	header := newHeader(com)

	ui := &UI{
		inSessionHost:       sessionhost.InHost(),
		com:                 com,
		dialog:              dialog.NewOverlay(),
		keyMap:              keyMap,
		composer:            composer{textarea: ta},
		chat:                ch,
		header:              header,
		attachments:         attachments,
		todoSpinner:         todoSpinner,
		frames:              newFrameCache(frameCacheTTL, frameCacheMaxEntries),
		notifyBackend:       notification.NoopBackend{},
		notifyWindowFocused: true,
		initialSessionID:    initialSessionID,
		continueLastSession: continueLast,
		skillStates:         skills.GetLatestStates(),
	}

	// Dialogs read the shown chat's model through com.
	com.ShownModel = ui.shownChoice
	status := NewStatus(com, ui)
	ui.mention.popup = comp
	ui.lspProbe.val.states = make(map[string]workspace.LSPClientInfo)
	ui.mcpProbe.val = make(map[string]mcp.ClientInfo)

	// Seed the last pick once (a synchronous round-trip in client/server
	// mode, like the yolo seed below) so the landing page shows the model a
	// new chat starts on; picks made here keep it fresh.
	if pick, ok := com.Workspace.LastPick(context.Background()); ok {
		ui.lastPick = pick
	}

	// Seed the active theme key from the shown model's provider so the
	// first model selection can correctly skip a redundant theme swap.
	if cfg := com.Config(); cfg != nil {
		ui.theme.key = styles.ThemeKeyForProvider(ui.lastPick.Provider)
		ui.theme.userSelected = common.ThemeNameFromConfig(cfg) != ""
		ui.theme.shown = common.ThemeNameFromConfig(cfg)
		ui.theme.revision = styles.ThemeFileRevision(ui.theme.shown)
	}

	// Seed the yolo cache once at construction; afterwards it is kept
	// fresh by write-through toggles and off-thread refreshes so Update
	// and View never probe the workspace synchronously.
	yolo := com.Workspace.PermissionSkipRequests()
	ui.yoloCache.set(yolo)

	// Seed the memoized agent ready state the same way so the first frame
	// renders the model info; the busy probe keeps it fresh afterwards.
	if com.Workspace.AgentIsReady() {
		ui.agentReady = true
	}
	ui.mode = uiInputModeCode
	ui.setEditorPrompt(yolo)
	ui.randomizePlaceholders()
	ui.textarea.Placeholder = ui.readyPlaceholder
	ui.status = status

	// Initialize compact mode from config
	ui.forceCompactMode = com.Config().Options.TUI.CompactMode

	// set onboarding state defaults
	ui.onboarding.yesInitializeSelected = true

	desiredState := uiLanding
	desiredFocus := uiFocusEditor
	if !com.Config().IsConfigured() {
		desiredState = uiOnboarding
	} else if n, _ := com.Workspace.ProjectNeedsInitialization(); n {
		desiredState = uiInitialize
	}

	// set initial state
	ui.setState(desiredState, desiredFocus)

	opts := com.Config().Options

	// disable indeterminate progress bar
	ui.progressBarEnabled = opts.Progress == nil || *opts.Progress
	// enable transparent mode
	ui.isTransparent = opts.TUI.IsTransparent()
	// enable mouse support (default on)
	ui.mouseEnabled = opts.TUI.Mouse == nil || *opts.TUI.Mouse
	// busy lines follow the thought line setting as it changes
	chat.SetThoughtLineSetting(ui.showThoughtLine)

	return ui
}

// Init initializes the UI model.
func (m *UI) Init() tea.Cmd {
	var cmds []tea.Cmd
	if m.state == uiOnboarding {
		if cmd := m.openModelsDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	// load the user commands async
	m.logoShineStart = time.Now()
	cmds = append(cmds, m.loadCustomCommands(), m.pollBgProcs(), m.checkCLIUpdates(false), m.checkSelfUpdate(false), m.logoShineTick(), m.memoryCheckTick(), m.applySavedMemoryLimit(), m.loadEngineShares())
	// Prime the memoized LSP state off-thread.
	if cmd := m.requestLSPRefresh(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	// Opened from the phone (crush remote launcher): share this window at once.
	// The variable is cleared so nothing started from here inherits it.
	if id := os.Getenv(remote.LaunchEnv); id != "" {
		_ = os.Unsetenv(remote.LaunchEnv)
		remote.SetLaunchID(id)
		m.remote.quiet = true
		if cmd := m.openRemote(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	// load initial session if specified
	initialSession := m.loadInitialSession()
	if initialSession != nil {
		cmds = append(cmds, initialSession)
	}
	// loadSessionMsg reloads history for whichever session arrives, so
	// doing it here too would be discarded — and with no session set yet
	// it reads every user message in the database, which the pending
	// session load then queues behind on the single connection.
	if initialSession == nil {
		cmds = append(cmds, m.loadPromptHistory())
	}
	// Prime the ChatGPT model catalog: a signed-in OpenAI provider
	// whose catalog is missing (the fetch at login failed, or the
	// credentials predate it) refills lazily, so the models dialog shows
	// the subscription section as soon as it is opened.
	ws := m.com.Workspace
	cmds = append(cmds, m.updateAgentModelCmd(func() tea.Msg {
		_ = ws.UpdateAgentModel(context.TODO())
		return nil
	}))
	// Prime the memoized busy/permission state off-thread.
	if cmd := m.dispatchBusyRefresh(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	// The credits balance is shown from the first frame on, so fetch it
	// right away and keep polling it while Crush sits idle. The poll runs
	// for every provider: it is a no-op unless Hyper is selected.
	if m.com.IsHyper() {
		cmds = append(cmds, m.fetchHyperCredits())
	}
	cmds = append(cmds, m.fetchCLILimits())
	// The branch is shown from the first frame on, so read it now and keep
	// polling for checkouts made outside Crush.
	cmds = append(cmds, m.fetchGitBranch(), m.gitBranchTicker())
	cmds = append(cmds, m.hyperCreditsTicker())
	// Prime the memoized MCP state off-thread. There is deliberately no
	// wait for server-side MCP initialization: the init gate is
	// process-local and only armed where mcp.Initialize runs (the server),
	// so waiting here is a no-op in client/server mode. The retry loop and
	// TTL backstop in mcp.go converge on the settled states instead.
	if cmd := m.requestMCPRefresh(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

// Update handles updates to the UI model.
func (m *UI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(questionform.PreviewReadyMsg); ok {
		m.frame.invalidate()
	}
	model, cmd := m.update(msg)
	return model, tea.Batch(cmd, m.syncPreview(), m.hostStatus())
}

// syncPreview keeps a question form's choice sketch on screen in step
// with the form: placed over its blank area as of the last draw, moved
// when that changes, and removed once the form closes or something
// covers it.
func (m *UI) syncPreview() tea.Cmd {
	var want *questionform.PreviewPlacement
	var load tea.Cmd
	form, ok := m.activeInline.(*questionform.QuestionForm)
	if ok && !m.dialog.HasDialogs() && m.state == uiChat {
		want, load = form.PreviewState()
	}
	// Look again once the next frame is drawn: the form may have moved, or
	// a just-opened form shows where its sketch goes.
	recheck := tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg { return questionform.PreviewReadyMsg{} })
	if equalPlacement(want, m.previewPlaced) {
		if ok && form.PreviewWaiting() {
			return tea.Batch(load, recheck)
		}
		return load
	}
	old := m.previewPlaced
	m.previewPlaced = want
	_, tmux := m.caps.Env.LookupEnv("TMUX")
	return tea.Batch(load, questionform.PreviewPlacementCmd(old, want, tmux), recheck)
}

func equalPlacement(a, b *questionform.PreviewPlacement) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
