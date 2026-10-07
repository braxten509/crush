package model

import (
	"os"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/chat"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

func TestImageFilePreservesAttachedBytes(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	image := message.Attachment{FilePath: "missing clipboard.png", MimeType: "image/png", Content: []byte("original picture")}
	path, err := imageFile(image)
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, image.Content, data)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.EqualValues(t, 0o600, info.Mode().Perm())
}

func TestComposerImageOpensOnReleaseAndNotDrag(t *testing.T) {
	for _, drag := range []bool{false, true} {
		u, _ := newAttachmentClickTestUI(t)
		u.attachments.Reset()
		attachment := message.Attachment{FileName: "picture.png", MimeType: "image/png", Content: []byte("picture")}
		u.attachments.Update(attachment)
		u.attachments.Render(u.layout.editor.Dx())
		x, y := u.layout.editor.Min.X, u.layout.editor.Min.Y+editorAttachmentsRow
		_, cmd := u.Update(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: uv.MouseLeft}))
		_ = cmd // Focus commands may be present; the viewer is deferred.
		require.NotNil(t, u.imagePress)
		if drag {
			u.Update(tea.MouseMotionMsg(tea.Mouse{X: x + 2, Y: y, Button: uv.MouseLeft}))
		}
		_, cmd = u.Update(tea.MouseReleaseMsg(tea.Mouse{X: x, Y: y, Button: uv.MouseLeft}))
		if drag {
			require.Nil(t, cmd)
		} else {
			require.NotNil(t, cmd)
			require.Equal(t, openImageMsg{attachment}, cmd())
		}
		require.Len(t, u.attachments.List(), 1)
	}
}

func TestChatImageUsesOriginalAttachment(t *testing.T) {
	u, _ := newAttachmentClickTestUI(t)
	item := chat.NewUserMessageItem(u.com.Styles, &message.Message{ID: "picture", Role: message.User, Parts: []message.ContentPart{
		message.TextContent{Text: "Picture"}, message.BinaryContent{Path: "missing.png", MIMEType: "image/png", Data: []byte("picture bytes")},
	}}, u.attachments.Renderer()).(*chat.UserMessageItem)
	u.chat.SetMessages(item)
	u.updateLayoutAndSize()
	item.Render(u.chat.list.Width())
	// Another item can reuse the renderer while this item's render is cached.
	u.attachments.Renderer().Render([]message.Attachment{{FileName: "other.png", MimeType: "image/png", Content: []byte("wrong image")}}, false, false, u.chat.list.Width())
	var attachment *message.Attachment
	for y := 0; y < 10; y++ {
		if a := item.ImageAt(2, y, u.chat.list.Width()); a != nil {
			attachment = a
			break
		}
	}
	require.NotNil(t, attachment)
	require.Equal(t, []byte("picture bytes"), attachment.Content)
}
