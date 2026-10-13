package tools

import (
	"encoding/json"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
)

func TestFetchWithoutSessionNamesFetching(t *testing.T) {
	t.Parallel()

	tool := NewFetchTool(&mockPermissionService{}, t.TempDir(), nil)
	input, err := json.Marshal(FetchParams{URL: "https://example.com", Format: "text"})
	require.NoError(t, err)

	_, err = tool.Run(t.Context(), fantasy.ToolCall{ID: "fetch", Name: FetchToolName, Input: string(input)})
	require.EqualError(t, err, "session ID is required for fetching a URL")
}
