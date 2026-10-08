//go:build linux && amd64

package tools

import (
	"context"
	"encoding/json"
	"testing"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/shell"
	"github.com/stretchr/testify/require"
)

func TestBackgroundJobOutputIncludesCompletedReview(t *testing.T) {
	root := visibleReviewRoot(t)
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "background-output-review")
	response := runBashTool(t, newBashToolForTest(root), ctx, BashParams{
		Command: `sleep 2; printf 'late change' > late.txt`, RunInBackground: true,
	})
	require.False(t, response.IsError, response.Content)
	var metadata BashResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(response.Metadata), &metadata))
	require.NotEmpty(t, metadata.ShellID)
	job, ok := shell.GetBackgroundShellManager().Get(metadata.ShellID)
	require.True(t, ok)
	defer shell.GetBackgroundShellManager().Remove(metadata.ShellID)
	job.Wait()
	input, err := json.Marshal(JobOutputParams{ShellID: metadata.ShellID, Wait: true})
	require.NoError(t, err)
	output, err := NewJobOutputTool(t.TempDir()).Run(ctx, fantasy.ToolCall{ID: "output", Name: JobOutputToolName, Input: string(input)})
	require.NoError(t, err)
	_, review := filechange.TakeReview(output.Metadata)
	require.NotNil(t, review)
	require.Len(t, review.Changes, 1)
	require.Nil(t, review.Changes[0].Before)
	require.Equal(t, "late change", review.Changes[0].After.Content)
}
