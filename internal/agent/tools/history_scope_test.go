package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/stretchr/testify/require"
)

type scopeHistory struct {
	mockHistoryService
	recorded int
}

func (h *scopeHistory) Create(ctx context.Context, session, path, content string) (history.File, error) {
	h.recorded++
	return h.mockHistoryService.Create(ctx, session, path, content)
}
func (h *scopeHistory) CreateVersion(ctx context.Context, session, path, content string) (history.File, error) {
	h.recorded++
	return h.mockHistoryService.CreateVersion(ctx, session, path, content)
}

func TestNativeFileHistoryRespectsRepositoryBoundary(t *testing.T) {
	for _, mode := range []string{"inside", "outside", "no-repository", "external-symlink"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			if mode != "no-repository" {
				require.NoError(t, os.Mkdir(filepath.Join(root, ".git"), 0700))
			}
			path := filepath.Join(root, "file.txt")
			if mode == "outside" || mode == "external-symlink" {
				outside := filepath.Join(t.TempDir(), "outside.txt")
				require.NoError(t, os.WriteFile(outside, []byte("before"), 0600))
				if mode == "outside" {
					path = outside
				} else {
					require.NoError(t, os.Symlink(outside, path))
				}
			}
			versions := &scopeHistory{}
			tracker := &mockEditFileTracker{}
			edit := editContext{ctx: t.Context(), workingDir: root, files: versions, filetracker: tracker}
			require.NoError(t, commitFileChange(edit, "session", path, "before", "after"))
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, "after", string(data), "the review boundary must not block the actual edit")
			if mode == "inside" {
				require.Positive(t, versions.recorded)
			} else {
				require.Zero(t, versions.recorded)
			}
		})
	}
}

func TestNativeWriteOutsideRepositoryDoesNotSaveHistory(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, ".git"), 0700))
	path := filepath.Join(t.TempDir(), "file.txt")
	versions := &scopeHistory{}
	tool := NewWriteTool(nil, &mockPermissionService{}, versions, mockFileTrackerService{}, root)
	input, err := json.Marshal(WriteParams{FilePath: path, Content: "written"})
	require.NoError(t, err)
	ctx := context.WithValue(t.Context(), SessionIDContextKey, "write-session")
	response, err := tool.Run(ctx, fantasy.ToolCall{ID: "write", Name: WriteToolName, Input: string(input)})
	require.NoError(t, err)
	require.False(t, response.IsError, response.Content)
	require.Zero(t, versions.recorded)
}
