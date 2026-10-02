//go:build linux && amd64

package tools

import (
	"charm.land/fantasy"
	"context"
	"encoding/json"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/shell"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/stretchr/testify/require"
)

func TestNativeBashReviewsScriptsAndRedirections(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "existing"), []byte("before\n"), 0600))
	command := `printf 'first\n' > created
python3 - <<'PY'
from pathlib import Path
Path('existing').write_text('after\n')
Path('created').write_text('second\n')
PY
printf 'last\n' >> created`
	response := runBashTool(t, newBashToolForTest(root), context.WithValue(t.Context(), SessionIDContextKey, "native-review"), BashParams{Command: command})
	require.False(t, response.IsError, response.Content)
	_, review := filechange.TakeReview(response.Metadata)
	require.NotNil(t, review)
	require.Len(t, review.Changes, 2)
	require.Equal(t, filepath.Join(root, "created"), review.Changes[0].Path)
	require.Nil(t, review.Changes[0].Before)
	require.Equal(t, "second\nlast\n", review.Changes[0].After.Content)
	require.Equal(t, "before\n", review.Changes[1].Before.Content)
	require.Equal(t, "after\n", review.Changes[1].After.Content)
}

func TestBackgroundShellPersistsReviewAfterTurnEnds(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(map[bool]string{false: "explicit", true: "manual"}[manual], func(t *testing.T) {
			root := t.TempDir()
			conn, err := db.Connect(t.Context(), t.TempDir())
			require.NoError(t, err)
			defer conn.Close()
			queries := db.New(conn)
			sessions := session.NewService(queries, conn)
			sess, err := sessions.Create(t.Context(), "background review")
			require.NoError(t, err)
			messages := message.NewService(queries)
			turnCtx, cancelTurn := context.WithCancel(context.WithValue(t.Context(), SessionIDContextKey, sess.ID))
			defer cancelTurn()
			tool := newBashToolForTest(root)
			command := `python3 -c 'import time,pathlib; time.sleep(2); pathlib.Path("late.txt").write_text("late change")'`
			input, err := json.Marshal(BashParams{Command: command, RunInBackground: !manual})
			require.NoError(t, err)
			responses := make(chan fantasy.ToolResponse, 1)
			errors := make(chan error, 1)
			go func() {
				response, err := tool.Run(turnCtx, fantasy.ToolCall{ID: "bash-origin", Name: BashToolName, Input: string(input)})
				responses <- response
				errors <- err
			}()
			if manual {
				require.Eventually(t, func() bool { return BackgroundShell(sess.ID) }, time.Second, 10*time.Millisecond)
			}
			response := <-responses
			require.NoError(t, <-errors)
			require.False(t, response.IsError, response.Content)
			var meta BashResponseMetadata
			require.NoError(t, json.Unmarshal([]byte(response.Metadata), &meta))
			require.True(t, meta.Background)
			require.NotEmpty(t, meta.ShellID)
			job, ok := shell.GetBackgroundShellManager().Get(meta.ShellID)
			require.True(t, ok)
			defer shell.GetBackgroundShellManager().Remove(meta.ShellID)
			metadata, initialReview := filechange.TakeReview(response.Metadata)
			result := message.ToolResult{ToolCallID: "bash-origin", Name: BashToolName, Content: response.Content, Metadata: metadata, Review: initialReview}
			saved, err := messages.Create(turnCtx, sess.ID, message.CreateMessageParams{Role: message.Tool, Parts: []message.ContentPart{result}})
			require.NoError(t, err)
			PersistBackgroundReview(turnCtx, messages, sess.ID, result)
			cancelTurn()
			job.Wait()
			data, err := os.ReadFile(filepath.Join(root, "late.txt"))
			require.NoError(t, err)
			require.Equal(t, "late change", string(data))
			require.Eventually(t, func() bool {
				stored, err := messages.Get(t.Context(), saved.ID)
				return err == nil && stored.ToolResults()[0].Review != nil
			}, 3*time.Second, 10*time.Millisecond)
			// Reload through a fresh service to verify the completed review reached SQL.
			reloaded, err := message.NewService(queries).Get(t.Context(), saved.ID)
			require.NoError(t, err)
			review := reloaded.ToolResults()[0].Review
			require.NotNil(t, review)
			require.Len(t, review.Changes, 1)
			require.Equal(t, filepath.Join(root, "late.txt"), review.Changes[0].Path)
			require.Nil(t, review.Changes[0].Before)
			require.Equal(t, "late change", review.Changes[0].After.Content)
			input, err = json.Marshal(JobOutputParams{ShellID: meta.ShellID, Wait: true})
			require.NoError(t, err)
			output, err := NewJobOutputTool(t.TempDir()).Run(t.Context(), fantasy.ToolCall{ID: "job", Name: JobOutputToolName, Input: string(input)})
			require.NoError(t, err)
			_, outputReview := filechange.TakeReview(output.Metadata)
			require.Equal(t, review, outputReview)
		})
	}
}
