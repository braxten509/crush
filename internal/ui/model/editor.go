package model

import (
	"cmp"
	"context"
	"image"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/textarea"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/completions"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/editor"
)

func (m *UI) handleClickFocus(msg tea.MouseClickMsg) (cmd tea.Cmd) {
	switch {
	case m.state != uiChat:
		return nil
	case m.focus != uiFocusSidebar && image.Pt(msg.X, msg.Y).In(m.layout.sidebar) && m.sidebar.scrollable && !m.composerFocusOnly():
		m.setFocus(uiFocusSidebar)
		return nil
	case m.focus != uiFocusEditor && image.Pt(msg.X, msg.Y).In(m.layout.editor):
		if m.activeInline != nil {
			m.focusActiveInline(uiFocusEditor)
		} else {
			cmd = m.setFocus(uiFocusEditor)
		}
		m.sidebar.scrollbarVisible = false
	case m.focus != uiFocusMain && image.Pt(msg.X, msg.Y).In(m.layout.main) && !m.composerFocusOnly():
		// With composer_focus_only the chat still handles the click
		// (selection, expanding groups) without taking focus.
		if m.activeInline != nil {
			m.focusActiveInline(uiFocusMain)
		} else {
			m.setFocus(uiFocusMain)
		}
		m.sidebar.scrollbarVisible = false
	}
	return cmd
}

// composerFocusOnly reports whether the composer keeps keyboard focus
// (options.tui.composer_focus_only): Tab and clicks never move it away.
func (m *UI) composerFocusOnly() bool {
	if m.com == nil || m.com.Workspace == nil {
		return false
	}
	cfg := m.com.Config()
	return cfg != nil && cfg.Options != nil && cfg.Options.TUI.FocusesComposerOnly()
}

// focusActiveInline moves focus between an inline editor and the chat while
// preserving the editor and reconciling any collapsed layout.
func (m *UI) focusActiveInline(focus uiFocusState) {
	if m.activeInline == nil {
		return
	}

	m.setFocus(focus)
	switch focus {
	case uiFocusEditor:
		m.activeInline.SetFocused(true)
	case uiFocusMain:
		m.activeInline.SetFocused(false)
		if m.chat != nil {
			m.chat.SetSelected(m.chat.Len() - 1)
		}
	}

	if m.status != nil && m.chat != nil {
		m.updateLayoutAndSize()
	}
}

// closeDoneInline closes an inline editor a mouse click finished and
// returns the command it left to run, if any.
func (m *UI) closeDoneInline() tea.Cmd {
	prev := m.activeInline
	m.activeInline = nil
	m.textarea.Focus()
	m.updateLayoutAndSize()
	if cod, ok := prev.(dialog.CmdOnDone); ok {
		return cod.PendingCmd()
	}
	return nil
}

// forwardMouseToTextarea forwards a mouse event to the textarea with
// coordinates translated into the textarea's local space. It reports whether
// the event landed within the textarea's rendered region and was forwarded.
func (m *UI) forwardMouseToTextarea(msg tea.MouseMsg) (bool, tea.Cmd) {
	mouse := msg.Mouse()

	// The textarea is rendered inside layout.editor below the separator
	// and attachments rows, which renderEditorView always reserves.
	origin := image.Pt(m.layout.editor.Min.X, m.layout.editor.Min.Y+editorTextTop)

	// The textarea occupies its own height starting at the origin.
	area := image.Rectangle{Min: origin, Max: origin.Add(image.Pt(m.layout.editor.Dx(), m.textarea.Height()))}
	if !image.Pt(mouse.X, mouse.Y).In(area) {
		return false, nil
	}

	rel := tea.Mouse{
		X:      mouse.X - origin.X,
		Y:      mouse.Y - origin.Y,
		Button: mouse.Button,
		Mod:    mouse.Mod,
	}

	switch msg.(type) {
	case tea.MouseClickMsg:
		if rel.Button != uv.MouseLeft {
			return false, nil
		}
		m.textareaMouseSelecting = true
		m.textarea.BeginSelection(rel.X, rel.Y)
		return true, nil
	case tea.MouseMotionMsg:
		if !m.textareaMouseSelecting {
			return true, nil
		}
		m.textarea.ExtendSelection(rel.X, rel.Y)
		return true, nil
	case tea.MouseReleaseMsg:
		m.textarea.EndSelection()
		m.textareaMouseSelecting = false
		return true, nil
	default:
		return false, nil
	}
}

func (m *UI) openEditor(value string) tea.Cmd {
	tmpfile, err := os.CreateTemp("", "msg_*.md")
	if err != nil {
		return util.ReportError(err)
	}
	tmpPath := tmpfile.Name()
	defer tmpfile.Close() //nolint:errcheck
	if _, err := tmpfile.WriteString(value); err != nil {
		return util.ReportError(err)
	}
	cmd, err := editor.Command(
		"crush",
		tmpPath,
		editor.AtPosition(
			m.textarea.Line()+1,
			m.textarea.Column()+1,
		),
	)
	if err != nil {
		return util.ReportError(err)
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer func() {
			_ = os.Remove(tmpPath)
		}()

		if err != nil {
			return util.ReportError(err)()
		}
		return editorFileMsg(tmpPath)
	})
}

// promptWidth is the editor's prompt column: a mode mark and a space, so
// typed text lines up with the chat's text.
const promptWidth = 2

// editorFileMsg reads the file the external editor was supposed to write and
// turns it into the message that replaces the composer text. An emptied buffer
// yields an empty Text, which clears the composer just like deleting every
// character in-app does.
func editorFileMsg(path string) tea.Msg {
	content, err := os.ReadFile(path)
	if err != nil {
		return util.ReportError(err)()
	}
	return openEditorMsg{
		Text: strings.TrimSpace(string(content)),
	}
}

// setEditorPrompt configures the textarea prompt function based on whether
// plan, yolo, or bang mode is enabled.
func (m *UI) setEditorPrompt(yolo bool) {
	if m.bangMode {
		m.textarea.SetPromptFunc(promptWidth, m.bangPromptFunc)
		return
	}
	if m.mode == uiInputModePlan {
		m.textarea.SetPromptFunc(promptWidth, m.planPromptFunc)
		return
	}
	if yolo {
		m.textarea.SetPromptFunc(promptWidth, m.yoloPromptFunc)
		return
	}
	m.textarea.SetPromptFunc(promptWidth, m.normalPromptFunc)
}

// normalPromptFunc returns the normal editor prompt style ("›" on the
// first line, blank on subsequent lines).
func (m *UI) normalPromptFunc(info textarea.PromptInfo) string {
	t := m.com.Styles
	if info.LineNumber == 0 {
		if info.Focused {
			return t.Editor.PromptNormalIconFocused.Render()
		}
		return t.Editor.PromptNormalIconBlurred.Render()
	}
	if info.Focused {
		return t.Editor.PromptNormalFocused.Render()
	}
	return t.Editor.PromptNormalBlurred.Render()
}

// planPromptFunc marks planning with a badge beside the editor.
func (m *UI) planPromptFunc(info textarea.PromptInfo) string {
	t := m.com.Styles
	if info.LineNumber == 0 {
		if info.Focused {
			return t.Editor.PromptPlanIconFocused.Render()
		}
		return t.Editor.PromptPlanIconBlurred.Render()
	}
	if info.Focused {
		return t.Editor.PromptPlanDotsFocused.Render()
	}
	return t.Editor.PromptPlanDotsBlurred.Render()
}

// yoloPromptFunc returns the yolo mode editor prompt style with warning icon
// and colored dots.
func (m *UI) yoloPromptFunc(info textarea.PromptInfo) string {
	t := m.com.Styles
	if info.LineNumber == 0 {
		if info.Focused {
			return t.Editor.PromptYoloIconFocused.Render()
		} else {
			return t.Editor.PromptYoloIconBlurred.Render()
		}
	}
	if info.Focused {
		return t.Editor.PromptYoloDotsFocused.Render()
	}
	return t.Editor.PromptYoloDotsBlurred.Render()
}

// bangPromptFunc returns the bang mode editor prompt style with Turtle-colored
// icon and dots.
func (m *UI) bangPromptFunc(info textarea.PromptInfo) string {
	t := m.com.Styles
	if info.LineNumber == 0 {
		if info.Focused {
			return t.Editor.PromptBangIconFocused.Render()
		}
		return t.Editor.PromptBangIconBlurred.Render()
	}
	if info.Focused {
		return t.Editor.PromptBangDotsFocused.Render()
	}
	return t.Editor.PromptBangDotsBlurred.Render()
}

// insertCompletionText replaces the @query in the textarea with the given text.
// Returns false if the replacement cannot be performed.
func (m *UI) insertCompletionText(text string) bool {
	value := m.textarea.Value()
	if m.mention.startIndex > len(value) {
		return false
	}

	word := m.textareaWord()
	endIdx := min(m.mention.startIndex+len(word), len(value))
	newValue := value[:m.mention.startIndex] + text + value[endIdx:]
	m.textarea.SetValue(newValue)
	m.textarea.MoveToEnd()
	m.textarea.InsertRune(' ')
	return true
}

// insertFileCompletion inserts the selected file path into the textarea,
// replacing the @query, and adds the file as an attachment.
func (m *UI) insertFileCompletion(path string) tea.Cmd {
	prevHeight := m.textarea.Height()
	if !m.insertCompletionText(path) {
		return nil
	}
	heightCmd := m.handleTextareaHeightChange(prevHeight)

	// The command runs off the Update goroutine, so it captures what it
	// needs now; the read is recorded by applyFileCompletion.
	supportsImages := m.currentModelSupportsImages()
	ws := m.com.Workspace
	sessionID := m.currentSessionID()
	hasSession := m.hasSession()
	fileCmd := func() tea.Msg {
		if !supportsImages && common.IsImagePath(path) {
			return util.NewWarnMsg("The current model does not support image attachments")
		}

		absPath, _ := filepath.Abs(path)

		if hasSession {
			// Skip attachment if file was already read and hasn't been modified.
			lastRead := ws.FileTrackerLastReadTime(context.Background(), sessionID, absPath)
			if !lastRead.IsZero() {
				if info, err := os.Stat(path); err == nil && !info.ModTime().After(lastRead) {
					return nil
				}
			}
		}

		msg := fileCompletionMsg{absPath: absPath, noSession: !hasSession}
		// Add file as attachment.
		content, err := os.ReadFile(path)
		if err != nil {
			// If it fails, let the LLM handle it later.
			return msg
		}
		msg.attachment = &message.Attachment{
			FilePath: path,
			FileName: filepath.Base(path),
			MimeType: mimeOf(content),
			Content:  content,
		}
		return msg
	}
	return tea.Batch(heightCmd, fileCmd)
}

// fileCompletionMsg carries a file picked from the @ completions, read off
// the Update goroutine.
type fileCompletionMsg struct {
	absPath string
	// noSession: the file was picked before the chat existed, so reads are
	// tracked (and deduplicated) in sessionFileReads alone.
	noSession bool
	// attachment is nil when the file could not be read; the LLM handles
	// it later.
	attachment *message.Attachment
}

// applyFileCompletion records a completed file read and attaches the file.
// Runs on the Update goroutine.
func (m *UI) applyFileCompletion(msg fileCompletionMsg) tea.Cmd {
	if msg.noSession && slices.Contains(m.sessionFileReads, msg.absPath) {
		return nil
	}
	m.sessionFileReads = append(m.sessionFileReads, msg.absPath)
	if msg.attachment == nil || !m.attachments.Update(*msg.attachment) {
		return nil
	}
	m.frame.invalidate()
	return m.turnOffRemovedKeptSkills()
}

// insertMCPResourceCompletion inserts the selected resource into the textarea,
// replacing the @query, and adds the resource as an attachment.
func (m *UI) insertMCPResourceCompletion(item completions.ResourceCompletionValue) tea.Cmd {
	displayText := cmp.Or(item.Title, item.URI)

	prevHeight := m.textarea.Height()
	if !m.insertCompletionText(displayText) {
		return nil
	}
	heightCmd := m.handleTextareaHeightChange(prevHeight)

	resourceCmd := func() tea.Msg {
		contents, err := m.com.Workspace.ReadMCPResource(
			context.Background(),
			item.MCPName,
			item.URI,
		)
		if err != nil {
			slog.Warn("Failed to read MCP resource", "uri", item.URI, "error", err)
			return nil
		}
		if len(contents) == 0 {
			return nil
		}

		content := contents[0]
		var data []byte
		if content.Text != "" {
			data = []byte(content.Text)
		} else if len(content.Blob) > 0 {
			data = content.Blob
		}
		if len(data) == 0 {
			return nil
		}

		mimeType := item.MIMEType
		if mimeType == "" && content.MIMEType != "" {
			mimeType = content.MIMEType
		}
		if mimeType == "" {
			mimeType = "text/plain"
		}

		if !m.currentModelSupportsImages() && strings.HasPrefix(mimeType, "image/") {
			return util.NewWarnMsg("The current model does not support image attachments")
		}

		return message.Attachment{
			FilePath: item.URI,
			FileName: displayText,
			MimeType: mimeType,
			Content:  data,
		}
	}
	return tea.Batch(heightCmd, resourceCmd)
}

// completionsPosition returns the X and Y position for the completions popup.
func (m *UI) completionsPosition() image.Point {
	cur := m.textarea.Cursor()
	if cur == nil {
		return image.Point{
			X: m.layout.editor.Min.X,
			Y: m.layout.editor.Min.Y,
		}
	}
	return image.Point{
		X: cur.X + m.layout.editor.Min.X,
		Y: m.layout.editor.Min.Y + editorTextTop - 1 + cur.Y,
	}
}

// textareaWord returns the current word at the cursor position.
func (m *UI) textareaWord() string {
	return m.textarea.Word()
}

// isWhitespace returns true if the byte is a whitespace character.
func isWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

var readyPlaceholders = [...]string{
	"Ready!",
	"Ready...",
	"Ready?",
	"Ready for instructions",
}

var workingPlaceholders = [...]string{
	"Working!",
	"Working...",
	"Brrrrr...",
	"Prrrrrrrr...",
	"Processing...",
	"Thinking...",
}

// randomizePlaceholders selects random placeholder text for the textarea's
// ready and working states.
func (m *UI) randomizePlaceholders() {
	m.workingPlaceholder = workingPlaceholders[rand.Intn(len(workingPlaceholders))]
	m.readyPlaceholder = readyPlaceholders[rand.Intn(len(readyPlaceholders))]
}
