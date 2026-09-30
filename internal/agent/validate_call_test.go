package agent

import (
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

func TestValidateCallImageOnly(t *testing.T) {
	t.Parallel()
	image := []message.Attachment{{MimeType: "image/png", Content: []byte("png")}}
	require.NoError(t, ValidateCall(SessionAgentCall{SessionID: "s", Attachments: image}))
	require.ErrorIs(t, ValidateCall(SessionAgentCall{SessionID: "s"}), ErrEmptyPrompt)
}
