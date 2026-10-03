package cliagent

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

// An image sent without text must not carry an empty text block: Claude's
// API rejects those.
func TestImageOnlyPrompt(t *testing.T) {
	images := []message.Attachment{{MimeType: "image/png", Content: []byte("png")}}

	content := claudePrompt("", images)["message"].(map[string]any)["content"].([]any)
	require.Len(t, content, 1)
	require.Equal(t, "image", content[0].(map[string]any)["type"])

	input := codexInput("", images)
	require.Len(t, input, 1)
	require.Equal(t, "image", input[0].(map[string]any)["type"])

	require.Equal(t, "hi", claudePrompt("hi", nil)["message"].(map[string]any)["content"])
	require.Len(t, codexInput("hi", images), 2)
}

// Claude and Codex see images inline, but also get the saved copies' paths so
// they can pass them on; echoes drop the note to match what the user typed.
func TestSavedImagePaths(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	images := []message.Attachment{{MimeType: "image/png", Content: []byte("png")}}

	require.Equal(t, "hi", withSavedImagePaths("hi", nil))
	for _, text := range []string{"look at this", ""} {
		full := withSavedImagePaths(text, images)
		require.NotEqual(t, text, full)
		require.Equal(t, text, withoutImagePaths(full))

		path := full[strings.LastIndex(full, "\n")+1:]
		saved, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, "png", string(saved))
	}
}
