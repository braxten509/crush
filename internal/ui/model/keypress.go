package model

import (
	"strings"
	"unicode"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/completions"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// inputOwner is the part of the UI that gets keyboard input first. The
// values are in precedence order: an owner only gets input none of the ones
// before it took.
type inputOwner uint8

const (
	// ownerSecureEntry is a secure entry that holds a secret but does not
	// have the keys (a dialog covers it, or the chat is focused): clipboard
	// input is dropped so a secret can't land anywhere else, and the rest
	// goes on to the next owner.
	ownerSecureEntry inputOwner = iota
	// ownerSubagentView is the sub-agent transcript shown over the chat.
	ownerSubagentView
	ownerDialog
	// ownerInline is an inline editor that replaced the textarea and has
	// the keys.
	ownerInline
	// ownerTaskRow is the background task row under the editor, selected
	// with Down.
	ownerTaskRow
	// ownerNone is onboarding, where only the quit key works and the
	// dialogs take everything else.
	ownerNone
	// ownerInitialize is the project initialization prompt.
	ownerInitialize
	// ownerCompletions is the @ mention popup over the editor.
	ownerCompletions
	ownerEditor
	ownerChat
	ownerSidebar
	// ownerGlobal is a chat or landing page where nothing has focus: only
	// the global keys work.
	ownerGlobal
)

// inputOwner returns the owner of the input arriving now.
func (m *UI) inputOwner() inputOwner {
	if m.secureEntryCovered() {
		return ownerSecureEntry
	}
	if v := m.subagentView; v != nil && m.activeInline == nil && m.currentSessionID() == v.task.SessionID && !m.dialog.HasDialogs() {
		return ownerSubagentView
	}
	return m.paneOwner()
}

// paneOwner returns the owner of the input among the dialogs and panes of
// the main window, below the secure entry and the sub-agent view.
func (m *UI) paneOwner() inputOwner {
	switch {
	case m.dialog.HasDialogs():
		return ownerDialog
	case m.activeInline != nil && m.focus == uiFocusEditor:
		return ownerInline
	case m.taskRow.focused && m.focus == uiFocusEditor:
		return ownerTaskRow
	case m.state == uiOnboarding:
		return ownerNone
	case m.state == uiInitialize:
		return ownerInitialize
	}
	switch m.focus {
	case uiFocusEditor:
		if m.mention.open {
			return ownerCompletions
		}
		return ownerEditor
	case uiFocusMain:
		return ownerChat
	case uiFocusSidebar:
		return ownerSidebar
	}
	return ownerGlobal
}

// secureEntryCovered reports whether a secure entry holds a secret while
// something else has the keys.
func (m *UI) secureEntryCovered() bool {
	form, ok := m.activeInline.(*dialog.QuestionForm)
	return ok && form.HasSecureEntry() && form.SecureEntryActive() && (m.focus != uiFocusEditor || m.dialog.HasDialogs())
}

// handleKeyPressMsg routes a key press to its owner. A few keys come
// first: the quit key works everywhere but over the quit dialog, Tab
// toggles an inline editor's focus, and while the agent works, cancel and
// interrupt beat the editor and the chat.
func (m *UI) handleKeyPressMsg(msg tea.KeyPressMsg) tea.Cmd {
	if msg.IsRepeat && key.Matches(msg, m.keyMap.Chat.Interrupt) {
		return nil
	}
	if key.Matches(msg, m.keyMap.Quit) && !m.dialog.ContainsDialog(dialog.QuitID) {
		return m.openQuitDialog()
	}

	owner := m.paneOwner()
	if owner == ownerDialog {
		return m.handleDialogMsg(msg)
	}
	// Tab toggles focus between an inline editor and the chat, which
	// lets users collapse the question form to view chat.
	if m.activeInline != nil && key.Matches(msg, m.keyMap.Tab) {
		m.toggleInlineFocus()
		return nil
	}
	switch owner {
	case ownerInline:
		return m.handleInlineKey(msg)
	case ownerTaskRow:
		cmd, ok := m.handleTaskRowKey(msg)
		if ok {
			return cmd
		}
		// The row let the key go and gave the editor its keys back.
		owner = m.paneOwner()
	}
	if cmd, ok := m.handleRunKey(msg); ok {
		return cmd
	}

	switch owner {
	case ownerNone:
		return nil
	case ownerInitialize:
		return tea.Batch(m.updateInitializeView(msg)...)
	case ownerCompletions:
		if cmd, ok := m.handleCompletionsKey(msg); ok {
			return cmd
		}
		return m.handleEditorKey(msg)
	case ownerEditor:
		return m.handleEditorKey(msg)
	case ownerChat:
		return m.handleChatKey(msg)
	case ownerSidebar:
		return m.handleSidebarKey(msg)
	}
	cmd, _ := m.handleGlobalKey(msg)
	return cmd
}

// toggleInlineFocus moves focus between an inline editor and the chat.
// With composer_focus_only it does nothing.
func (m *UI) toggleInlineFocus() {
	if m.composerFocusOnly() {
		return
	}
	if m.focus == uiFocusEditor {
		m.focusActiveInline(uiFocusMain)
	} else {
		m.focusActiveInline(uiFocusEditor)
	}
}

// handleInlineKey gives a key to the inline editor that replaced the
// textarea, restoring the textarea once the editor is done.
func (m *UI) handleInlineKey(msg tea.KeyPressMsg) tea.Cmd {
	done, cmd := m.activeInline.HandleKey(msg)
	if !done {
		if m.activeInline.HeightChanged() {
			m.updateLayoutAndSize()
		}
		return cmd
	}
	prev := m.activeInline
	m.activeInline = nil
	m.textarea.Focus()
	m.updateLayoutAndSize()
	if cmd != nil {
		return cmd
	}
	if cod, ok := prev.(dialog.CmdOnDone); ok {
		return cod.PendingCmd()
	}
	return nil
}

// handleRunKey handles the keys that act on the running agent: cancel,
// unless esc is closing something in the editor first (completions,
// attachment delete, history), and interrupt, which stops the active run
// and hands off the queued batch.
func (m *UI) handleRunKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if key.Matches(msg, m.keyMap.Chat.Cancel) && !m.editorWantsEsc() && m.isAgentBusy() {
		return m.cancelAgent(), true
	}
	if key.Matches(msg, m.keyMap.Chat.Interrupt) && (m.isAgentBusy() || m.interruptInFlight[m.currentSessionID()]) {
		return m.interruptAgent(), true
	}
	return nil, false
}

// handleCompletionsKey gives a key to the open @ mention popup. ok is false
// when the popup let the key go.
func (m *UI) handleCompletionsKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	selection, ok := m.mention.popup.Update(msg)
	if !ok {
		return nil, false
	}
	var cmd tea.Cmd
	switch selection := selection.(type) {
	case completions.SelectionMsg[completions.FileCompletionValue]:
		cmd = m.insertFileCompletion(selection.Value.Path)
		if !selection.KeepOpen {
			m.mention.close()
		}
	case completions.SelectionMsg[completions.ResourceCompletionValue]:
		cmd = m.insertMCPResourceCompletion(selection.Value)
		if !selection.KeepOpen {
			m.mention.close()
		}
	case completions.ClosedMsg:
		m.mention.open = false
	}
	return cmd, true
}

// handleEditorKey handles a key for the focused editor: attachment
// deletion first, then the editor's own keys, the global keys, and
// otherwise typing.
func (m *UI) handleEditorKey(msg tea.KeyPressMsg) tea.Cmd {
	if ok := m.attachments.Update(msg); ok {
		return m.turnOffRemovedKeptSkills()
	}

	var cmds []tea.Cmd
	switch {
	case key.Matches(msg, m.keyMap.ShiftTab):
		cmds = append(cmds, m.toggleInputMode())
	case key.Matches(msg, m.keyMap.Editor.AddImage):
		if m.currentModelSupportsImages() {
			cmds = append(cmds, m.openFilesDialog())
		}
	case key.Matches(msg, m.keyMap.Editor.PasteImage):
		if m.currentModelSupportsImages() {
			cmds = append(cmds, m.pasteImageFromClipboard())
		}
	case key.Matches(msg, m.keyMap.Editor.PasteText):
		cmds = append(cmds, m.pasteTextFromClipboard)
	case key.Matches(msg, m.keyMap.Editor.SendMessage):
		cmd, done := m.submitEditor()
		if done {
			return cmd
		}
		cmds = append(cmds, cmd)
	case key.Matches(msg, m.keyMap.Chat.NewSession):
		cmds = append(cmds, m.newSessionFromKey())
	case key.Matches(msg, m.keyMap.Tab):
		if m.state != uiLanding && !m.composerFocusOnly() {
			m.setState(m.state, uiFocusMain)
			m.setFocus(uiFocusMain)
			m.chat.SetSelected(m.chat.Len() - 1)
		}
	case key.Matches(msg, m.keyMap.Editor.OpenEditor):
		cmds = append(cmds, m.openExternalEditor())
	case key.Matches(msg, m.keyMap.Editor.Newline):
		prevHeight := m.textarea.Height()
		m.textarea.InsertRune('\n')
		m.mention.close()
		cmds = append(cmds, m.updateTextareaWithPrevHeight(msg, prevHeight))
	case key.Matches(msg, m.keyMap.Editor.CopySelection):
		if m.textarea.HasSelection() {
			cmds = append(cmds, common.CopyToClipboardWithCallback(
				m.textarea.SelectedText(),
				"Selection copied to clipboard",
				nil,
			))
			m.textarea.ClearSelection()
		}
	case key.Matches(msg, m.keyMap.Editor.CutSelection):
		if m.textarea.HasSelection() {
			cmds = append(cmds, common.CopyToClipboardWithCallback(
				m.textarea.SelectedText(),
				"Selection cut to clipboard",
				nil,
			))
			m.textarea.DeleteSelection()
		}
	case key.Matches(msg, m.keyMap.Editor.HistoryPrev):
		cmds = append(cmds, m.handleHistoryUp(msg))
	case key.Matches(msg, m.keyMap.Editor.HistoryNext):
		// Past the end of the editor, Down selects the background row.
		if m.promptHistory.index < 0 && m.isAtEditorEnd() && m.focusTaskRow() {
			break
		}
		cmds = append(cmds, m.handleHistoryDown(msg))
	case key.Matches(msg, m.keyMap.Editor.Escape):
		cmds = append(cmds, m.handleHistoryEscape(msg))
	case key.Matches(msg, m.keyMap.Editor.Commands) && m.textarea.Value() == "":
		cmds = append(cmds, m.openCommandsDialog())
	default:
		// Handle global keys first before passing to textarea.
		if cmd, ok := m.handleGlobalKey(msg); ok {
			cmds = append(cmds, cmd)
			break
		}
		cmds = append(cmds, m.typeInEditor(msg)...)
	}
	return tea.Sequence(cmds...)
}

// submitEditor handles the send key: a trailing backslash continues the
// prompt on a new line, "exit" and "quit" ask to quit, and anything else is
// sent, to the agent or, in bang mode, to the shell. done reports that cmd
// is all there is to do.
func (m *UI) submitEditor() (cmd tea.Cmd, done bool) {
	if m.modeSwitching {
		return util.ReportInfo("Switching input mode, one moment..."), false
	}
	prevHeight := m.textarea.Height()
	value := m.textarea.Value()
	if before, ok := strings.CutSuffix(value, "\\"); ok {
		// If the last character is a backslash, remove it and add a newline.
		m.textarea.SetValue(before)
		return m.handleTextareaHeightChange(prevHeight), false
	}

	// Otherwise, send the message. The editor is empty either way, so only
	// the layout follows its height; nothing else is left to run.
	m.textarea.Reset()
	m.handleTextareaHeightChange(prevHeight)

	value = strings.TrimSpace(value)
	if value == "exit" || value == "quit" {
		return m.openQuitDialog(), true
	}

	if m.bangMode && value != "" {
		m.bangMode = false
		m.setEditorPrompt(m.yoloModeCached())
		m.randomizePlaceholders()
		m.historyReset()
		return tea.Batch(m.runShellCommand(value)), true
	}

	attachments := m.attachments.List()
	m.attachments.Reset()
	if len(value) == 0 && len(attachments) == 0 {
		// Enter on an empty editor while a ready plan is pending
		// reopens the dismissed handoff prompt.
		if m.mode == uiInputModePlan && m.hasSession() && m.planReadySessionID == m.session.ID {
			m.openPlanHandoff()
		}
		return nil, true
	}

	m.randomizePlaceholders()
	m.historyReset()
	return tea.Batch(m.sendMessage(value, attachments...), m.loadPromptHistory()), true
}

// openExternalEditor opens the prompt in the user's editor, unless the
// agent is working.
func (m *UI) openExternalEditor() tea.Cmd {
	if m.isAgentBusy() {
		return util.ReportWarn("Agent is working, please wait...")
	}
	editorValue := m.textarea.Value()
	if m.bangMode {
		editorValue = "!" + editorValue
	}
	return m.openEditor(editorValue)
}

// typeInEditor passes a key to the textarea, keeping bang mode, the @
// mention popup, the details panel and the history draft in step.
func (m *UI) typeInEditor(msg tea.KeyPressMsg) []tea.Cmd {
	// Bang mode: backspace on already-empty prompt exits.
	if m.bangMode && m.bangWasEmpty && msg.Code == tea.KeyBackspace {
		m.bangMode = false
		m.bangWasEmpty = false
		m.setEditorPrompt(m.yoloModeCached())
		return nil
	}

	var cmds []tea.Cmd
	// Check for @ trigger before passing to textarea.
	curValue := m.textarea.Value()
	curIdx := len(curValue)

	// Trigger completions on @.
	if m.mention.triggers(msg.String(), curValue) {
		depth, limit := m.com.Config().Options.TUI.Completions.Limits()
		cmds = append(cmds, m.mention.begin(curIdx, m.completionsPosition(), depth, limit))
	}

	// remove the details if they are open when user starts typing
	if m.detailsOpen {
		m.detailsOpen = false
		m.updateLayoutAndSize()
	}

	prevHeight := m.textarea.Height()
	cmds = append(cmds, m.updateTextareaWithPrevHeight(msg, prevHeight))

	m.syncBangModeAfterTyping(curValue)

	// Any text modification becomes the current draft.
	m.updateHistoryDraft(curValue)

	// After updating textarea, check if we need to filter completions.
	// Skip filtering on the initial @ keystroke since items are loading async.
	if m.mention.open && msg.String() != "@" {
		m.mention.follow(msg.String(), m.textarea.Value(), m.textareaWord())
	}
	return cmds
}

// syncBangModeAfterTyping enters bang mode when "!" was typed at the start
// of the prompt, optionally preceded by whitespace (either on an
// empty/whitespace-only prompt or prepended to existing text), and tracks
// whether the bang prompt became empty. curValue is the text before the
// keystroke.
func (m *UI) syncBangModeAfterTyping(curValue string) {
	newVal := m.textarea.Value()
	trimmedNew := strings.TrimLeftFunc(newVal, unicode.IsSpace)
	trimmedCur := strings.TrimLeftFunc(curValue, unicode.IsSpace)
	switch {
	case !m.bangMode && strings.HasPrefix(trimmedNew, "!") && !strings.HasPrefix(trimmedCur, "!"):
		m.bangMode = true
		m.bangWasEmpty = len(strings.TrimSpace(curValue)) == 0
		// Strip leading whitespace and the "!" from the textarea while
		// preserving the cursor position relative to the command text. The
		// cursor line doesn't change.
		col := m.textarea.Column()
		stripped := trimmedNew[1:]
		m.textarea.SetValue(stripped)
		m.textarea.SetCursorColumn(max(0, col-(len(newVal)-len(stripped))))
		m.setEditorPrompt(m.yoloModeCached())
	case m.bangMode && newVal == "" && curValue != "":
		// Just cleared last character; mark empty, stay in bang mode.
		m.bangWasEmpty = true
	case m.bangMode && newVal != "":
		m.bangWasEmpty = false
	}
}

// chatNavKey is a key that moves through the chat. scrollOnly marks moves
// that change nothing but the scroll position, so a memoized frame may
// serve them.
type chatNavKey struct {
	binding    func(*KeyMap) key.Binding
	scrollOnly bool
	move       func(*Chat)
}

// chatNavKeys are the chat's navigation keys, in matching order.
var chatNavKeys = []chatNavKey{
	{func(k *KeyMap) key.Binding { return k.Chat.Up }, true, func(c *Chat) {
		c.ScrollBy(-1)
		if !c.SelectedItemInView() {
			c.SelectPrev()
			c.ScrollToSelected()
		}
	}},
	{func(k *KeyMap) key.Binding { return k.Chat.Down }, true, func(c *Chat) {
		c.ScrollBy(1)
		if !c.SelectedItemInView() {
			c.SelectNext()
			c.ScrollToSelected()
		}
	}},
	{func(k *KeyMap) key.Binding { return k.Chat.UpOneItem }, false, func(c *Chat) {
		c.SelectPrev()
		c.ScrollToSelected()
	}},
	{func(k *KeyMap) key.Binding { return k.Chat.DownOneItem }, false, func(c *Chat) {
		c.SelectNext()
		c.ScrollToSelected()
	}},
	{func(k *KeyMap) key.Binding { return k.Chat.HalfPageUp }, true, func(c *Chat) {
		c.ScrollBy(-c.Height() / 2)
		c.SelectFirstInView()
	}},
	{func(k *KeyMap) key.Binding { return k.Chat.HalfPageDown }, true, func(c *Chat) {
		c.ScrollBy(c.Height() / 2)
		c.SelectLastInView()
	}},
	{func(k *KeyMap) key.Binding { return k.Chat.PageUp }, true, func(c *Chat) {
		c.ScrollBy(-c.Height())
		c.SelectFirstInView()
	}},
	{func(k *KeyMap) key.Binding { return k.Chat.PageDown }, true, func(c *Chat) {
		c.ScrollBy(c.Height())
		c.SelectLastInView()
	}},
	{func(k *KeyMap) key.Binding { return k.Chat.Home }, false, func(c *Chat) {
		c.ScrollToTop()
		c.SelectFirst()
	}},
	{func(k *KeyMap) key.Binding { return k.Chat.End }, false, func(c *Chat) {
		c.ScrollToBottomAndSelectLast()
	}},
}

// handleChatKey handles a key for the focused chat: focus and mode keys,
// navigation, then the selected item's own keys and the global keys.
func (m *UI) handleChatKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keyMap.ShiftTab):
		return m.toggleInputMode()
	case key.Matches(msg, m.keyMap.Tab):
		m.sidebar.scrollbarVisible = false
		return m.setFocus(uiFocusEditor)
	case key.Matches(msg, m.keyMap.Chat.FocusSidebar):
		if m.state == uiChat && !m.isCompact && m.hasSession() && m.sidebar.scrollable {
			m.setFocus(uiFocusSidebar, keepTextareaFocus)
		}
		return nil
	case key.Matches(msg, m.keyMap.Chat.NewSession):
		return m.newSessionFromKey()
	case key.Matches(msg, m.keyMap.Chat.Expand):
		m.chat.ToggleExpandedSelectedItem()
		return nil
	}
	for _, nav := range chatNavKeys {
		if key.Matches(msg, nav.binding(&m.keyMap)) {
			if nav.scrollOnly {
				m.frame.markScrollOnly()
			}
			nav.move(m.chat)
			return nil
		}
	}
	if ok, cmd := m.chat.HandleKeyMsg(msg); ok {
		return cmd
	}
	cmd, _ := m.handleGlobalKey(msg)
	return cmd
}

// handleSidebarKey handles a key for the focused sidebar: scrolling, focus
// keys and the global keys.
func (m *UI) handleSidebarKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.state != uiChat || m.isCompact || !m.hasSession() {
		return nil
	}
	switch {
	case key.Matches(msg, m.keyMap.Chat.Up):
		m.sidebar.scrollUp()
	case key.Matches(msg, m.keyMap.Chat.Down):
		m.sidebar.scrollDown()
	case key.Matches(msg, m.keyMap.Chat.Home):
		m.sidebar.scrollToTop()
	case key.Matches(msg, m.keyMap.Chat.End):
		m.sidebar.scrollToBottom()
	case key.Matches(msg, m.keyMap.Chat.FocusChat):
		m.sidebar.scrollbarVisible = false
		m.setFocus(uiFocusMain, keepTextareaFocus)
	case key.Matches(msg, m.keyMap.Tab):
		m.sidebar.scrollbarVisible = false
		return m.setFocus(uiFocusEditor)
	default:
		cmd, _ := m.handleGlobalKey(msg)
		return cmd
	}
	return nil
}

// handleGlobalKey handles the keys that work whichever pane has focus. ok
// is false when msg is none of them, or one that does nothing here.
func (m *UI) handleGlobalKey(msg tea.KeyPressMsg) (cmd tea.Cmd, ok bool) {
	switch {
	case key.Matches(msg, m.keyMap.Help):
		m.status.ToggleHelp()
		m.updateLayoutAndSize()
		return nil, true
	case key.Matches(msg, m.keyMap.Commands):
		return m.openCommandsDialog(), true
	case key.Matches(msg, m.keyMap.Models):
		return m.openModelsDialog(), true
	case key.Matches(msg, m.keyMap.Sessions):
		return m.openSessionsDialog(), true
	case key.Matches(msg, m.keyMap.Chat.Details) && m.isCompact:
		m.detailsOpen = !m.detailsOpen
		m.updateLayoutAndSize()
		return nil, true
	case key.Matches(msg, m.keyMap.Chat.Background):
		if m.state != uiChat {
			return nil, false
		}
		if m.taskRow.focused {
			return m.openTaskRowDialog(), true
		}
		return m.openBackgroundDialog(), true
	case key.Matches(msg, m.keyMap.Chat.BackgroundCommand):
		// While a command is running, Ctrl+B moves it to the
		// background, as in Claude Code.
		if m.hasSession() {
			if g := m.chat.BackgroundableGroup(); g != nil && m.com.Workspace.AgentBackground(m.session.ID) {
				g.Backgrounded()
			}
		}
		return nil, true
	case key.Matches(msg, m.keyMap.Chat.EndFollow):
		if m.state != uiChat || !m.hasSession() {
			return nil, false
		}
		return m.chat.ScrollToBottomAndSelectLast(), true
	case key.Matches(msg, m.keyMap.Chat.TogglePills):
		if m.state != uiChat || !m.hasSession() {
			return nil, false
		}
		return m.togglePillsExpanded(), true
	case key.Matches(msg, m.keyMap.Chat.PillLeft):
		if !m.canSwitchPillSection() {
			return nil, false
		}
		return m.switchPillSection(-1), true
	case key.Matches(msg, m.keyMap.Chat.PillRight):
		if !m.canSwitchPillSection() {
			return nil, false
		}
		return m.switchPillSection(1), true
	case key.Matches(msg, m.keyMap.Suspend):
		if m.isAgentBusy() {
			return util.ReportWarn("Agent is busy, please wait..."), true
		}
		return tea.Suspend, true
	case key.Matches(msg, m.keyMap.ToggleYolo):
		if m.mode == uiInputModePlan {
			// YOLO has no meaning while planning; activating it
			// switches straight to YOLO coding.
			return m.switchPlanToYolo(), true
		}
		if m.toggleYoloMode() {
			return util.CmdHandler(util.InfoMsg{Type: util.InfoTypeYolo, Msg: yoloModeBannerMsg}), true
		}
		return util.ReportInfo("Yolo mode disabled"), true
	}
	return nil, false
}

// canSwitchPillSection reports whether the pill section keys work: in a
// chat with the pills expanded, from outside the editor.
func (m *UI) canSwitchPillSection() bool {
	return m.state == uiChat && m.hasSession() && m.pillsExpanded && m.focus != uiFocusEditor
}
