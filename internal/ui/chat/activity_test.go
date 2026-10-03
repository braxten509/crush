package chat

import (
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestToolGroupActivityTimer(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	g := NewToolGroupItem(&sty)
	g.live, g.busy = true, true
	now := time.Now()
	call := message.ToolCall{ID: "first", Name: "bash", Input: `{"command":"sleep 30"}`, Finished: true}
	tool := NewToolMessageItem(&sty, "message", call, nil, false, "")
	g.SetChildren([]MessageItem{tool})
	g.syncActivity(now)
	require.Equal(t, "", g.activity.elapsed(now))
	g.syncActivity(now.Add(20 * time.Second))
	require.Equal(t, "20s", g.activity.elapsed(now.Add(20*time.Second)))
	require.Contains(t, ansi.Strip(g.header(120)), "Running a command")
	require.Regexp(t, `Running a command\.* *$`, ansi.Strip(g.header(120)))

	tool.SetResult(&message.ToolResult{ToolCallID: call.ID, Content: "done"})
	g.syncActivity(now.Add(21 * time.Second))
	require.Equal(t, "Thinking", g.status())
	require.Equal(t, "", g.activity.elapsed(now.Add(21*time.Second)))
	msg := &message.Message{ID: "thinking", Role: message.Assistant, Activity: "thinking"}
	assistant := NewAssistantMessageItem(&sty, msg).(*AssistantMessageItem)
	g.SetChildren([]MessageItem{tool, assistant})
	msg.ActivityAt = now.Add(30 * time.Second).Unix()
	g.syncActivity(now.Add(30 * time.Second))
	require.Equal(t, now.Add(21*time.Second), g.activity.started, "heartbeats must not restart thinking")
	require.Empty(t, g.activity.elapsed(now.Add(30*time.Second)))

	call.ID = "second"
	g.SetChildren([]MessageItem{tool, assistant, NewToolMessageItem(&sty, "message2", call, nil, false, "")})
	g.syncActivity(now.Add(31 * time.Second))
	require.Equal(t, "", g.activity.elapsed(now.Add(31*time.Second)))
	call.ID = "third"
	g.SetChildren([]MessageItem{NewToolMessageItem(&sty, "message3", call, nil, false, "")})
	g.syncActivity(now.Add(40 * time.Second))
	require.Equal(t, "", g.activity.elapsed(now.Add(40*time.Second)), "consecutive commands get separate timers")
	g.SetLive(false)
	require.Empty(t, g.activity.elapsed(now.Add(41*time.Second)))
}

func TestAssistantActivityTimer(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	msg := &message.Message{ID: "active", Role: message.Assistant, Activity: "thinking"}
	a := NewAssistantMessageItem(&sty, msg).(*AssistantMessageItem)
	now := time.Now()
	a.activity.started = now.Add(-65 * time.Second)
	msg.ActivityAt = now.Unix()
	a.SetMessage(msg)
	require.Equal(t, "1m 5s", a.activity.elapsed(now))
	require.Contains(t, ansi.Strip(a.renderSpinning()), "1m 5s")

	msg.IsCompacting = true
	a.syncActivity(now)
	require.Equal(t, "", a.activity.elapsed(now))
	a.activity.started = now.Add(-38 * time.Second)
	require.Equal(t, "Compacting conversation.   38s", ansi.Strip(a.renderSpinning()))

	msg.IsCompacting = false
	a.syncActivity(now)
	require.Equal(t, "", a.activity.elapsed(now))
	msg.AddFinish(message.FinishReasonEndTurn, "", "")
	a.syncActivity(now)
	require.Empty(t, a.activity.elapsed(now))
}

func TestAssistantDistinguishesThinkingAndWaiting(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	msg := &message.Message{ID: "active", Role: message.Assistant}
	item := NewAssistantMessageItem(&sty, msg).(*AssistantMessageItem)
	require.Contains(t, ansi.Strip(item.renderSpinning()), "Working")
	msg.AppendContent("I will check.")
	msg.Activity, msg.ActivityAt = "thinking", time.Now().Unix()
	require.Contains(t, ansi.Strip(item.renderSpinning()), "Thinking")
	msg.IsCompacting = true
	require.Contains(t, ansi.Strip(item.renderSpinning()), "Compacting conversation")
}

func TestActivityTimerVisibilityThreshold(t *testing.T) {
	t.Parallel()
	now := time.Unix(1000, 0)
	var timer activityTimer
	timer.update("Thinking", now)
	require.Empty(t, timer.elapsed(now.Add(9999*time.Millisecond)))
	require.Equal(t, "10s", timer.elapsed(now.Add(10*time.Second)))
	timer.update("Running a command", now.Add(11*time.Second))
	require.Empty(t, timer.elapsed(now.Add(11*time.Second)))
}
