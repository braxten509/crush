package dialog

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestBackgroundDialogsStaySeparate(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	com := &common.Common{Styles: &sty}
	tasks := []agent.Task{
		{ID: "t1", Name: "Review auth", CLI: "codex", Model: "gpt-5.6-sol", Effort: "medium", Status: agent.TaskRunning, Started: time.Now()},
		{ID: "t2", Name: "Finished task", Status: agent.TaskDone},
	}
	procs := []agent.Process{{PID: 42, Command: "sleep 900", Started: time.Now()}}
	for _, b := range []*Background{NewSubAgents(com, tasks, "t1"), NewBackground(com, procs)} {
		t.Run(b.ID(), func(t *testing.T) {
			// Refresh with both sources, as the UI does while the dialog is open.
			b.SetItems(tasks, procs, "")
			require.Len(t, b.items, 1)
			scr := uv.NewScreenBuffer(120, 30)
			b.Draw(scr, scr.Bounds())
			out := ansi.Strip(scr.Render())
			t.Log("\n" + out)
			if b.ID() == SubAgentsID {
				require.Contains(t, out, "Sub-agents")
				require.Contains(t, out, "Review auth")
				require.Contains(t, out, "codex/gpt-5.6-sol/medium")
				require.NotContains(t, out, "sleep 900")
				require.NotContains(t, out, "Finished task")
			} else {
				require.Contains(t, out, "Background Processes")
				require.Contains(t, out, "sleep 900")
				require.Contains(t, out, "pid 42")
				require.NotContains(t, out, "Review auth")
			}
			b.SetItems(nil, nil, "")
			scr = uv.NewScreenBuffer(120, 30)
			b.Draw(scr, scr.Bounds())
			out = ansi.Strip(scr.Render())
			if b.ID() == SubAgentsID {
				require.Contains(t, out, "No sub-agents are running.")
			} else {
				require.Contains(t, out, "No background processes are running.")
			}
		})
	}
}

func TestSubAgentsSelectionSurvivesRefresh(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	tasks := []agent.Task{
		{ID: "t1", Name: "First", Status: agent.TaskRunning},
		{ID: "t2", Name: "Second", Status: agent.TaskRunning},
	}
	b := NewSubAgents(&common.Common{Styles: &sty}, tasks, "t2")
	require.Equal(t, "t2", b.list.SelectedItem().(*BackgroundItem).ID())
	b.SetItems(tasks, nil, "t1")
	require.Equal(t, "t2", b.list.SelectedItem().(*BackgroundItem).ID())
	b.HandleMsg(tea.KeyPressMsg{Code: tea.KeyUp})
	require.Equal(t, "t1", b.list.SelectedItem().(*BackgroundItem).ID())
	b.SetItems(tasks[1:], nil, "")
	require.Equal(t, "t2", b.list.SelectedItem().(*BackgroundItem).ID())
}

func TestSubAgentWithoutEffort(t *testing.T) {
	t.Parallel()
	i := &BackgroundItem{task: &agent.Task{CLI: "claude", Model: "haiku", Started: time.Now()}}
	require.Contains(t, i.info(), "claude/haiku · ")
	require.NotContains(t, i.info(), "haiku/")
}

func TestEnterOpensSelectedSubagentChat(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	tasks := []agent.Task{
		{ID: "t1", SessionID: "parent", ChildID: "child-1", Status: agent.TaskRunning},
		{ID: "t2", SessionID: "parent", ChildID: "child-2", Status: agent.TaskRunning},
	}
	com := &common.Common{Styles: &sty}
	b := NewSubAgents(com, tasks, "t2")
	action := b.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, ActionViewSubAgent{Task: tasks[1]}, action)
	require.Len(t, b.items, 2, "opening a conversation must not stop the task")
	b.SetItems(nil, nil, "")
	require.Nil(t, b.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}))
	processes := NewBackground(com, []agent.Process{{PID: 42}})
	require.Nil(t, processes.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}))
}

func TestManagedBackgroundJobsKeepDistinctSelection(t *testing.T) {
	sty := styles.CharmtonePantera()
	com := &common.Common{Styles: &sty}
	jobs := []agent.Process{{JobID: "001", Command: "first", Started: time.Now()}, {JobID: "002", Command: "second", Started: time.Now()}}
	dialog := NewBackground(com, jobs)
	dialog.list.SetSelected(1)
	require.Equal(t, "job:002", dialog.list.SelectedItem().(*BackgroundItem).ID())
	dialog.SetItems(nil, []agent.Process{jobs[1], jobs[0]}, "")
	selected := dialog.list.SelectedItem().(*BackgroundItem)
	require.Equal(t, "job:002", selected.ID())
	require.Contains(t, selected.info(), "job 002")
	require.NotContains(t, selected.info(), "pid 0")
}
