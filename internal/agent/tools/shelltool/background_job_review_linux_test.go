//go:build linux && amd64

package shelltool

import (
	"context"
	"encoding/json"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/shell"
)

func TestBackgroundJobOutputIncludesCompletedReview(t *testing.T) {
	root := visibleReviewRoot(t)
	ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, "background-output-review")
	response := runBashTool(t, newBashToolForTest(root), ctx, tools.BashParams{
		Command: `sleep 2; printf 'late change' > late.txt`, RunInBackground: true,
	})
	require.False(t, response.IsError, response.Content)
	var metadata tools.BashResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(response.Metadata), &metadata))
	require.NotEmpty(t, metadata.ShellID)
	job, ok := shell.GetBackgroundShellManager().Get(metadata.ShellID)
	require.True(t, ok)
	defer shell.GetBackgroundShellManager().Remove(metadata.ShellID)
	job.Wait()
	input, err := json.Marshal(tools.JobOutputParams{ShellID: metadata.ShellID, Wait: true})
	require.NoError(t, err)
	output, err := NewJobOutputTool(t.TempDir()).Run(ctx, fantasy.ToolCall{ID: "output", Name: tools.JobOutputToolName, Input: string(input)})
	require.NoError(t, err)
	_, review := filechange.TakeReview(output.Metadata)
	require.NotNil(t, review)
	require.Len(t, review.Changes, 1)
	require.Nil(t, review.Changes[0].Before)
	require.Equal(t, "late change", review.Changes[0].After.Content)
}
