package model

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/crush/internal/ui/dialog/mcpservers"
	"github.com/charmbracelet/crush/internal/ui/dialog/questionform"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/selfupdate"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/sessionhost"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/completions"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	fimage "github.com/charmbracelet/crush/internal/ui/image"
	"github.com/charmbracelet/crush/internal/ui/notification"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/charmbracelet/crush/internal/workspace"
	uv "github.com/charmbracelet/ultraviolet"
	xstrings "github.com/charmbracelet/x/exp/strings"
)

// updateFlow says how update goes on after a message handler.
type updateFlow uint8

const (
	// flowContinue runs the work every update ends with: the placeholder,
	// the refresh backstop, attachments, the animation clock and the frame
	// GC (see finishUpdate).
	flowContinue updateFlow = iota
	// flowReturn skips that work and returns the commands gathered so far
	// together with the handler's.
	flowReturn
	// flowReplace skips it too and returns only the handler's command.
	flowReplace
)

// update handles one message: the secure entry and the sub-agent view get
// first pick, routeMsg hands it to its handler, and finishUpdate does the
// work every update ends with unless the handler ended the update early.
func (m *UI) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// A release ends a scrollbar drag wherever it goes, so a dialog or the
	// secure entry opening mid-drag can't leave the drag stuck on.
	var endedScrollbarDrag bool
	if _, ok := msg.(tea.MouseReleaseMsg); ok {
		endedScrollbarDrag = m.chat.HandleScrollbarRelease()
	}

	// Secure input bypasses chat, clipboard attachments, global keys, history,
	// question answers, and remote presence handling entirely.
	if cmd, ok := m.handleSecureEntryMsg(msg); ok {
		return m, cmd
	}

	var cmds []tea.Cmd
	m.beginFrameUpdate()
	defer m.syncRemotePresence()
	if cmd, consumed := m.handleSubagentView(msg); consumed {
		return m, cmd
	} else if cmd != nil {
		cmds = append(cmds, cmd)
	}
	// Update terminal capabilities
	m.caps.Update(msg)

	cmd, flow := m.routeMsg(msg, endedScrollbarDrag)
	switch flow {
	case flowReturn:
		return m, tea.Batch(append(cmds, cmd)...)
	case flowReplace:
		return m, cmd
	}
	cmds = append(cmds, cmd)
	cmds = append(cmds, m.finishUpdate(msg)...)
	return m, tea.Batch(cmds...)
}

// routeMsg hands a message to its handler: input to its owner, then the
// terminal's, the workspace's and the UI's own messages by concern.
// Anything else goes to the dialogs, if any are open.
func (m *UI) routeMsg(msg tea.Msg, endedScrollbarDrag bool) (tea.Cmd, updateFlow) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKeyPressMsg(msg), flowContinue
	case tea.PasteMsg:
		return m.routePaste(msg)
	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)
	case tea.MouseMotionMsg:
		return m.handleMouseMotion(msg)
	case tea.MouseReleaseMsg:
		return m.handleMouseRelease(msg, endedScrollbarDrag)
	case common.CoalescedWheelMsg:
		return m.handleMouseWheel(msg)
	}
	if cmd, flow, ok := m.handleTerminalMsg(msg); ok {
		return cmd, flow
	}
	if cmd, flow, ok := m.handleWorkspaceEvent(msg); ok {
		return cmd, flow
	}
	if cmd, ok := m.handleRemoteMsg(msg); ok {
		return cmd, flowContinue
	}
	if cmd, ok := m.handleAgentMsg(msg); ok {
		return cmd, flowContinue
	}
	if cmd, ok := m.handleSessionMsg(msg); ok {
		return cmd, flowContinue
	}
	if cmd, ok := m.handleUIMsg(msg); ok {
		return cmd, flowContinue
	}
	if cmd, ok := m.handleTickMsg(msg); ok {
		return cmd, flowContinue
	}
	if m.dialog.HasDialogs() {
		return m.handleDialogMsg(msg), flowContinue
	}
	return nil, flowContinue
}

// finishUpdate is the work every update ends with, whatever the message.
func (m *UI) finishUpdate(msg tea.Msg) []tea.Cmd {
	m.syncPlaceholder()

	// TTL backstop: schedule an off-thread re-probe for any memoized
	// workspace state that has gone stale. Never does IO on this
	// goroutine.
	cmds := m.staleWorkspaceRefreshCmds()

	// at this point this can only handle [message.Attachment] message, and we
	// should return all cmds anyway.
	if m.attachments.Update(msg) {
		cmds = append(cmds, m.turnOffRemovedKeptSkills())
		m.frame.invalidate()
	}
	// Any update may have put a spinner on screen (new message, tool update,
	// scroll, session load); make sure the clock is running. This is the
	// sole place the clock is armed so a tick never sits inside a caller's
	// tea.Sequence.
	if m.state == uiChat {
		if cmd := m.chat.EnsureAnimating(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if v := m.subagentView; v != nil {
			if cmd := v.chat.EnsureAnimating(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	if cmd := m.endFrameUpdate(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return cmds
}

// syncPlaceholder keeps the focused editor's placeholder in step with the
// mode and the agent. It runs at the end of every update.
func (m *UI) syncPlaceholder() {
	prevPlaceholder := m.textarea.Placeholder
	if m.focus == uiFocusEditor {
		m.textarea.Placeholder = m.placeholder(m.isAgentBusy(), m.mode == uiInputModePlan, m.yoloModeCached())
	}
	if m.textarea.Placeholder != prevPlaceholder {
		m.frame.invalidate()
	}
}

// handleTerminalMsg handles what the terminal reports: its environment,
// capabilities, size, focus and background.
// ok is false for any other message.
func (m *UI) handleTerminalMsg(msg tea.Msg) (tea.Cmd, updateFlow, bool) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.EnvMsg:
		// Is this Windows Terminal?
		if !m.sendProgressBar {
			m.sendProgressBar = slices.Contains(msg, "WT_SESSION")
		}
		cmds = append(cmds, common.QueryCmd(uv.Environ(msg)))
		if !m.widthProbePending {
			m.widthProbePending = true
			cmds = append(cmds, tea.Raw(common.WidthProbe()))
		}
	case tea.CursorPositionMsg:
		if cmd := m.handleWidthProbe(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case tea.ModeReportMsg:
		m.updateNotificationBackend()
	case uv.UnknownOscEvent:
		if port, ok := sessionhost.ParseStopServer(string(msg)); ok && m.inSessionHost {
			cmds = append(cmds, m.stopHostServer(port))
			break
		}
		m.updateNotificationBackend()
	case tea.FocusMsg:
		m.notifyWindowFocused = true
		m.turnOutcome = ""
		if !m.isTransparent {
			cmds = append(cmds, tea.RequestBackgroundColor)
		}
	case tea.BackgroundColorMsg:
		m.handleTerminalBackground(msg)

	case tea.BlurMsg:
		m.notifyWindowFocused = false
	case tea.TerminalVersionMsg:
		termVersion := strings.ToLower(msg.Name)
		// Only enable progress bar for the following terminals.
		if !m.sendProgressBar {
			m.sendProgressBar = xstrings.ContainsAnyOf(termVersion, "ghostty", "iterm2", "rio")
		}
		return nil, flowReplace, true
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// Suppress the chat's full-height scan during the resize so a drag
		// only reflows visible items; it settles (and recomputes) shortly
		// after the last resize event.
		if m.state == uiChat {
			cmds = append(cmds, m.chat.BeginResize())
		}
		m.updateLayoutAndSize()
		if m.state == uiChat && m.chat.Follow() {
			m.chat.ScrollToBottom()
		}
	case tea.KeyboardEnhancementsMsg:
		m.keyenh = msg
		if msg.SupportsKeyDisambiguation() {
			m.keyMap.Models.SetHelp("ctrl+m", "models")
			m.keyMap.Editor.Newline.SetHelp("shift+enter", "newline")
		}
	case uv.KittyGraphicsEvent:
		if !bytes.HasPrefix(msg.Payload, []byte("OK")) {
			slog.Warn("Unexpected Kitty graphics response",
				"response", string(msg.Payload),
				"options", msg.Options)
		}
	default:
		return nil, flowContinue, false
	}
	return tea.Batch(cmds...), flowContinue, true
}

// handleRemoteMsg handles the Remote Control share's messages.
// ok is false for any other message.
func (m *UI) handleRemoteMsg(msg tea.Msg) (tea.Cmd, bool) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case remoteStartedMsg:
		cmds = append(cmds, m.handleRemoteStarted(msg))
	case remoteSetupPollMsg:
		cmds = append(cmds, m.handleRemoteSetupPoll(msg))
	case remoteSetupFinishedMsg:
		cmds = append(cmds, m.handleRemoteSetupFinished(msg))
	case remoteActionMsg:
		cmds = append(cmds, m.handleRemoteAction(msg.action))
	case remotePhonesMsg:
		cmds = append(cmds, m.handleRemotePhones())
	default:
		return nil, false
	}
	return tea.Batch(cmds...), true
}

// handleWorkspaceEvent handles the events the workspace publishes:
// sessions, messages, files, LSP and MCP state, permissions, questions
// and tasks.
// ok is false for any other message.
func (m *UI) handleWorkspaceEvent(msg tea.Msg) (tea.Cmd, updateFlow, bool) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case pubsub.Event[notify.Notification]:
		if cmd := m.handleAgentNotification(msg.Payload); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case pubsub.Event[notify.RunComplete]:
		if msg.Payload.Cancelled {
			cmds = append(cmds, m.finishPendingPrompt(msg.Payload.SubmissionID, false))
		} else if msg.Payload.Error != "" {
			cmds = append(cmds, m.resolvePendingPrompt(msg.Payload.SubmissionID, true))
		}
		if cmd := m.handlePlanHandoff(msg.Payload); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case pubsub.Event[session.Session]:
		cmds = append(cmds, m.handleSessionEvent(msg)...)
	case pubsub.Event[message.Message]:
		cmds = append(cmds, m.handleMessageEvent(msg)...)
	case pubsub.Event[history.File]:
		cmds = append(cmds, m.handleFileEvent(msg.Payload))
	case pubsub.Event[app.LSPEvent]:
		// Refresh the memoized LSP state off-thread: LSPGetStates is a
		// synchronous HTTP round-trip in client/server mode and diagnostics
		// events can arrive per edited file.
		if cmd := m.requestLSPRefresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case pubsub.Event[workspace.LSPEvent]:
		if cmd := m.requestLSPRefresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case pubsub.Event[skills.Event]:
		m.skillStates = msg.Payload.States
	case pubsub.Event[mcp.Event]:
		switch msg.Payload.Type {
		case mcp.EventStateChanged:
			return tea.Batch(m.handleStateChanged()...), flowReplace, true
		case mcp.EventPromptsListChanged:
			return handleMCPPromptsEvent(m.com.Workspace, msg.Payload.Name), flowReplace, true
		case mcp.EventToolsListChanged:
			return handleMCPToolsEvent(m.com.Workspace, msg.Payload.Name), flowReplace, true
		case mcp.EventResourcesListChanged:
			return handleMCPResourcesEvent(m.com.Workspace, msg.Payload.Name), flowReplace, true
		case mcp.EventChannelMessage:
			return m.handleChannelMessage(msg.Payload), flowReplace, true
		}
	case pubsub.Event[permission.PermissionRequest]:
		if cmd := m.openPermissionsDialog(msg.Payload); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := m.sendNotification(notification.Notification{
			Title:   "Crush is waiting...",
			Message: fmt.Sprintf("Permission required to execute \"%s\"", msg.Payload.ToolName),
		}); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case pubsub.Event[permission.PermissionNotification]:
		m.handlePermissionNotification(msg.Payload)
	case pubsub.Event[question.Request]:
		if cmd := m.openBatchFormDialog(msg.Payload); cmd != nil {
			cmds = append(cmds, cmd)
		}
		m.chat.ScrollToBottom()
		cmds = append(cmds, m.playNotificationSound(notification.SoundQuestion))
		if cmd := m.sendNotification(notification.Notification{
			Title:   "Crush is waiting...",
			Message: fmt.Sprintf("%d questions need your input", len(msg.Payload.Questions)),
		}); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case pubsub.Event[agent.Task]:
		if cmd := m.handleTaskEvent(msg.Payload); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case pubsub.Event[question.Notification]:
		m.handleQuestionNotification(msg.Payload)
	case workspace.ConnectionEvent:
		cmds = append(cmds, m.handleConnectionEvent(msg)...)
	default:
		return nil, flowContinue, false
	}
	return tea.Batch(cmds...), flowContinue, true
}

// handleAgentMsg handles the results of the agent, model, shell and
// probe commands.
// ok is false for any other message.
func (m *UI) handleAgentMsg(msg tea.Msg) (tea.Cmd, bool) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case agentFinishedMsg:
		cmds = append(cmds, m.handleAgentFinished(msg))
	case busyStateMsg:
		cmds = append(cmds, m.applyBusyState(msg)...)
	case promptQueueMsg:
		cmds = append(cmds, m.applyPromptQueue(msg)...)
	case lspStatesMsg:
		if cmd := m.applyLSPStates(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case agentModelChangedMsg:
		// The coordinator model changed (selection, thinking, reasoning):
		// re-fetch the memoized ready/model state off-thread.
		m.invalidateBusyCaches()
		if cmd := m.dispatchBusyRefresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case agentRunSubmittedMsg:
		cmds = append(cmds, m.handleRunSubmitted(msg)...)
	case pendingPromptResolvedMsg:
		cmds = append(cmds, m.applyResolvedPrompt(msg))
	case agentInterruptedMsg:
		id := msg.sessionID
		if id == "" {
			id = m.currentSessionID()
		}
		delete(m.queue.interruptInFlight, id)
		if id != m.currentSessionID() {
			break
		}
		m.invalidateBusyCaches()
		m.queue.invalidate()
		cmds = append(cmds, m.dispatchBusyRefresh(), m.dispatchPromptQueueRefresh())
	case modeSwitchedMsg:
		m.modeSwitching = false
		cmds = append(cmds, m.applyModeSwitch(msg)...)

	case sendMessageMsg:
		cmds = append(cmds, m.sendMessage(msg.Content, msg.Attachments...))

	case mcpStateChangedMsg:
		if dia := m.dialog.Dialog(dialog.MCPTogglesID); dia != nil {
			if toggles, ok := dia.(*mcpservers.MCPToggles); ok {
				for name, info := range msg.states {
					toggles.SetItemStatus(name, mcpStatusText(info))
				}
			}
		}
		cmds = append(cmds, m.applyMCPStates(msg)...)
	case mcpStartingRetryMsg:
		// A server was still connecting when the last fetch landed; re-probe
		// so "starting..." converges without depending on state_changed
		// events (which can be missed in client/server mode).
		if cmd := m.requestMCPRefresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case queuedPromptRecalledMsg:
		cmds = append(cmds, m.applyRecalledPrompt(msg))

	case shellStreamMsg:
		cmds = append(cmds, m.handleShellStream(msg)...)
	case shellResultMsg:
		cmds = append(cmds, m.handleShellResult(msg)...)
	case hyperRefreshDoneMsg:
		if cmd := m.handleSelectModel(msg.action); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case creditsUpdatedMsg:
		m.hyperCredits = msg.credits
	case usageRefreshedMsg:
		m.applyUsageRefresh(msg)
	case cliLimitsMsg:
		// New limits are read while drawing; this only redraws.
	case gitBranchUpdatedMsg:
		m.gitBranch = msg.branch
	case dialog.ActionMCPAuthStarted:
		cmds = append(cmds, m.authenticateMCP(msg.Ctx, msg.Name))
	case dialog.ActionMCPAuthComplete, dialog.ActionMCPAuthErrored:
		// The OAuth flow finished server-side: refresh the memoized states
		// so the sidebar reflects the outcome even when the state_changed
		// event was missed.
		if cmd := m.requestMCPRefresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if m.dialog.HasDialogs() {
			if cmd := m.handleDialogMsg(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	default:
		return nil, false
	}
	return tea.Batch(cmds...), true
}

// handleSessionMsg handles the results of session, history, command and
// update loading.
// ok is false for any other message.
func (m *UI) handleSessionMsg(msg tea.Msg) (tea.Cmd, bool) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case filePreviewMsg:
		cmds = append(cmds, m.finishFilePreview(msg))
	case fileUndoMsg:
		cmds = append(cmds, m.finishFileUndo(msg))
	case treeFinishedMsg:
		cmds = append(cmds, m.finishTree(msg))
	case loadSessionMsg, sessionLoadFailedMsg, sessionLoadTickMsg, openSessionMsg:
		cmds = append(cmds, m.handleSessionLoadMsg(msg)...)

	case sessionFilesUpdatesMsg:
		m.sessionFiles = msg.sessionFiles
		var paths []string
		for _, f := range msg.sessionFiles {
			paths = append(paths, f.LatestVersion.Path)
		}
		cmds = append(cmds, m.startLSPs(paths))

	case keptSkillsListMsg:
		if msg.err != nil {
			cmds = append(cmds, util.ReportError(msg.err))
			break
		}
		if !m.dialog.ContainsDialog(dialog.KeptSkillsID) {
			m.dialog.OpenDialog(dialog.NewKeptSkills(m.com, m.keptSkillItems(msg.entries)))
		}
	case keptSkillsChangedMsg:
		if msg.err != nil {
			cmds = append(cmds, util.ReportError(msg.err))
			break
		}
		if m.session != nil && m.session.ID == msg.session.ID {
			m.session.KeptSkills = msg.session.KeptSkills
			m.syncKeptSkillsDialog()
			m.frame.invalidate()
		}
	case userCommandsLoadedMsg:
		m.customCommands = msg.Commands
		dia := m.dialog.Dialog(dialog.CommandsID)
		if dia == nil {
			break
		}

		commands, ok := dia.(*dialog.Commands)
		if ok {
			commands.SetCustomCommands(m.customCommands)
		}

	case mcpPromptsLoadedMsg:
		m.mcpPrompts = msg.Prompts
		dia := m.dialog.Dialog(dialog.CommandsID)
		if dia == nil {
			break
		}

		commands, ok := dia.(*dialog.Commands)
		if ok {
			commands.SetMCPPrompts(m.mcpPrompts)
		}

	case promptHistoryLoadedMsg:
		m.promptHistory.messages = msg.messages
		m.promptHistory.index = -1
		m.promptHistory.draft = ""
	case reviewLoadedMsg:
		cmds = append(cmds, m.applyReview(msg))
	case cliUpdatesMsg:
		cmds = append(cmds, m.handleCLIUpdates(msg))
	case selfUpdateMsg:
		cmds = append(cmds, m.handleSelfUpdate(msg))
	case selfUpdateBuiltMsg:
		cmds = append(cmds, m.handleSelfUpdateBuilt(msg))
	case selfUpdateInstalledMsg:
		cmds = append(cmds, m.handleSelfUpdateInstalled(msg))
	case cliUpdatesInstalledMsg:
		cmds = append(cmds, m.handleCLIUpdatesInstalled(msg))
	default:
		return nil, false
	}
	return tea.Batch(cmds...), true
}

// handleUIMsg handles the messages the UI sends itself: dialogs, the chat
// selection, the editor and the status bar.
// ok is false for any other message.
func (m *UI) handleUIMsg(msg tea.Msg) (tea.Cmd, bool) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case dialog.CollapseInlineMsg:
		m.focusActiveInline(uiFocusMain)
	case closeDialogMsg:
		m.dialog.CloseFrontDialog()

	case dialog.SkillsResultMsg:
		if m.dialog.Dialog(dialog.SkillsID) == msg.Dialog {
			cmds = append(cmds, m.handleDialogAction(msg.Dialog.HandleMsg(msg)))
		} else if msg.Mutated {
			cmds = append(cmds, m.loadCustomCommands())
		}
	case copyChatHighlightMsg:
		cmds = append(cmds, m.copyChatHighlight())
	case chatReleaseCopyMsg:
		if time.Since(m.lastClickTime) >= doubleClickThreshold {
			cmds = append(cmds, m.copyChatHighlight())
		}
	case chatHighlightCopiedMsg:
		m.chat.ClearMouse()
	case questionform.OpenLinkMsg:
		cmds = append(cmds, openChatLink(msg.URL, m.com.Workspace.WorkingDir()))
	case openImageMsg:
		cmds = append(cmds, openAttachmentImage(msg.Attachment, m.com.Workspace.WorkingDir()))
	case DelayedClickMsg:
		// Handle delayed single-click action (e.g., expansion).
		m.chat.HandleDelayedClick(msg)
		cmds = append(cmds, m.takeChatLink(m.chat))
		if g := m.chat.TakeChangesRequest(); g != nil {
			cmds = append(cmds, m.openReview(g))
		}
	case openEditorMsg:
		prevHeight := m.textarea.Height()
		m.textarea.SetValue(msg.Text)
		m.textarea.MoveToEnd()
		m.syncBangModeFromTextarea()
		cmds = append(cmds, m.updateTextareaWithPrevHeight(msg, prevHeight))
	case configWriteMsg:
		cmds = append(cmds, m.applyConfigWrite(msg))
	case fileCompletionMsg:
		cmds = append(cmds, m.applyFileCompletion(msg))
	case filePickerDoneMsg:
		m.dialog.CloseDialog(dialog.FilePickerID)
		fimage.ResetCache()
	case util.InfoMsg:
		if msg.Type == util.InfoTypeError {
			slog.Error("Error reported", "error", msg.Msg)
		}
		m.status.SetInfoMsg(msg)
		ttl := msg.TTL
		if ttl <= 0 {
			ttl = DefaultStatusTTL
		}
		cmds = append(cmds, clearInfoMsgCmd(ttl))
	case app.UpdateAvailableMsg:
		if selfupdate.Dir() != "" {
			// Fork builds get the self-update offer instead.
			break
		}
		text := fmt.Sprintf("Crush update available: v%s → v%s.", msg.CurrentVersion, msg.LatestVersion)
		if msg.IsDevelopment {
			text = fmt.Sprintf("This is a development version of Crush. The latest version is v%s.", msg.LatestVersion)
		}
		ttl := 10 * time.Second
		m.status.SetInfoMsg(util.InfoMsg{
			Type: util.InfoTypeUpdate,
			Msg:  text,
			TTL:  ttl,
		})
		cmds = append(cmds, clearInfoMsgCmd(ttl))
	case util.ClearStatusMsg:
		m.status.ClearInfoMsg()
	case completions.CompletionItemsLoadedMsg:
		m.mention.setItems(msg)
	default:
		return nil, false
	}
	return tea.Batch(cmds...), true
}

// handleTickMsg handles timers: animation, polling and auto-hide ticks.
// ok is false for any other message.
func (m *UI) handleTickMsg(msg tea.Msg) (tea.Cmd, bool) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case taskTickMsg:
		if cmd := m.handleTaskTick(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case bgProcsMsg:
		cmds = append(cmds, m.handleBgProcs(msg))
	case logoShineMsg:
		cmds = append(cmds, m.handleLogoShine())
	case frameGCMsg:
		m.handleFrameGC()
	case animTickMsg:
		if cmd := m.handleAnimTick(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case scrollbarHideMsg:
		if m.state == uiChat {
			cmds = append(cmds, m.chat.HideScrollbar(msg.seq))
		}
	case chatWarmMsg:
		// A resize has settled; warm the message cache one batch at a time
		// so the scrollbar recompute never blocks the UI thread.
		if m.state == uiChat {
			cmd, done := m.chat.WarmStep(msg.seq)
			if cmd != nil {
				// Warming only fills caches; the frame is unchanged.
				m.frame.markScrollOnly()
				cmds = append(cmds, cmd)
			} else if done {
				// Heights are cached now, so the final layout pass (scrollbar
				// reservation) is cheap.
				m.updateLayoutAndSize()
			}
		}
	case sidebarScrollbarHideMsg:
		m.sidebar.hideScrollbar(msg.seq, m.focus == uiFocusSidebar)
	case spinner.TickMsg:
		if m.dialog.HasDialogs() {
			// route to dialog
			if cmd := m.handleDialogMsg(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if m.state == uiChat && m.hasSession() && hasInProgressTodo(m.session.Todos) && m.todoIsSpinning {
			var cmd tea.Cmd
			m.todoSpinner, cmd = m.todoSpinner.Update(msg)
			if cmd != nil {
				m.renderPills()
				cmds = append(cmds, cmd)
			}
		}

	case hyperCreditsPollMsg:
		// While a session runs every response refreshes the balance, so
		// the poll only has to cover idle time. Re-arm it either way: the
		// next poll may well land after the agent went idle again.
		if m.com.IsHyper() && !m.isAgentBusy() {
			cmds = append(cmds, m.fetchHyperCredits())
		}
		// Same for agent CLI usage limits: turns report their own.
		if !m.isAgentBusy() {
			cmds = append(cmds, m.fetchCLILimits())
		}
		cmds = append(cmds, m.hyperCreditsTicker())
	case usageTickMsg:
		if m.dialog.Dialog(dialog.UsageID) == msg.dialog {
			m.updateUsageData(msg.dialog)
			cmds = append(cmds, usageTick(msg.dialog))
		}
	case gitBranchPollMsg:
		cmds = append(cmds, m.fetchGitBranch(), m.gitBranchTicker())
	default:
		return nil, false
	}
	return tea.Batch(cmds...), true
}

// handleSessionEvent follows a change to a session: the open one deleted
// starts a new chat, the open one updated refreshes the todos and pills.
func (m *UI) handleSessionEvent(msg pubsub.Event[session.Session]) []tea.Cmd {
	var cmds []tea.Cmd
	if cmd := m.holdSessionLoadSession(msg); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if msg.Type == pubsub.DeletedEvent {
		if m.session != nil && m.session.ID == msg.Payload.ID {
			if cmd := m.newSession(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return cmds
	}
	if m.session != nil && msg.Payload.ID == m.session.ID {
		prevHasInProgress := hasInProgressTodo(m.session.Todos)
		prevPillsHeight := m.pillsAreaHeight()
		m.updateHyperCredits()
		m.session = &msg.Payload
		m.syncKeptSkillsDialog()
		if !prevHasInProgress && hasInProgressTodo(m.session.Todos) {
			m.todoIsSpinning = true
			cmds = append(cmds, m.todoSpinner.Tick)
		}
		// The pills panel reserves vertical space that the chat area
		// must yield. Recompute the layout whenever that footprint
		// changes (todos appearing, the list growing, etc.) so the
		// box renders on first paint rather than waiting for a toggle.
		// When the footprint is unchanged we still re-render the pill
		// content so status changes (e.g. the in-progress spinner)
		// show up.
		if m.pillsAreaHeight() != prevPillsHeight {
			m.updateLayoutAndSize()
		} else {
			m.renderPills()
		}
		m.autoExpandPillsIfReasonable()
	}
	return cmds
}

// handleMessageEvent follows a change to a message of the open session,
// or of a sub-agent run by one of its tools.
func (m *UI) handleMessageEvent(msg pubsub.Event[message.Message]) []tea.Cmd {
	var cmds []tea.Cmd
	// Events for a session still loading are replayed once it lands.
	if m.holdSessionLoadEvent(msg) {
		return cmds
	}
	// Check if this is a child session message for an agent tool.
	if m.session == nil {
		return cmds
	}
	if msg.Payload.SessionID != m.session.ID {
		// This might be a child session message from an agent tool.
		if cmd := m.handleChildSessionMessage(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return cmds
	}
	switch msg.Type {
	case pubsub.CreatedEvent:
		cmds = append(cmds, m.appendSessionMessage(msg.Payload))
		// A new message is a run boundary — a user prompt starting
		// a turn or the agent replying/dequeueing. Drop the
		// memoized busy state and re-fetch it and the queue
		// off-thread. Per-chunk UpdatedEvents deliberately do NOT
		// trigger this: during streaming that would put workspace
		// probes on every token.
		m.invalidateBusyCaches()
		m.queue.invalidate()
		if cmd := m.dispatchBusyRefresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := m.dispatchPromptQueueRefresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case pubsub.UpdatedEvent:
		cmds = append(cmds, m.updateSessionMessage(msg.Payload))
	case pubsub.DeletedEvent:
		m.chat.RemoveMessage(msg.Payload.ID)
	}
	// start the spinner if there is a new message
	if hasInProgressTodo(m.session.Todos) && m.isAgentBusy() && !m.todoIsSpinning {
		m.todoIsSpinning = true
		cmds = append(cmds, m.todoSpinner.Tick)
	}
	// stop the spinner if the agent is not busy anymore
	if m.todoIsSpinning && !m.isAgentBusy() {
		m.todoIsSpinning = false
	}
	// there is a number of things that could change the pills here so we want to re-render
	m.renderPills()
	return cmds
}

// handleRunSubmitted settles a prompt submission: a failed one is resolved
// or reported, and the busy and queue state is fetched to confirm the
// optimistic values sendMessage wrote.
func (m *UI) handleRunSubmitted(msg agentRunSubmittedMsg) []tea.Cmd {
	var cmds []tea.Cmd
	id := msg.dispatchID
	if id == "" {
		id = msg.submissionID
	}
	if submission, ok := m.queue.submitting[id]; ok {
		submission.cancel()
		delete(m.queue.submitting, id)
	}
	if msg.err != nil {
		if errors.Is(msg.err, context.Canceled) {
			cmds = append(cmds, m.finishPendingPrompt(msg.submissionID, false))
		} else {
			var definitive *message.DefinitiveSubmissionError
			cmds = append(cmds, m.resolvePendingPrompt(msg.submissionID, errors.As(msg.err, &definitive)), util.ReportError(msg.err))
		}
	}
	m.turnOutcome = ""
	// A prompt was just accepted (run started or enqueued): fetch the
	// authoritative busy/queue state to confirm the optimistic values
	// sendMessage wrote.
	m.invalidateBusyCaches()
	m.queue.invalidate()
	if cmd := m.dispatchBusyRefresh(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := m.dispatchPromptQueueRefresh(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return cmds
}

// handleShellStream appends a chunk of a bang command's output to its
// pending item and keeps draining the stream.
func (m *UI) handleShellStream(msg shellStreamMsg) []tea.Cmd {
	var cmds []tea.Cmd
	if item := m.chat.MessageItem(msg.PendingID); item != nil {
		if shellItem, ok := item.(*chat.ShellItem); ok {
			shellItem.AppendOutput(msg.Chunk)
			m.chat.ScrollToBottom()
		}
	}
	// Continue draining the stream channel.
	if msg.streamCh != nil {
		ch := msg.streamCh
		pid := msg.PendingID
		cmds = append(cmds, func() tea.Msg {
			chunk, ok := <-ch
			if !ok {
				return nil
			}
			return shellStreamMsg{PendingID: pid, Chunk: chunk, streamCh: ch}
		})
	}
	return cmds
}

// handleShellResult completes a bang command's item, or adds one.
func (m *UI) handleShellResult(msg shellResultMsg) []tea.Cmd {
	var cmds []tea.Cmd
	// Clear the bang cancel func — command is done.
	if m.bangCancel != nil {
		m.bangCancel()
		m.bangCancel = nil
	}
	// Complete the pending shell item if it exists, otherwise create a new one.
	completed := false
	if msg.PendingID != "" {
		if item := m.chat.MessageItem(msg.PendingID); item != nil {
			if shellItem, ok := item.(*chat.ShellItem); ok {
				shellItem.Complete(msg.Output, msg.ExitCode)
				m.chat.ScrollToBottom()
				completed = true
			}
		}
	}
	if !completed {
		item := chat.NewShellItem(m.com.Styles, msg.Command, msg.Output, msg.ExitCode)
		m.chat.AppendMessages(item)
		m.chat.ScrollToBottom()
	}
	cmds = append(cmds, m.loadPromptHistory())
	return cmds
}
