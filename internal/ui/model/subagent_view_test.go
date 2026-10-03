package model

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestSubagentViewPreservesParentAndReturns(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"esc", "button", "keyboard"} {
		t.Run(method, func(t *testing.T) {
			ws := newLoadWorkspace()
			u := newLoadTestUI(t, ws)
			msg := message.Message{ID: "child-msg", SessionID: "child", Role: message.Assistant}
			msg.AppendContent("Live child conversation")
			ws.transcripts["child"] = []message.Message{msg}
			u.textarea.SetValue("Unsent parent draft")
			parent, parentChat := u.session, u.chat
			task := agent.Task{ID: "t1", SessionID: parent.ID, ChildID: "child", Name: "Review", Status: agent.TaskRunning}
			u.dialog.OpenDialog(dialog.NewSubAgents(u.com, []agent.Task{task}, task.ID))
			ws.inUpdate.Store(true)
			cmd := u.handleDialogAction(u.dialog.Dialog(dialog.SubAgentsID).HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}))
			ws.inUpdate.Store(false)
			require.Zero(t, ws.readsInUpdate.Load())
			require.NotNil(t, u.subagentView)
			require.False(t, u.dialog.ContainsDialog(dialog.SubAgentsID))
			// The action handler batches its command; load directly for this check.
			require.NotNil(t, cmd)
			loaded := u.loadSubagentView()()
			u.handleSubagentView(loaded)
			require.Same(t, parent, u.session)
			require.Same(t, parentChat, u.chat)
			u.handleSubagentView(tea.PasteMsg{Content: "must not reach parent"})
			require.Equal(t, "Unsent parent draft", u.textarea.Value())
			for _, width := range []int{140, 80, 40} {
				u.width = width
				scr := uv.NewScreenBuffer(width, 24)
				u.Draw(scr, scr.Bounds())
				out := ansi.Strip(scr.Render())
				require.Contains(t, out, "Live child conversation")
				require.Contains(t, out, "Return to parent")
				require.NotContains(t, out, "Unsent parent draft")
				t.Logf("%d columns:\n%s", width, out)
			}
			switch method {
			case "esc":
				u.handleSubagentView(tea.KeyPressMsg{Code: tea.KeyEscape})
			case "button":
				_, _, back := u.subagentViewAreas(uv.Rect(0, 0, u.width, u.height))
				u.handleSubagentView(tea.MouseClickMsg{X: back.Min.X, Y: back.Min.Y, Button: uv.MouseLeft})
			case "keyboard":
				u.handleSubagentView(tea.KeyPressMsg{Code: tea.KeyTab})
				u.handleSubagentView(tea.KeyPressMsg{Code: tea.KeyEnter})
			}
			require.Nil(t, u.subagentView)
			u.handleSubagentView(loaded)
			require.Nil(t, u.subagentView, "late load cannot reopen the child")
			require.Same(t, parent, u.session)
			require.Same(t, parentChat, u.chat)
			require.Equal(t, "Unsent parent draft", u.textarea.Value())
			require.Empty(t, ws.runSessions)
		})
	}
}

func TestSubagentStreamingRefreshKeepsScrollAndCoalesces(t *testing.T) {
	t.Parallel()
	ws := newLoadWorkspace()
	u := newLoadTestUI(t, ws)
	for i := range 30 {
		msg := message.Message{ID: fmt.Sprintf("m%d", i), SessionID: "child", Role: message.User}
		msg.AppendContent(strings.Repeat("message ", 12))
		ws.transcripts["child"] = append(ws.transcripts["child"], msg)
	}
	cmd := u.openSubagentView(agent.Task{ID: "t1", SessionID: "old", ChildID: "child"})
	event := pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: ws.transcripts["child"][0]}
	refresh, consumed := u.handleSubagentView(event)
	require.Nil(t, refresh, "in-flight read holds new events")
	require.False(t, consumed, "parent still handles its events")
	refresh, _ = u.handleSubagentView(cmd())
	require.NotNil(t, refresh, "events during the read cause a second refresh")
	require.True(t, u.subagentView.refreshQueued)
	refresh, _ = u.handleSubagentView(event)
	require.Nil(t, refresh, "multiple events share the pending refresh")
	u.subagentView.chat.SetSize(80, 10)
	u.subagentView.chat.ScrollToTop()
	u.subagentView.chat.ScrollBy(3)
	index, line := u.subagentView.chat.ScrollPosition()
	cmd, _ = u.handleSubagentView(subagentRefreshMsg{view: u.subagentView})
	u.handleSubagentView(cmd())
	i, l := u.subagentView.chat.ScrollPosition()
	require.Equal(t, index, i)
	require.Equal(t, line, l)
	require.False(t, u.subagentView.chat.Follow())
	u.closeSubagentView()
}

func TestCompactUsageShowsConfiguredCompactionTokens(t *testing.T) {
	t.Parallel()
	ws := newLoadWorkspace()
	ws.cfg.Options.AutoCompactTokenLimit = 325000
	u := newLoadTestUI(t, ws)
	u.agentReady = false
	u.forceCompactMode, u.isCompact = true, true
	u.session = &session.Session{ID: "old", PromptTokens: 32000, CompletionTokens: 1250}
	require.Equal(t, "  33.25k/325k tokens · Context 90% left", ansi.Strip(u.compactUsage(80)))
	u.session.EstimatedUsage = true
	require.Equal(t, "  ~33.25k/325k tokens · Context ~90% left", ansi.Strip(u.compactUsage(80)))
	u.updateLayoutAndSize()
	require.Positive(t, u.layout.usage.Dy())
	require.Equal(t, u.layout.editor.Max.Y, u.layout.usage.Min.Y)
	u.isCompact = false
	require.Empty(t, u.compactUsage(80))
	ws.cfg.Options = nil
	u.isCompact = true
	require.Contains(t, ansi.Strip(u.compactUsage(80)), "/400k tokens")
}
