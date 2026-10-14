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
	"github.com/charmbracelet/crush/internal/ui/chat"
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
				// A resize reaches the parent's layout, as in the app.
				u.width = width
				u.updateLayoutAndSize()
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

func TestComposerFooterShowsConfiguredCompactionTokens(t *testing.T) {
	t.Parallel()
	ws := newLoadWorkspace()
	ws.cfg.Options.AutoCompactTokenLimit = 325000
	u := newLoadTestUI(t, ws)
	u.agentReady = false
	u.forceCompactMode, u.isCompact = true, true
	u.session = &session.Session{ID: "old", PromptTokens: 32000, CompletionTokens: 1250}
	require.Equal(t, "  33.25k/325k tokens · Context 90% left", ansi.Strip(u.composerFooter(80)))
	u.session.EstimatedUsage = true
	require.Equal(t, "  ~33.25k/325k tokens · Context ~90% left", ansi.Strip(u.composerFooter(80)))
	u.updateLayoutAndSize()
	require.Positive(t, u.layout.usage.Dy())
	require.Equal(t, u.layout.editor.Max.Y, u.layout.usage.Min.Y)
	u.isCompact = false
	require.Empty(t, u.composerFooter(80))
	ws.cfg.Options = nil
	u.isCompact = true
	require.Contains(t, ansi.Strip(u.composerFooter(80)), "/400k tokens")
}

// A running sub-agent whose last step is done is still working out its next
// one, so its status group shows "Thinking" like the main chat does.
func TestSubagentViewShowsThinkingWhileRunning(t *testing.T) {
	t.Parallel()
	ws := newLoadWorkspace()
	u := newLoadTestUI(t, ws)
	call := message.ToolCall{ID: "c1", Name: "bash", Input: `{"command":"ls"}`, Finished: true}
	asst := message.Message{ID: "a1", SessionID: "child", Role: message.Assistant}
	asst.AddToolCall(call)
	asst.AddFinish(message.FinishReasonToolUse, "", "")
	tool := message.Message{ID: "r1", SessionID: "child", Role: message.Tool}
	tool.AddToolResult(message.ToolResult{ToolCallID: "c1", Content: "ok"})
	ws.transcripts["child"] = []message.Message{asst, tool}
	task := agent.Task{ID: "t1", SessionID: u.session.ID, ChildID: "child", Name: "Review", Status: agent.TaskRunning}
	u.handleSubagentView(u.openSubagentView(task)())
	draw := func() string {
		scr := uv.NewScreenBuffer(100, 24)
		u.Draw(scr, scr.Bounds())
		return ansi.Strip(scr.Render())
	}
	require.Contains(t, draw(), "Thinking", "a running sub-agent between steps shows it is working")

	task.Status = agent.TaskDone
	cmd, _ := u.handleSubagentView(pubsub.Event[agent.Task]{Type: pubsub.UpdatedEvent, Payload: task})
	u.handleSubagentView(cmd())
	u.handleSubagentView(u.loadSubagentView()())
	require.NotContains(t, draw(), "Thinking", "a finished sub-agent stops")
}

// Opening a sub-agent looks like the main chat: the chat sits in the same
// spot, a read-only composer band holds its name, and the way back sits on
// the bottom row.
func TestSubagentViewMatchesMainChatLayout(t *testing.T) {
	t.Parallel()
	for _, width := range []int{100, 160} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			ws := newLoadWorkspace()
			u := newLoadTestUI(t, ws)
			prompt := "Review the packaging work"
			for _, sid := range []string{"old", "child"} {
				msg := message.Message{ID: sid + "-u", SessionID: sid, Role: message.User}
				msg.AppendContent(prompt)
				ws.transcripts[sid] = []message.Message{msg}
			}
			u.chat.SetMessages(chat.ExtractMessageItems(u.com.Styles, &ws.transcripts["old"][0], nil, "")...)
			u.width, u.height = width, 30
			u.updateLayoutAndSize()
			draw := func() []string {
				scr := uv.NewScreenBuffer(width, 30)
				u.Draw(scr, scr.Bounds())
				return strings.Split(ansi.Strip(scr.Render()), "\n")
			}
			find := func(rows []string, text string) (int, int) {
				for y, row := range rows {
					if x := strings.Index(row, text); x >= 0 {
						return y, ansi.StringWidth(row[:x])
					}
				}
				t.Fatalf("%q not drawn:\n%s", text, strings.Join(rows, "\n"))
				return 0, 0
			}
			mainY, mainX := find(draw(), prompt)

			task := agent.Task{ID: "t1", SessionID: u.session.ID, ChildID: "child", Name: "Gemini review", Status: agent.TaskRunning, Model: "gemini-flash"}
			u.handleSubagentView(u.openSubagentView(task)())
			rows := draw()
			subY, subX := find(rows, prompt)
			require.Equal(t, mainX, subX, "the sub-agent's chat starts in the main chat's column")
			require.Equal(t, mainY, subY, "the sub-agent's chat starts on the main chat's row")
			require.NotContains(t, strings.Join(rows, "\n"), "Sub-agent:", "no title row above the chat")
			find(rows, "Gemini review · running")
			find(rows, "read only")
			require.Contains(t, rows[len(rows)-1], "Return to parent", "the way back is on the bottom row")
			_, _, back := u.subagentViewAreas(uv.Rect(0, 0, width, 30))
			require.Equal(t, len(rows)-1, back.Min.Y)
			t.Logf("%d columns:\n%s", width, strings.Join(rows, "\n"))
		})
	}
}
