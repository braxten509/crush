package model

import (
	"bytes"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/crush/internal/ui/dialog/questionform"
	"github.com/charmbracelet/crush/internal/ui/dialog/skillbrowser"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/selfupdate"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/completions"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	fimage "github.com/charmbracelet/crush/internal/ui/image"
	"github.com/charmbracelet/crush/internal/ui/sessionhost"
	"github.com/charmbracelet/crush/internal/ui/util"
	uv "github.com/charmbracelet/ultraviolet"
	xstrings "github.com/charmbracelet/x/exp/strings"
)

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

	case skillbrowser.SkillsResultMsg:
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
	case workDoneMsg:
		cmds = append(cmds, m.applyWorkDone(msg))
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
	case memoryCheckMsg:
		cmds = append(cmds, m.handleMemoryCheck(msg))
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
