//go:build linux

package cliagent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

func TestCodexSteerSurvivesBackgroundTransition(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
read -r _; echo '{"id":"1","result":{}}'
read -r _; read -r _; echo '{"id":"2","result":{"thread":{"id":"th"}}}'
read -r _; echo '{"id":"3","result":{"turn":{"id":"tu"}}}'
echo '{"method":"item/started","params":{"item":{"type":"commandExecution","id":"c1","command":"echo fixture"}}}'
read -r _
echo '{"method":"turn/completed","params":{"turn":{"id":"tu","status":"interrupted"}}}'
read -r _; echo '{"id":"ctrl-b-list","result":{"data":[]}}'
read -r _; echo '{"id":"3","result":{"turn":{"id":"next"}}}'
read -r steer
case "$steer" in *expectedTurnId*next*) ;; *) exit 1;; esac
case "$steer" in *steer*fixture*) ;; *) exit 1;; esac
echo '{"method":"item/started","params":{"item":{"type":"userMessage","id":"u2","content":[{"type":"text","text":"steer fixture"}]}}}'
echo '{"method":"turn/completed","params":{"turn":{"id":"next","status":"completed"}}}'
cat >/dev/null
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ready := make(chan struct{}, 1)
	entered, release := make(chan struct{}), make(chan struct{})
	model := &Model{Kind: config.TypeCodexCLI, ID: "fixture", Dir: dir, Guarded: true}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	consumed, confirmed := 0, 0
	err := model.Run(ctx, Turn{SessionID: "steer-transition", Prompt: "fixture", SteerReady: ready,
		SteerInput: func() (string, []message.Attachment) {
			consumed++
			if consumed > 1 {
				return "", nil
			}
			close(entered)
			<-release
			return "steer fixture", nil
		},
		Emit: func(event Event) error {
			if event.Type == EventToolCall {
				ready <- struct{}{}
				<-entered
				require.True(t, Background("steer-transition"))
			}
			if event.Type == EventToolResult {
				close(release)
			}
			if event.Type == EventUserMessage {
				confirmed++
				require.Equal(t, "steer fixture", event.Text)
			}
			return nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, 1, consumed, "retained input must be retried without draining the queue again")
	require.Equal(t, 1, confirmed)
}

func blockedStdin(t *testing.T) (*proc, <-chan error) {
	t.Helper()
	p, err := startProc(t.TempDir(), "sh", "-c", "sleep 30")
	require.NoError(t, err)
	t.Cleanup(p.kill)
	sent := make(chan error, 1)
	go func() { sent <- p.send(map[string]string{"text": strings.Repeat("x", 1<<20)}) }()
	require.Eventually(t, func() bool {
		if p.mu.TryLock() {
			p.mu.Unlock()
			return false
		}
		return true
	}, time.Second, 10*time.Millisecond)
	return p, sent
}

func TestFinishUnblocksStdinWrite(t *testing.T) {
	p, sent := blockedStdin(t)
	finished := make(chan struct{})
	go func() { p.finish(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(6 * time.Second):
		p.kill()
		<-finished
		t.Fatal("finish exceeded its shutdown watchdog")
	}
	require.Error(t, <-sent)
}

func TestCancelWatchdogSurvivesBlockedInterrupt(t *testing.T) {
	p, sent := blockedStdin(t)
	ctx, cancel := context.WithCancel(t.Context())
	interrupted := make(chan struct{})
	stop := p.watchCancel(ctx, func() {
		_ = p.send(map[string]string{"interrupt": "fixture"})
		close(interrupted)
	})
	t.Cleanup(stop)
	cancel()
	select {
	case <-interrupted:
	case <-time.After(6 * time.Second):
		p.kill()
		<-interrupted
		t.Fatal("blocked interrupt prevented the cancellation watchdog")
	}
	require.Error(t, <-sent)
	p.finish()
}

func TestKillCommandsDecodesQuotedEval(t *testing.T) {
	for _, escape := range []string{`'"'"'`, `'\''`} {
		t.Run(escape, func(t *testing.T) {
			command := "while :; do printf 'fixture' >/dev/null; sleep 30; done"
			wrapper := "eval '" + strings.ReplaceAll(command, "'", escape) + "'"
			p, err := startProcEnv(t.TempDir(), []string{"CRUSH_TEST_WRAPPER=" + wrapper}, "sh", "-c", `setsid sh -c "$CRUSH_TEST_WRAPPER" & echo $!; wait`)
			require.NoError(t, err)
			require.True(t, p.lines.Scan())
			pid, err := strconv.Atoi(p.lines.Text())
			require.NoError(t, err)
			t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL); p.kill(); p.finish() })
			calls := &openCalls{calls: map[string]openCall{}}
			require.NoError(t, calls.track(func(Event) error { return nil })(Event{Type: EventToolCall, ID: "fixture", Name: "bash", Input: marshal(map[string]string{"command": command})}))
			require.Eventually(t, func() bool {
				args, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
				return strings.Contains(string(args), "eval '")
			}, time.Second, 10*time.Millisecond)
			p.killCommands(calls.list())
			require.Eventually(t, func() bool { return syscall.Kill(pid, 0) != nil }, time.Second, 10*time.Millisecond)
		})
	}
}

func TestLinksSerializeSeparateProviders(t *testing.T) {
	dir := t.TempDir()
	const count = 64
	start := make(chan struct{})
	var wait sync.WaitGroup
	errors := make(chan error, count)
	for i := range count {
		provider := NewProvider(config.TypeCodexCLI, dir, dir, nil, nil, "", false).(*provider)
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			errors <- provider.links.Set(strconv.Itoa(i), config.TypeCodexCLI, Link{Native: strconv.Itoa(i)})
		}()
	}
	close(start)
	wait.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	links := &Links{path: filepath.Join(dir, "cli-sessions.json")}
	require.Len(t, links.load(), count)
	for i := range count {
		require.Equal(t, strconv.Itoa(i), links.Get(strconv.Itoa(i), config.TypeCodexCLI).Native)
	}
}

func TestClaudeSteerTimeoutIgnoresUnrelatedOutput(t *testing.T) {
	for _, output := range []string{
		`{"type":"control_response","response":{"subtype":"success","request_id":"unrelated"}}`,
		`{"type":"system","subtype":"task_notification","task_id":"unrelated","status":"completed"}`,
		`not JSON`,
	} {
		t.Run(output, func(t *testing.T) {
			dir := t.TempDir()
			script := `#!/bin/sh
read -r _; read -r _
echo '{"type":"system","subtype":"init","session_id":"s1"}'
read -r _
echo '{"type":"result","subtype":"success"}'
cat <<'OUTPUT'
` + output + `
OUTPUT
cat >/dev/null
`
			require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0755))
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			previous := steerEchoTimeout
			steerEchoTimeout = 40 * time.Millisecond
			t.Cleanup(func() { steerEchoTimeout = previous })
			ready := make(chan struct{}, 1)
			given := false
			model := &Model{Kind: config.TypeClaudeCode, ID: "fixture", Dir: dir}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			confirmed := 0
			err := model.Run(ctx, Turn{Prompt: "fixture", SteerReady: ready, Steer: func() string {
				if given {
					return ""
				}
				given = true
				return "steer fixture"
			}, Emit: func(event Event) error {
				if event.Type == EventSession {
					ready <- struct{}{}
				}
				if event.Type == EventUserMessage {
					confirmed++
				}
				return nil
			}})
			require.NoError(t, err, "pending input must time out before context cancellation")
			require.Equal(t, 0, confirmed, "unconfirmed input stays available for the next turn")
		})
	}
}

func TestAGYCredentialLookupUsesFetchDeadline(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "secret-tool"), []byte("#!/bin/sh\nsleep 30 &\nwait\n"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agy"), []byte("#!/bin/sh\nexit 1\n"), 0755))
	started := time.Now()
	_, err := fetchAGYLimits(context.Background())
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(started), 17*time.Second)
}

func TestRefreshLimitsCoalescesPendingFetch(t *testing.T) {
	kind := config.TypeAGYCLI
	previous := limitFetchers[kind]
	limitStore.Lock()
	previousTime := limitStore.fetched[kind]
	delete(limitStore.fetched, kind)
	limitStore.Unlock()
	t.Cleanup(func() {
		limitFetchers[kind] = previous
		limitStore.Lock()
		if limitStore.fetched == nil {
			limitStore.fetched = map[catwalk.Type]time.Time{}
		}
		limitStore.fetched[kind] = previousTime
		limitStore.Unlock()
	})
	entered, release := make(chan struct{}), make(chan struct{})
	limitFetchers[kind] = func(context.Context) ([]Limit, error) {
		close(entered)
		<-release
		return nil, context.Canceled
	}
	finished := make(chan bool, 1)
	go func() { finished <- RefreshLimits(t.Context(), kind) }()
	<-entered
	require.False(t, RefreshLimits(t.Context(), kind))
	close(release)
	require.False(t, <-finished)
}

func TestGrokBillingRejectsEmptyPeriodName(t *testing.T) {
	limits, err := grokBilling(json.RawMessage(`{"config":{"currentPeriod":{"type":"USAGE_PERIOD_TYPE_"}}}`))
	require.Error(t, err)
	require.Nil(t, limits)
}
