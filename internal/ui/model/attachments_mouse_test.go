package model

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/ui/attachments"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

type attachmentClickWorkspace struct {
	historyWorkspace
}

func (attachmentClickWorkspace) AgentIsReady() bool { return false }

func newAttachmentClickTestUI(t *testing.T) (*UI, int) {
	t.Helper()

	u := newTestUI()
	u.com.Workspace = attachmentClickWorkspace{}
	u.dialog = dialog.NewOverlay()
	sty := u.com.Styles.Attachments
	renderer := attachments.NewRenderer(
		sty.Normal,
		sty.Deleting,
		sty.Image,
		sty.Text,
		sty.Skill,
		sty.Remove,
	)
	u.attachments = attachments.New(renderer, attachments.Keymap{})
	u.updateLayoutAndSize()
	require.True(t, u.attachments.Update(message.Attachment{FileName: "test.txt"}))
	_ = u.attachments.Render(u.layout.editor.Dx())

	for x := range u.layout.editor.Dx() {
		if renderer.HitTestRemove(u.attachments.List(), x) == 0 {
			return u, x
		}
	}
	t.Fatal("remove button was not rendered")
	return nil, 0
}

func TestAttachmentClickIgnoredWhileInlineEditorIsActive(t *testing.T) {
	t.Parallel()

	u, removeX := newAttachmentClickTestUI(t)
	u.openBatchFormDialog(question.Request{
		Questions: []question.Question{{
			ID:   "question",
			Type: question.TypeFreeText,
			Text: "Question?",
		}},
	})

	_, _ = u.Update(tea.MouseClickMsg(tea.Mouse{
		X:      u.layout.editor.Min.X + removeX,
		Y:      u.layout.editor.Min.Y + editorAttachmentsRow,
		Button: uv.MouseLeft,
	}))

	require.Len(t, u.attachments.List(), 1)
}

func TestAttachmentClickRequiresLeftMouseButton(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		button    tea.MouseButton
		remaining int
	}{
		{name: "left", button: uv.MouseLeft, remaining: 0},
		{name: "middle", button: uv.MouseMiddle, remaining: 1},
		{name: "right", button: uv.MouseRight, remaining: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			u, removeX := newAttachmentClickTestUI(t)
			_, _ = u.Update(tea.MouseClickMsg(tea.Mouse{
				X:      u.layout.editor.Min.X + removeX,
				Y:      u.layout.editor.Min.Y + editorAttachmentsRow,
				Button: tt.button,
			}))

			require.Len(t, u.attachments.List(), tt.remaining)
		})
	}
}

func TestAttachmentsSitRightOnTopOfTheComposerBand(t *testing.T) {
	t.Parallel()

	u, _ := newAttachmentClickTestUI(t)
	rows := strings.Split(u.renderEditorView(u.layout.editor.Dx()), "\n")
	require.Contains(t, ansi.Strip(rows[editorAttachmentsRow]), "test.txt")
	require.NotContains(t, ansi.Strip(rows[editorAttachmentsRow+1]), "test.txt", "the band's top padding stays empty")
	require.Equal(t, editorAttachmentsRow+1, editorTextTop-1, "no gap between the attachments and the band")
}

func TestCompactChatHasNoHeaderBar(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	u.forceCompactMode = true
	u.updateLayoutAndSize()
	require.True(t, u.isCompact)
	top := u.layout.area.Min.Y + 1 // the app's top margin
	require.Equal(t, top, u.layout.main.Min.Y, "the chat starts right below the margin, with no bar")
	require.True(t, u.layout.header.Empty())
	require.Equal(t, top, u.layout.sessionDetails.Min.Y, "ctrl+d details open from the top")
}

func TestLastRowStaysBlankUnderTheModeBadge(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	u.updateLayoutAndSize()
	require.Equal(t, u.height-2, u.layout.status.Min.Y, "the badge row sits above the last row")
	require.LessOrEqual(t, u.layout.status.Max.Y, u.height-1, "nothing is laid out on the last row")
}
