package model

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/v3/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

type pasteLimitWorkspace struct {
	*submissionWorkspace
	cfg *config.Config
}

func (w *pasteLimitWorkspace) Config() *config.Config { return w.cfg }

func TestLongPasteSurvivesFullAttachmentSlots(t *testing.T) {
	for _, fillAfterPaste := range []bool{false, true} {
		u, ws := newSubmissionUI()
		providers := csync.NewMap[string, config.ProviderConfig]()
		providers.Set("p", config.ProviderConfig{ID: "p", Models: []catwalk.Model{{ID: "m", MaxAttachments: 1}}})
		u.com.Workspace = &pasteLimitWorkspace{ws, &config.Config{Providers: providers}}
		u.lastPick = config.SelectedModel{Provider: "p", Model: "m"}
		u.textarea.Focus()
		content := strings.Repeat("long line\n", 11)
		if !fillAfterPaste {
			u.attachments.Update(message.Attachment{FileName: "occupied"})
		}
		cmd := u.handlePasteMsg(tea.PasteMsg{Content: content})
		if fillAfterPaste {
			require.NotNil(t, cmd)
			u.attachments.Update(message.Attachment{FileName: "occupied"})
			_, _ = u.handleAgentMsg(cmd())
		}
		require.Equal(t, content, u.textarea.Value())
		require.Len(t, u.attachments.List(), 1)
	}
}
