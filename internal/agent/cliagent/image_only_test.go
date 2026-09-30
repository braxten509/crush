package cliagent

import (
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
