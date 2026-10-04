package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"charm.land/fantasy"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/agent/tools"
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
