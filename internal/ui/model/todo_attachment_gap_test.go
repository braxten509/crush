package model

import (
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/attachments"
	"github.com/stretchr/testify/require"
)

// With an attachment showing, a blank row keeps the to-dos off the
// attachments row; it goes away with the attachment.
func TestTodosKeepABlankRowAboveAttachments(t *testing.T) {
	u := newTestUI()
	u.attachments = attachments.New(nil, attachments.Keymap{})
	u.session = &session.Session{ID: "s1", Todos: []session.Todo{{Content: "a", Status: session.TodoStatusPending}}}
	u.updateLayoutAndSize()
	require.Equal(t, 1, u.layout.pills.Dy())

	require.True(t, u.attachments.Update(message.Attachment{FilePath: "/tmp/shot.png", FileName: "shot.png", MimeType: "image/png", Content: []byte("x")}))
	u.syncAttachmentsLayout()
	require.Equal(t, 2, u.layout.pills.Dy(), "to-do row plus a blank row")

	u.attachments.Reset()
	u.syncAttachmentsLayout()
	require.Equal(t, 1, u.layout.pills.Dy())
}
