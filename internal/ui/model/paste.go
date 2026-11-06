package model

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/clipboard"
	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// If pasted text has more than 10 newlines, treat it as a file attachment.
const pasteLinesThreshold = 10

// If pasted text has more than 1000 columns, treat it as a file attachment.
const pasteColsThreshold = 1000

type pastedTextAttachmentMsg struct {
	attachment message.Attachment
	text       string
}

// mimeOf detects the MIME type of the given content.
func mimeOf(content []byte) string {
	mimeBufferSize := min(512, len(content))
	return http.DetectContentType(content[:mimeBufferSize])
}

// checkBangModeAfterPaste engages bang mode when pasted text starts with
// optional whitespace followed by "!". It strips the prefix and adjusts
// the cursor, mirroring the keypress bang-mode entry logic.
func (m *UI) checkBangModeAfterPaste() {
	if m.bangMode {
		return
	}
	val := m.textarea.Value()
	trimmed := strings.TrimLeftFunc(val, unicode.IsSpace)
	if !strings.HasPrefix(trimmed, "!") {
		return
	}
	m.bangMode = true
	m.bangWasEmpty = true
	stripped := trimmed[1:]
	m.textarea.SetValue(stripped)
	col := m.textarea.Column()
	m.textarea.SetCursorColumn(max(0, col-(len(val)-len(stripped))))
	m.setEditorPrompt(m.permModeCached())
}

// handlePasteMsg handles a paste message.
func (m *UI) handlePasteMsg(msg tea.PasteMsg) tea.Cmd {
	// Normalize \r\n before the textarea sanitizer sees it.
	msg.Content = strings.ReplaceAll(msg.Content, "\r\n", "\n")

	if m.dialog.HasDialogs() {
		return m.handleDialogMsg(msg)
	}

	if m.focus != uiFocusEditor {
		return nil
	}

	if hasPasteExceededThreshold(msg) && !m.attachmentsLimitReached() && int64(len(msg.Content)) <= common.MaxAttachmentSize {
		// Number the paste now: the command runs off the Update goroutine.
		idx := m.pasteIdx()
		return func() tea.Msg {
			content := []byte(msg.Content)
			name := fmt.Sprintf("paste_%d.txt", idx)
			mimeBufferSize := min(512, len(content))
			mimeType := http.DetectContentType(content[:mimeBufferSize])
			return pastedTextAttachmentMsg{attachment: message.Attachment{
				FileName: name,
				FilePath: name,
				MimeType: mimeType,
				Content:  content,
			}, text: msg.Content}
		}
	}

	// Attempt to parse pasted content as file paths. If possible to parse,
	// all files exist and are valid, add as attachments.
	// Otherwise, paste as text.
	paths := fsext.ParsePastedFiles(msg.Content)
	allExistsAndValid := func() bool {
		if len(paths) == 0 {
			return false
		}
		for _, path := range paths {
			if _, err := os.Stat(path); os.IsNotExist(err) {
				return false
			}
			if !common.IsImagePath(path) && !common.IsAudioPath(path) {
				return false
			}
		}
		return true
	}
	if !allExistsAndValid() {
		prevHeight := m.textarea.Height()
		cmd := m.updateTextareaWithPrevHeight(msg, prevHeight)
		m.checkBangModeAfterPaste()
		return cmd
	}
	if !m.currentModelSupportsImages() && slices.ContainsFunc(paths, common.IsImagePath) {
		return util.ReportWarn("The current model does not support image attachments")
	}
	if !m.currentModelSupportsAudio() && slices.ContainsFunc(paths, common.IsAudioPath) {
		return util.ReportWarn("The current model does not support audio attachments")
	}

	// A picture over the attachment limit goes in as its path instead
	// (owner, 2026-11-04), so the agent can read it from disk.
	var cmds []tea.Cmd
	var big []string
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Size() > common.MaxAttachmentSize {
			big = append(big, escapePastedPath(path))
			continue
		}
		cmds = append(cmds, m.handleFilePathPaste(path))
	}
	if len(big) > 0 {
		prevHeight := m.textarea.Height()
		cmds = append(cmds, m.updateTextareaWithPrevHeight(tea.PasteMsg{Content: strings.Join(big, " ")}, prevHeight))
	}
	return tea.Batch(cmds...)
}

// escapePastedPath escapes a path's backslashes and spaces, as a terminal
// pastes a dropped file, so fsext.ParsePastedFiles reads it back.
func escapePastedPath(path string) string {
	return strings.NewReplacer(`\`, `\\`, " ", `\ `).Replace(path)
}

func hasPasteExceededThreshold(msg tea.PasteMsg) bool {
	var (
		lineCount = 0
		colCount  = 0
	)
	for line := range strings.SplitSeq(msg.Content, "\n") {
		lineCount++
		colCount = max(colCount, len(line))

		if lineCount > pasteLinesThreshold || colCount > pasteColsThreshold {
			return true
		}
	}
	return false
}

// handleFilePathPaste handles a pasted file path.
func (m *UI) handleFilePathPaste(path string) tea.Cmd {
	return func() tea.Msg {
		fileInfo, err := os.Stat(path)
		if err != nil {
			return util.ReportError(err)()
		}
		if fileInfo.IsDir() {
			return util.ReportWarn("Cannot attach a directory")()
		}
		if fileInfo.Size() > common.MaxAttachmentSize {
			return util.ReportWarn("File is too big (>5mb)")()
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return util.ReportError(err)()
		}

		mimeBufferSize := min(512, len(content))
		mimeType := http.DetectContentType(content[:mimeBufferSize])
		fileName := filepath.Base(path)
		return message.Attachment{
			FilePath: path,
			FileName: fileName,
			MimeType: mimeType,
			Content:  content,
		}
	}
}

// pasteTextFromClipboard reads text from the system clipboard and returns a
// tea.PasteMsg so it flows through the same paste logic as bracketed paste.
func (m *UI) pasteTextFromClipboard() tea.Msg {
	textData, err := clipboard.Read(clipboard.FormatText)
	if err != nil || len(textData) == 0 {
		return util.InfoMsg{
			Type: util.InfoTypeError,
			Msg:  "Clipboard is empty or does not contain text",
		}
	}
	return tea.PasteMsg{Content: string(textData)}
}

// pasteImageFromClipboard returns a command that reads image data from the
// system clipboard and creates an attachment. If no image data is found, it
// falls back to interpreting clipboard text as a file path. What the command
// needs from the model is captured now, as it runs off the Update goroutine.
func (m *UI) pasteImageFromClipboard() tea.Cmd {
	supportsImages, idx := m.currentModelSupportsImages(), m.pasteIdx()
	return func() tea.Msg {
		return pasteImageFromClipboard(supportsImages, idx)
	}
}

// pasteImageFromClipboard does the work of [UI.pasteImageFromClipboard];
// idx numbers a pasted image.
func pasteImageFromClipboard(supportsImages bool, idx int) tea.Msg {
	if !supportsImages {
		return util.NewWarnMsg("The current model does not support image attachments")
	}
	imageData, err := clipboard.Read(clipboard.FormatImage)
	if int64(len(imageData)) > common.MaxAttachmentSize {
		return util.InfoMsg{
			Type: util.InfoTypeError,
			Msg:  "File too large, max 5MB",
		}
	}
	name := fmt.Sprintf("paste_%d.png", idx)
	if err == nil {
		return message.Attachment{
			FilePath: name,
			FileName: name,
			MimeType: mimeOf(imageData),
			Content:  imageData,
		}
	}

	textData, textErr := clipboard.Read(clipboard.FormatText)
	if textErr != nil || len(textData) == 0 {
		return nil // Clipboard is empty or does not contain an image
	}

	path := strings.TrimSpace(string(textData))
	path = strings.ReplaceAll(path, "\\ ", " ")
	if _, statErr := os.Stat(path); statErr != nil {
		return nil // Clipboard does not contain an image or valid file path
	}

	if !common.IsImagePath(path) {
		return util.NewInfoMsg("File type is not a supported image format")
	}

	fileInfo, statErr := os.Stat(path)
	if statErr != nil {
		return util.InfoMsg{
			Type: util.InfoTypeError,
			Msg:  fmt.Sprintf("Unable to read file: %v", statErr),
		}
	}
	if fileInfo.Size() > common.MaxAttachmentSize {
		return util.InfoMsg{
			Type: util.InfoTypeError,
			Msg:  "File too large, max 5MB",
		}
	}

	content, readErr := os.ReadFile(path)
	if readErr != nil {
		return util.InfoMsg{
			Type: util.InfoTypeError,
			Msg:  fmt.Sprintf("Unable to read file: %v", readErr),
		}
	}

	return message.Attachment{
		FilePath: path,
		FileName: filepath.Base(path),
		MimeType: mimeOf(content),
		Content:  content,
	}
}

var pasteRE = regexp.MustCompile(`paste_(\d+).txt`)

func (m *UI) pasteIdx() int {
	result := 0
	for _, at := range m.attachments.List() {
		found := pasteRE.FindStringSubmatch(at.FileName)
		if len(found) == 0 {
			continue
		}
		idx, err := strconv.Atoi(found[1])
		if err == nil {
			result = max(result, idx)
		}
	}
	return result + 1
}
