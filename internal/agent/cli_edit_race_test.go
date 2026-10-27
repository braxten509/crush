package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"charm.land/fantasy"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/stretchr/testify/require"
)

// raceSteps runs a tool call whose file is already written when Crush reads
// its "before" copy, as happens with Codex patches, and returns the metadata.
func raceSteps(t *testing.T, path, name, input, reported string) tools.EditResponseMetadata {
	t.Helper()
	var saved string
	s := &cliSteps{ctx: t.Context(), m: &cliagent.Model{Dir: filepath.Dir(path)}, sc: fantasy.AgentStreamCall{
		OnToolCall:   func(fantasy.ToolCallContent) error { return nil },
		OnToolResult: func(r fantasy.ToolResultContent) error { saved = r.ClientMetadata; return nil },
	}}
	require.NoError(t, s.handle(cliagent.Event{Type: cliagent.EventToolCall, ID: "call", Name: name, Input: input}))
	require.NoError(t, s.handle(cliagent.Event{Type: cliagent.EventToolResult, ID: "call", Name: name, Output: "Applied", Metadata: reported}))
	var meta tools.EditResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(saved), &meta))
	return meta
}

func TestCLIEditWrittenBeforeReadUsesReportedBefore(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "notes.md")
	require.NoError(t, os.WriteFile(path, []byte("one\nTWO\nthree\n"), 0o644))
	reported, err := json.Marshal(tools.EditResponseMetadata{Additions: 1, Removals: 1, OldContent: "one\ntwo\nthree\n", NewContent: "one\nTWO\nthree\n"})
	require.NoError(t, err)
	input, err := json.Marshal(tools.EditParams{FilePath: path, OldString: "two\n", NewString: "TWO\n"})
	require.NoError(t, err)
	meta := raceSteps(t, path, tools.EditToolName, string(input), string(reported))
	require.Equal(t, "one\ntwo\nthree\n", meta.OldContent)
	require.Equal(t, "one\nTWO\nthree\n", meta.NewContent)
	require.Equal(t, 1, meta.Additions)
	require.Equal(t, 1, meta.Removals)
}

func TestCLINewFileWrittenBeforeReadIsACreation(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "prompt.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello\nworld\n"), 0o644))
	input, err := json.Marshal(tools.WriteParams{FilePath: path, Content: "hello\nworld\n"})
	require.NoError(t, err)
	meta := raceSteps(t, path, tools.WriteToolName, string(input), "")
	require.Empty(t, meta.OldContent)
	require.Equal(t, "hello\nworld\n", meta.NewContent)
	require.Equal(t, 2, meta.Additions)
}

// A CLI that reports an edit only after writing it, with just the replaced
// text (Grok, OpenCode when it wins the race): the before copy is rebuilt
// by undoing the replacement, and the change is recorded.
func TestCLIEditReportedLateIsUndoneFromItsReplacement(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644))
	path := filepath.Join(dir, "notes.txt")
	require.NoError(t, os.WriteFile(path, []byte("one\nbanana\nthree\n"), 0o644))
	input, err := json.Marshal(tools.EditParams{FilePath: path, OldString: "apple", NewString: "banana"})
	require.NoError(t, err)
	reported, err := json.Marshal(tools.EditResponseMetadata{OldContent: "apple", NewContent: "banana"})
	require.NoError(t, err)
	var saved string
	s := &cliSteps{ctx: t.Context(), m: &cliagent.Model{Dir: dir}, sc: fantasy.AgentStreamCall{
		OnToolCall:   func(fantasy.ToolCallContent) error { return nil },
		OnToolResult: func(r fantasy.ToolResultContent) error { saved = r.ClientMetadata; return nil },
	}}
	require.NoError(t, s.handle(cliagent.Event{Type: cliagent.EventToolCall, ID: "call", Name: tools.EditToolName, Input: string(input)}))
	require.NoError(t, s.handle(cliagent.Event{Type: cliagent.EventToolResult, ID: "call", Name: tools.EditToolName, Output: "ok", Metadata: string(reported)}))
	metadata, review := filechange.TakeReview(saved)
	var meta tools.EditResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(metadata), &meta))
	require.Equal(t, "one\napple\nthree\n", meta.OldContent)
	require.Equal(t, 1, meta.Additions)
	require.Equal(t, 1, meta.Removals)
	require.NotNil(t, review, "the change is recorded")
	require.Len(t, review.Changes, 1)
	require.Equal(t, "one\napple\nthree\n", review.Changes[0].Before.Content)
	require.Equal(t, "one\nbanana\nthree\n", review.Changes[0].After.Content)
}
