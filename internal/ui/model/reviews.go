package model

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/diffreview"
	"github.com/charmbracelet/crush/internal/ui/util"
)

type reviewLoadedMsg struct {
	loading *dialog.Changes
	title   string
	files   []diffreview.File
	err     error
}

func (m *UI) openReview(group *chat.ToolGroupItem) tea.Cmd {
	inputs, title := group.ReviewInputs(), group.ChangesTitle()
	loading := dialog.NewChanges(m.com, title+" · Loading…", nil)
	m.dialog.OpenDialog(loading)
	ws := m.com.Workspace
	sessionID := ""
	if m.session != nil {
		sessionID = m.session.ID
	}
	return func() tea.Msg {
		loaded := map[string]message.Message{}
		for i, input := range inputs {
			if review := input.Result.Review; review != nil && review.Summary != nil {
				msg, ok := loaded[review.ID]
				if !ok {
					var err error
					reviewSession := review.SessionID
					if reviewSession == "" {
						reviewSession = sessionID
					}
					msg, err = ws.LoadMessageReview(context.Background(), reviewSession, review.ID)
					if err != nil {
						return reviewLoadedMsg{loading: loading, err: err}
					}
					loaded[review.ID] = msg
				}
				for _, part := range msg.Parts {
					if result, ok := part.(message.ToolResult); ok && result.ToolCallID == input.Result.ToolCallID {
						inputs[i].Result = result
						break
					}
				}
			}
		}
		return reviewLoadedMsg{loading: loading, title: title, files: chat.BuildReviewChanges(inputs)}
	}
}

func (m *UI) applyReview(msg reviewLoadedMsg) tea.Cmd {
	if m.dialog.Dialog(dialog.ChangesID) != msg.loading {
		return nil
	}
	m.dialog.CloseDialog(dialog.ChangesID)
	if msg.err != nil {
		return util.ReportError(msg.err)
	}
	m.dialog.OpenDialog(dialog.NewChanges(m.com, msg.title, msg.files))
	return nil
}
