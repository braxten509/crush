package cliagent

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestForegroundCommandsFollowToolLifecycle(t *testing.T) {
	t.Parallel()
	calls := &openCalls{calls: map[string]openCall{}}
	p := &proc{cmd: &exec.Cmd{Process: &os.Process{Pid: -123}}}
	stop := p.watchCancel(context.WithValue(context.Background(), openCallsKey{}, calls), func() {})
	t.Cleanup(stop)
	emit := calls.track(func(Event) error { return nil })
	require.NoError(t, emit(Event{Type: EventToolCall, ID: "one", Input: `{"command":"sleep 30"}`}))
	started := time.Now()
	for _, args := range [][]string{{"/usr/bin/sleep", "30"}, {"bash", "-c", "sleep 30"}} {
		require.True(t, IsForegroundCommand(-123, args, started))
		require.False(t, IsForegroundCommand(-124, args, started), "another CLI's job stays in the background list")
		require.False(t, IsForegroundCommand(-123, args, started.Add(-time.Minute)), "an older job with the same command stays in the background list")
	}
	require.False(t, IsForegroundCommand(-123, []string{"sleep", "31"}, started))
	require.NoError(t, emit(Event{Type: EventToolResult, ID: "one", Output: "Moved to the background"}))
	require.False(t, IsForegroundCommand(-123, []string{"sleep", "30"}, started), "a yielded tool is no longer foreground")
}

func TestForegroundQuotedEvalKeepsStartTimeCheck(t *testing.T) {
	t.Parallel()
	command := "while :; do printf 'fixture' >/dev/null; sleep 30; done"
	at := time.Now()
	calls := &openCalls{calls: map[string]openCall{"one": {command: command, at: at}}}
	foregroundCalls.Store(-125, calls)
	t.Cleanup(func() { foregroundCalls.Delete(-125) })
	for _, escape := range []string{`'"'"'`, `'\''`} {
		args := []string{"sh", "-c", "eval '" + strings.ReplaceAll(command, "'", escape) + "'"}
		require.Equal(t, command, CommandText(args))
		require.True(t, IsForegroundCommand(-125, args, at))
		require.False(t, IsForegroundCommand(-125, args, at.Add(-time.Minute)))
	}
}

func TestForegroundDirectArgumentsDoNotMatchPrefixes(t *testing.T) {
	t.Parallel()
	call := openCall{command: "sleep 3", words: []string{"sleep", "3"}}
	require.False(t, call.runBy([]string{"sleep", "30"}))
	require.True(t, call.runBy([]string{"sleep", "3"}))
}
