package model

import (
	"context"
	"log/slog"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/charmbracelet/crush/internal/ui/completions"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/charmbracelet/x/ansi"
)

// skillMentionPrefix starts a skill mention typed in the prompt, as in
// "$teach", the way Codex invokes skills.
const skillMentionPrefix = "$"

// skillMention is a $name in a prompt that names a skill.
type skillMention struct {
	// start and end are the byte offsets of "$name" in the text.
	start, end int
	entry      skills.CatalogEntry
}

// findSkillMentions returns the $name words in text that name one of the
// entries, in order. Any visible skill counts, as in Codex: the catalog
// already leaves out turned-off skills. A mention starts the text or follows
// whitespace; punctuation ending the word, like "." or ",", is not part of
// the name. Words naming no skill ($5, $HOME) are left out.
func findSkillMentions(text string, entries []skills.CatalogEntry) []skillMention {
	if len(entries) == 0 || !strings.Contains(text, skillMentionPrefix) {
		return nil
	}
	var mentions []skillMention
	for i := 0; i < len(text); i++ {
		if text[i] != skillMentionPrefix[0] || (i > 0 && !isWhitespace(text[i-1])) {
			continue
		}
		end := i + 1
		for end < len(text) && isSkillNameByte(text[end]) {
			end++
		}
		for end > i+1 && strings.ContainsRune(".:/-_", rune(text[end-1])) {
			end--
		}
		name := text[i+1 : end]
		for _, entry := range entries {
			if entry.Name == name && name != "" {
				mentions = append(mentions, skillMention{start: i, end: end, entry: entry})
				break
			}
		}
		i = end - 1
	}
	return mentions
}

// isSkillNameByte reports whether b can be part of a skill name: ASCII
// letters, digits and the separators skill names use.
func isSkillNameByte(b byte) bool {
	return b < unicode.MaxASCII && (b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.IndexByte("-_.:/", b) >= 0)
}

// skillCompletionValues lists the skills for the $ popup, each name once.
func skillCompletionValues(entries []skills.CatalogEntry) []completions.SkillCompletionValue {
	values := make([]completions.SkillCompletionValue, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if seen[entry.Name] {
			continue
		}
		seen[entry.Name] = true
		values = append(values, completions.SkillCompletionValue{Name: entry.Name})
	}
	return values
}

// highlightSkillMentions colors each $name naming a skill in the rendered
// editor rows with style. Rows keep their own styling around the names.
func highlightSkillMentions(rows []string, entries []skills.CatalogEntry, style lipgloss.Style) []string {
	for i, row := range rows {
		plain := ansi.Strip(row)
		mentions := findSkillMentions(plain, entries)
		if len(mentions) == 0 {
			continue
		}
		ranges := make([]lipgloss.Range, 0, len(mentions))
		for _, mention := range mentions {
			start := ansi.StringWidth(plain[:mention.start])
			end := start + ansi.StringWidth(plain[mention.start:mention.end])
			ranges = append(ranges, lipgloss.NewRange(start, end, style))
		}
		rows[i] = lipgloss.StyleRanges(row, ranges...)
	}
	return rows
}

// composerSkillHighlight colors the skill mentions in the editor's rendered
// rows, except in shell mode (where $ starts a variable) and while text is
// selected (so the selection stays visible).
func (m *UI) composerSkillHighlight(rows []string) []string {
	if m.bangMode || m.textarea.HasSelection() {
		return rows
	}
	return highlightSkillMentions(rows, m.skillCatalog, m.com.Styles.ComposerFooter.Skill)
}

// sendPrompt sends a prompt the user wrote, first adding each skill its
// $name words invoke, once, unless it is attached already.
func (m *UI) sendPrompt(text string, attachments []message.Attachment) tea.Cmd {
	var wanted []skills.CatalogEntry
	seen := make(map[string]bool)
	for _, mention := range findSkillMentions(text, m.skillCatalog) {
		name := mention.entry.Name
		if !seen[name] && !hasSkillAttachment(attachments, name) {
			wanted = append(wanted, mention.entry)
		}
		seen[name] = true
	}
	if len(wanted) == 0 {
		return m.sendMessage(text, attachments...)
	}
	// The skills are read off the Update goroutine.
	ws := m.com.Workspace
	return func() tea.Msg {
		for _, entry := range wanted {
			attachment, err := readSkillAttachment(ws, entry.ID, entry.Name)
			if err != nil {
				slog.Warn("Failed to read invoked skill", "skill", entry.Name, "error", err)
				continue
			}
			attachments = append(attachments, attachment)
		}
		return sendMessageMsg{Content: text, Attachments: attachments}
	}
}

// hasSkillAttachment reports whether attachments hold the skill name.
func hasSkillAttachment(attachments []message.Attachment, name string) bool {
	for _, attachment := range attachments {
		if attachment.IsMarkdown() && attachment.FileName == name {
			return true
		}
	}
	return false
}

// readSkillAttachment reads a skill into the attachment that carries it to
// the agent, named after the skill.
func readSkillAttachment(ws workspace.Workspace, skillID, name string) (message.Attachment, error) {
	content, result, err := ws.ReadSkill(context.Background(), skillID)
	if err != nil {
		return message.Attachment{}, err
	}
	fileName := result.Name
	if fileName == "" {
		fileName = name
	}
	return message.Attachment{
		FilePath: fileName,
		FileName: fileName,
		MimeType: "text/markdown",
		Content:  content,
	}, nil
}
