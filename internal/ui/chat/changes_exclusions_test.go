package chat

import (
	"testing"

	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestReviewExcludesAgentAndApplicationFiles(t *testing.T) {
	t.Setenv("HOME", "/home/reviewer")
	t.Setenv("XDG_CONFIG_HOME", "/settings")
	t.Setenv("XDG_DATA_HOME", "/app-data")
	t.Setenv("XDG_STATE_HOME", "/app-state")
	for _, directory := range []string{".agents", ".claude", ".codex", ".config", ".crush", ".gemini", ".grok", ".opencode", ".cursor", ".agy", ".antigravity"} {
		require.True(t, ignoredReviewPath(directory+"/settings.json", "/workspace"))
		require.True(t, ignoredReviewPath("/home/reviewer/"+directory+"/settings.json", ""))
		require.False(t, ignoredReviewPath(directory+"-example/main.go", "/workspace"))
	}
	for _, path := range []string{"/settings/app/config", "/app-data/agent-memory/note.md", "/app-state/prompter/prompt.md"} {
		require.True(t, ignoredReviewPath(path, ""), path)
	}
	for _, path := range []string{"/workspace/.env", "/workspace/.github/workflows/test.yml", "/workspace/.gitignore", "/workspace/config/settings.json", "/home/reviewer/.local/bin/helper", "/app-data-source/main.go"} {
		require.False(t, ignoredReviewPath(path, ""), path)
	}
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_STATE_HOME", "relative-invalid")
	require.True(t, ignoredReviewPath(".local/share/agent-memory/note.md", "/home/reviewer"))
	require.True(t, ignoredReviewPath(".local/state/prompter/prompt.md", "/home/reviewer"))
}

func TestSavedReviewAndLegacyEditsHideAgentFiles(t *testing.T) {
	sty := styles.CharmtonePantera()
	call := message.ToolCall{ID: "install", Name: "bash", Finished: true}
	result := &message.ToolResult{ToolCallID: call.ID, Name: call.Name, Review: &filechange.Review{
		Root: "/workspace", Changes: []filechange.Change{
			{Path: ".agents/skills/example/SKILL.md", After: &filechange.State{Content: "skill\n"}},
			{Path: "main.go", After: &filechange.State{Content: "package main\n"}},
		},
	}}
	legacy := message.ToolCall{ID: "edit", Name: "edit", Finished: true, Input: `{"file_path":"/home/reviewer/.codex/settings.json"}`}
	legacyResult := &message.ToolResult{ToolCallID: legacy.ID, Name: legacy.Name, Metadata: `{"old_content":"old","new_content":"new"}`}
	group := NewToolGroupItem(&sty)
	group.SetChildren([]MessageItem{
		NewToolMessageItem(&sty, "saved", call, result, false, ""),
		NewToolMessageItem(&sty, "legacy", legacy, legacyResult, false, ""),
	})
	require.Len(t, group.Changes(), 1)
	require.Equal(t, "main.go", group.Changes()[0].Path)
	header := ansi.Strip(group.header(160))
	require.Contains(t, header, "edited main.go +1 −0")
	require.NotContains(t, header, "SKILL.md")
	require.NotContains(t, header, "settings.json")
}
