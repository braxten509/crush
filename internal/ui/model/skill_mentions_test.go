package model

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/charmbracelet/crush/internal/ui/completions"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

var testSkillCatalog = []skills.CatalogEntry{
	{ID: "/skills/teach/SKILL.md", Name: "teach", UserInvocable: true},
	// Most skills never set user-invocable; $name still invokes them.
	{ID: "/skills/deep-debugging/SKILL.md", Name: "deep-debugging"},
}

func mentionNames(mentions []skillMention) []string {
	names := make([]string, 0, len(mentions))
	for _, mention := range mentions {
		names = append(names, mention.entry.Name)
	}
	return names
}

func TestFindSkillMentions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		text string
		want []string
	}{
		{"$teach me sets", []string{"teach"}},
		{"please $teach, then $deep-debugging.", []string{"teach", "deep-debugging"}},
		{"costs $5 and uses $HOME", []string{}},
		{"turned off, so not listed: $off-skill", []string{}},
		{"glued a$teach and $teacher", []string{}},
		{"line one\n$teach", []string{"teach"}},
		{"$ alone and $", []string{}},
	}
	for _, tt := range tests {
		got := mentionNames(findSkillMentions(tt.text, testSkillCatalog))
		require.Equal(t, tt.want, got, tt.text)
	}

	mentions := findSkillMentions("try $teach.", testSkillCatalog)
	require.Len(t, mentions, 1)
	require.Equal(t, "$teach", "try $teach."[mentions[0].start:mentions[0].end])
}

func TestHighlightSkillMentionsColorsOnlySkills(t *testing.T) {
	t.Parallel()

	style := lipgloss.NewStyle().Foreground(lipgloss.Color("#8b5cf6")).Bold(true)
	base := lipgloss.NewStyle().Foreground(lipgloss.Color("#cccccc"))
	row := base.Render("use $teach for $5 ünï $deep-debugging")

	got := highlightSkillMentions([]string{row}, testSkillCatalog, style)[0]
	require.Equal(t, ansi.Strip(row), ansi.Strip(got), "the text must not change")
	require.Contains(t, got, style.Render("$teach"))
	require.Contains(t, got, style.Render("$deep-debugging"))
	require.NotContains(t, got, style.Render("$5"))

	plain := base.Render("costs $5 and $HOME")
	require.Equal(t, plain, highlightSkillMentions([]string{plain}, testSkillCatalog, style)[0])
}

// skillReadWorkspace serves skill reads for sendPrompt.
type skillReadWorkspace struct {
	workspace.Workspace
	reads []string
}

func (w *skillReadWorkspace) ReadSkill(_ context.Context, skillID string) ([]byte, skills.SkillReadResult, error) {
	w.reads = append(w.reads, skillID)
	for _, entry := range testSkillCatalog {
		if entry.ID == skillID {
			return []byte("# " + entry.Name), skills.SkillReadResult{Name: entry.Name}, nil
		}
	}
	return nil, skills.SkillReadResult{}, skills.ErrSkillNotFound
}

func TestSendPromptAttachesEachInvokedSkillOnce(t *testing.T) {
	t.Parallel()

	ws := &skillReadWorkspace{}
	u := newTestUI()
	u.com.Workspace = ws
	u.skillCatalog = testSkillCatalog

	attached := message.Attachment{FileName: "deep-debugging", MimeType: "text/markdown", Content: []byte("kept")}
	text := "$teach then $teach again with $deep-debugging and $5"
	cmd := u.sendPrompt(text, []message.Attachment{attached})
	require.NotNil(t, cmd)

	msg, ok := cmd().(sendMessageMsg)
	require.True(t, ok)
	require.Equal(t, text, msg.Content, "the prompt is sent as typed")
	require.Equal(t, []string{"/skills/teach/SKILL.md"}, ws.reads)
	require.Len(t, msg.Attachments, 2)
	require.Equal(t, attached, msg.Attachments[0])
	require.Equal(t, message.Attachment{
		FilePath: "teach",
		FileName: "teach",
		MimeType: "text/markdown",
		Content:  []byte("# teach"),
	}, msg.Attachments[1])
}

func typeKeys(u *UI, text string) {
	for _, r := range text {
		u.typeInEditor(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func TestSkillPopupOpensOnDollarAndFillsName(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	st := u.com.Styles
	u.mention.popup = completions.New(st.Completions.Normal, st.Completions.Focused, st.Completions.Match)
	u.skillCatalog = testSkillCatalog

	typeKeys(u, "hi $")
	require.True(t, u.mention.open, "$ after a space opens the skill list")
	require.Equal(t, skillMentionPrefix, u.mention.prefix)

	typeKeys(u, "tea")
	require.True(t, u.mention.popup.HasItems())

	_, ok := u.handleCompletionsKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.True(t, ok)
	require.False(t, u.mention.open)
	require.Equal(t, "hi $teach ", u.textarea.Value())

	typeKeys(u, "$zz")
	require.False(t, u.mention.open, "the list closes once no skill matches")

	typeKeys(u, " $HOME")
	require.False(t, u.mention.open, "a loose fuzzy match does not keep the list open")

	u.textarea.Reset()
	u.bangMode = true
	typeKeys(u, "$")
	require.False(t, u.mention.open, "shell mode keeps $ for variables")
}
