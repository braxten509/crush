package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"testing"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/secureentry"
	"github.com/charmbracelet/crush/internal/ui/diffreview"
	"github.com/stretchr/testify/require"
)

func TestSecureEntryExcludedFromLateAndFutureReviews(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "secure entry review")
	require.NoError(t, err)
	path := filepath.Join(env.workingDir, "credentials.env")
	r := &turnFileReview{root: env.workingDir, messages: env.messages, sessionID: sess.ID,
		trackers: map[string]*filechange.Tracker{}, results: map[string]string{}}
	r.track("template", "write", `{"file_path":"credentials.env"}`)
	require.NoError(t, os.WriteFile(path, []byte("TOKEN=%s\n"), 0o600))
	require.NoError(t, r.save(t.Context(), message.ToolResult{ToolCallID: "template", Name: "write"}))
	target, err := secureentry.Prepare(secureentry.Spec{File: path, Occurrence: 1})
	require.NoError(t, err)
	defer target.Close()
	require.NoError(t, target.Save([]byte("synthetic-secret-never-in-chat")))
	r.finish(t.Context())
	future := &turnFileReview{root: env.workingDir, messages: env.messages, sessionID: sess.ID,
		trackers: map[string]*filechange.Tracker{}, results: map[string]string{}}
	future.track("later", "edit", `{"file_path":"credentials.env"}`)
	// Native tool reviews and snapshots both obey the sensitive exclusion.
	require.NoError(t, future.save(t.Context(), message.ToolResult{ToolCallID: "later", Name: "edit", Metadata: filechange.WithReview("", &filechange.Review{Root: env.workingDir,
		Changes: []filechange.Change{{Path: path, After: &filechange.State{Content: "synthetic-secret-never-in-chat"}}}})}))
	future.finish(t.Context())
	require.NoError(t, env.messages.FlushAll(t.Context()))
	msgs, err := loadFileReviewMessages(env.messages, t.Context(), sess.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	encoded, err := json.Marshal(msgs)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "synthetic-secret-never-in-chat")
	require.Len(t, msgs[0].ToolResults()[0].Review.Changes, 1, "the original placeholder review is safe")
	require.Empty(t, msgs[1].ToolResults()[0].Review.Changes)
}

// Run the real CLI event adapter and the shared agent callbacks. The fake CLI
// deliberately writes before emitting the tool call, as some real CLIs do.
func TestFileReviewCLIEndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX shell")
	}
	env := testEnv(t)
	bin := t.TempDir()
	script := `#!/bin/sh
read -r _; read -r _
printf 'after\n' > changed.txt
printf 'unrelated\n' > unreported.txt
echo '{"type":"system","subtype":"init","session_id":"file-review-test"}'
echo '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"edit","name":"Edit","input":{"file_path":"changed.txt","old_string":"before\n","new_string":"after\n"}}]}}'
echo '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"edit","content":"Applied"}]}}'
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"Finished fixture."}]}}'
echo '{"type":"result","subtype":"success"}'
`
	require.NoError(t, os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for path, content := range map[string]string{"changed.txt": "before\n", "unrelated.txt": "not edited\n"} {
		require.NoError(t, os.WriteFile(filepath.Join(env.workingDir, path), []byte(content), 0o644))
	}
	provider := cliagent.NewProvider(config.TypeClaudeCode, env.workingDir, t.TempDir(), env.permissions, env.history, "", false)
	model, err := provider.LanguageModel(t.Context(), "fixture")
	require.NoError(t, err)
	// Avoid title generation starting another CLI process for the fixture.
	sa := testSessionAgent(env, model, &finishStreamModel{text: "fixture"}, "system").(*sessionAgent)
	sa.isSubAgent = true
	session, err := env.sessions.Create(t.Context(), "file review fixture")
	require.NoError(t, err)
	_, err = sa.Run(t.Context(), SessionAgentCall{SessionID: session.ID, Prompt: "Apply fixture changes", NonInteractive: true})
	require.NoError(t, err)
	msgs, err := loadFileReviewMessages(env.messages, t.Context(), session.ID)
	require.NoError(t, err)
	var results []message.ToolResult
	for _, msg := range msgs {
		results = append(results, msg.ToolResults()...)
	}
	require.Len(t, results, 1)
	require.False(t, results[0].IsError)
	require.Nil(t, results[0].Review, "a late snapshot must not hide the CLI's diff")
	var meta tools.EditResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(results[0].Metadata), &meta))
	require.Equal(t, "before\n", meta.OldContent)
	require.Equal(t, "after\n", meta.NewContent)
	files := diffreview.Build([]diffreview.Edit{{Path: "changed.txt", Before: meta.OldContent, After: meta.NewContent}})
	require.Len(t, files, 1)
	require.Equal(t, "changed.txt", files[0].Path)

}

func TestFileReviewPersistsFailedAndLateChanges(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	session, err := env.sessions.Create(t.Context(), "review")
	require.NoError(t, err)
	path := filepath.Join(env.workingDir, "file.txt")
	require.NoError(t, os.WriteFile(path, []byte("original\n"), 0o644))
	r := &turnFileReview{root: env.workingDir, messages: env.messages, sessionID: session.ID,
		trackers: map[string]*filechange.Tracker{}, results: map[string]string{}}
	r.track("edit-1", "edit", `{"file_path":"file.txt"}`)
	// A tool which changed the file before reporting its events, then failed.
	require.NoError(t, os.WriteFile(path, []byte("first\n"), 0o644))
	require.NoError(t, r.save(t.Context(), message.ToolResult{ToolCallID: "edit-1", Name: "edit", IsError: true}))
	firstID := r.results["edit-1"]
	first, err := env.messages.Get(t.Context(), firstID)
	require.NoError(t, err)
	require.Equal(t, "original\n", first.ToolResults()[0].Review.Changes[0].Before.Content)

	r.track("edit-2", "multiedit", `{"file_path":"file.txt"}`)
	require.NoError(t, os.WriteFile(path, []byte("second\n"), 0o644))
	require.NoError(t, r.save(t.Context(), message.ToolResult{ToolCallID: "edit-2", Name: "multiedit"}))
	require.NoError(t, os.WriteFile(path, []byte("last\n"), 0o644))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r.finish(ctx)
	require.Empty(t, r.trackers)
	require.NoError(t, env.messages.FlushAll(t.Context()))

	// Reload through the database/JSON path used when a chat is reopened.
	saved, err := loadFileReviewMessages(env.messages, t.Context(), session.ID)
	require.NoError(t, err)
	require.Len(t, saved, 2)
	firstReview := saved[0].ToolResults()[0].Review
	lastReview := saved[1].ToolResults()[0].Review
	require.Equal(t, "first\n", firstReview.Changes[0].After.Content)
	require.Len(t, lastReview.Changes, 2)
	require.Equal(t, "first\n", lastReview.Changes[0].Before.Content)
	require.Equal(t, "last\n", lastReview.Changes[1].After.Content)
	var edits []diffreview.Edit
	for _, msg := range saved {
		for _, change := range msg.ToolResults()[0].Review.Changes {
			edits = append(edits, diffreview.Edit{Path: change.Path, Snapshot: &change})
		}
	}
	files := diffreview.Build(edits)
	require.Len(t, files, 1)
	require.Equal(t, "original", files[0].Lines[1].Text)
	require.Equal(t, "last", files[0].Lines[2].Text)

	// Review snapshots are local UI data, not extra model context.
	ai, err := json.Marshal(saved[1].ToAIMessage())
	require.NoError(t, err)
	require.NotContains(t, string(ai), "original")
	require.NotContains(t, string(ai), "last\\n")
}

func TestFileReviewIgnoresNonEditingTools(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	session, err := env.sessions.Create(t.Context(), "no folder scans")
	require.NoError(t, err)
	r := &turnFileReview{root: env.workingDir, messages: env.messages, sessionID: session.ID,
		trackers: map[string]*filechange.Tracker{}, results: map[string]string{}}
	for _, name := range []string{"bash", "view", "glob", "grep"} {
		r.track(name, name, `{"file_path":".","path":".","command":"echo hello"}`)
		require.NoError(t, r.save(t.Context(), message.ToolResult{ToolCallID: name, Name: name}))
	}
	r.finish(t.Context())
	require.Empty(t, r.trackers)
	msgs, err := loadFileReviewMessages(env.messages, t.Context(), session.ID)
	require.NoError(t, err)
	for _, msg := range msgs {
		require.Nil(t, msg.ToolResults()[0].Review)
	}
}
