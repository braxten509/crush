package model

import (
	"testing"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// The sub-agent view's spinners run on their own clock, routed to its chat
// rather than the main one.
func TestSubagentViewAnimates(t *testing.T) {
	t.Parallel()
	u, mainTop, mainBottom := newAnimTestUI(t)
	sub := NewChat(u.com, config.ScrollbarDefault)
	sub.SetSize(80, 20)
	spinner := &spinTestItem{testMessageItem: testMessageItem{id: "sub", text: "running"}, spinning: true}
	sub.SetMessages(spinner)
	sub.SetAnimationsAllowed(true)
	u.subagentView = &subagentView{chat: sub, task: agent.Task{Status: agent.TaskRunning}}

	cmd := sub.EnsureAnimating()
	require.NotNil(t, cmd, "a running sub-agent's visible spinner must arm its clock")
	tick, ok := cmd().(animTickMsg)
	require.True(t, ok)
	require.Same(t, sub, tick.chat)

	next := u.handleAnimTick(tick)
	require.Equal(t, 1, spinner.advances, "the tick must advance the sub-agent's spinner")
	require.NotNil(t, next, "the clock keeps running while the spinner is visible")
	require.Zero(t, mainTop.advances+mainBottom.advances, "the main chat must not move on the sub-agent's tick")

	u.subagentView = nil
	require.Nil(t, u.handleAnimTick(next().(animTickMsg)), "a closed view's clock stops")
}
