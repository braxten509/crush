package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

// Exercise the real Claude driver while a fake CLI holds a tool open and then
// starts another tool. Queued input must arrive between these tools.
func TestCLIQueueWaitsForToolCompletionOrInterrupt(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is required for the fake native CLI")
	}
	for _, interrupt := range []bool{false, true} {
		name := "natural"
		if interrupt {
			name = "interrupt"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			script := `#!` + python + `
import json, os, time
root = os.environ["CRUSH_QUEUE_FIXTURE"]
def read():
    data = b""
    while not data.endswith(b"\n"):
        byte = os.read(0, 1)
        if not byte:
            raise SystemExit(0)
        data += byte
    return data.decode()
def emit(value):
    print(json.dumps(value), flush=True)
def mark(name):
    open(os.path.join(root, name), "w").close()
def wait(name):
    while not os.path.exists(os.path.join(root, name)):
        time.sleep(.005)
def capture(value):
    with open(os.path.join(root, "inputs"), "a") as output:
        output.write(value)
def tool(number):
    emit({"type":"assistant","message":{"content":[{"type":"tool_use","id":str(number),"name":"Bash","input":{"command":"echo fixture"}}]}})
def tool_done(number):
    emit({"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":str(number),"content":"done"}]}})
read() # Initialization control request.
prompt = read()
capture(prompt)
emit({"type":"system","subtype":"init","session_id":"queue-fixture"})
if "ACTIVE_QUEUE_FIXTURE" in prompt and "QUEUED_FIRST" not in prompt:
    tool(1)
    mark("started")
    wait("probe")
    prompt = read()
    capture(prompt)
    mark("observed")
    wait("finish")
    tool_done(1)
    if json.loads(prompt).get("priority") != "next":
        wait("end-turn")
    echo = json.loads(prompt)
    echo["isReplay"] = True
    emit(echo)
    mark("delivered")
    tool(2)
    wait("end-turn")
    tool_done(2)
emit({"type":"result","subtype":"success","result":"queued done"})
`
			require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755))
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			provider := cliagent.NewProvider(config.TypeClaudeCode, dir, dir, nil, nil, "", false)
			model, err := provider.LanguageModel(t.Context(), "fixture")
			require.NoError(t, err)
			model.(*cliagent.Model).Env = []string{"CRUSH_QUEUE_FIXTURE=" + dir}
			env := testEnv(t)
			sa := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
			sa.isSubAgent = true
			sess, err := env.sessions.Create(t.Context(), "queue boundary")
			require.NoError(t, err)
			t.Cleanup(func() { sa.Cancel(sess.ID) })
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := sa.Run(ctx, SessionAgentCall{SessionID: sess.ID, Prompt: "ACTIVE_QUEUE_FIXTURE"})
				done <- err
			}()
			waitFile := func(name string) {
				t.Helper()
				require.Eventually(t, func() bool {
					_, err := os.Stat(filepath.Join(dir, name))
					return err == nil
				}, 5*time.Second, 5*time.Millisecond, name)
			}
			waitFile("started")
			for _, prompt := range []string{"QUEUED_FIRST"} {
				_, err := sa.Run(ctx, SessionAgentCall{SessionID: sess.ID, Prompt: prompt, SubmissionID: prompt})
				require.NoError(t, err)
			}
			require.NoError(t, os.WriteFile(filepath.Join(dir, "probe"), nil, 0o600))
			waitFile("observed")
			inputs, err := os.ReadFile(filepath.Join(dir, "inputs"))
			require.NoError(t, err)
			require.Contains(t, string(inputs), "QUEUED_FIRST", "the native queue accepts the message while its tool is running")
			before, err := env.messages.List(t.Context(), sess.ID)
			require.NoError(t, err)
			for _, msg := range before {
				require.Empty(t, msg.Content().SubmissionID, "native input must remain visibly queued until acknowledged at the tool boundary")
			}
			require.Equal(t, []string{"QUEUED_FIRST"}, sa.QueuedPromptsList(sess.ID))
			if interrupt {
				sa.Interrupt(sess.ID)
			} else {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "finish"), nil, 0o600))
				waitFile("delivered")
				require.True(t, sa.IsSessionBusy(sess.ID), "queued input must arrive before the active turn ends")
				require.NoError(t, os.WriteFile(filepath.Join(dir, "end-turn"), nil, 0o600))
			}
			select {
			case err := <-done:
				if interrupt {
					require.ErrorIs(t, err, context.Canceled)
				} else {
					require.NoError(t, err)
				}
			case <-ctx.Done():
				t.Fatal("active turn did not finish")
			}
			require.Eventually(t, func() bool { return !sa.IsSessionBusy(sess.ID) }, 5*time.Second, 5*time.Millisecond)
			inputs, err = os.ReadFile(filepath.Join(dir, "inputs"))
			require.NoError(t, err)
			lines := strings.Split(strings.TrimSpace(string(inputs)), "\n")
			expectedInputs := 2
			if interrupt {
				expectedInputs++
			}
			require.Len(t, lines, expectedInputs, "interrupt must reclaim unacknowledged native input")
			require.Contains(t, lines[1], "QUEUED_FIRST")
			msgs, err := env.messages.List(t.Context(), sess.ID)
			require.NoError(t, err)
			var submissions []string
			for _, msg := range msgs {
				if msg.Role == message.User && msg.Content().SubmissionID != "" {
					submissions = append(submissions, msg.Content().SubmissionID)
				}
			}
			require.Equal(t, []string{"QUEUED_FIRST"}, submissions)
			require.Zero(t, sa.QueuedPrompts(sess.ID))
		})
	}
}

// Exercise the real Claude driver while a fake CLI answers without running
// a tool. With no tool boundary, a prompt queued mid-turn must wait for the
// turn to end (or be interrupted) and then run as its own turn.
func TestCLIQueueWithoutToolsWaitsForTurnEnd(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is required for the fake native CLI")
	}
	for _, interrupt := range []bool{false, true} {
		name := "natural"
		if interrupt {
			name = "interrupt"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			script := `#!` + python + `
import json, os, select, time
root = os.environ["CRUSH_QUEUE_FIXTURE"]
def read():
    data = b""
    while not data.endswith(b"\n"):
        byte = os.read(0, 1)
        if not byte:
            raise SystemExit(0)
        data += byte
    return data.decode()
def emit(value):
    print(json.dumps(value), flush=True)
def capture(value):
    with open(os.path.join(root, "inputs"), "a") as output:
        output.write(value)
read() # Initialization control request.
prompt = read()
capture(prompt)
emit({"type":"system","subtype":"init","session_id":"queue-fixture"})
if "ACTIVE_QUEUE_FIXTURE" in prompt and "QUEUED_FIRST" not in prompt:
    emit({"type":"assistant","message":{"content":[{"type":"text","text":"Still thinking it over"}]}})
    open(os.path.join(root, "started"), "w").close()
    while not os.path.exists(os.path.join(root, "end-turn")):
        # Record anything steered into this turn.
        if select.select([0], [], [], .005)[0]:
            capture(read())
emit({"type":"result","subtype":"success","result":"turn done"})
while True: # The driver keeps the process for later turns.
    capture(read())
    emit({"type":"result","subtype":"success","result":"queued done"})
`
			require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755))
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			provider := cliagent.NewProvider(config.TypeClaudeCode, dir, dir, nil, nil, "", false)
			model, err := provider.LanguageModel(t.Context(), "fixture")
			require.NoError(t, err)
			model.(*cliagent.Model).Env = []string{"CRUSH_QUEUE_FIXTURE=" + dir}
			env := testEnv(t)
			sa := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
			sa.isSubAgent = true
			sess, err := env.sessions.Create(t.Context(), "queue boundary")
			require.NoError(t, err)
			t.Cleanup(func() { sa.Cancel(sess.ID) })
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := sa.Run(ctx, SessionAgentCall{SessionID: sess.ID, Prompt: "ACTIVE_QUEUE_FIXTURE"})
				done <- err
			}()
			require.Eventually(t, func() bool {
				_, err := os.Stat(filepath.Join(dir, "started"))
				return err == nil
			}, 5*time.Second, 5*time.Millisecond)
			_, err = sa.Run(ctx, SessionAgentCall{SessionID: sess.ID, Prompt: "QUEUED_FIRST", SubmissionID: "QUEUED_FIRST"})
			require.NoError(t, err)
			// Longer than the old steering poll, so a steer would have landed.
			time.Sleep(400 * time.Millisecond)
			inputs, err := os.ReadFile(filepath.Join(dir, "inputs"))
			require.NoError(t, err)
			require.NotContains(t, string(inputs), "QUEUED_FIRST", "with no tool running the queued prompt must not reach the turn")
			require.Equal(t, []string{"QUEUED_FIRST"}, sa.QueuedPromptsList(sess.ID))
			if interrupt {
				sa.Interrupt(sess.ID)
			} else {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "end-turn"), nil, 0o600))
			}
			select {
			case err := <-done:
				if interrupt {
					require.ErrorIs(t, err, context.Canceled)
				} else {
					require.NoError(t, err)
				}
			case <-ctx.Done():
				t.Fatal("active turn did not finish")
			}
			require.Eventually(t, func() bool {
				inputs, _ := os.ReadFile(filepath.Join(dir, "inputs"))
				return !sa.IsSessionBusy(sess.ID) && strings.Contains(string(inputs), "QUEUED_FIRST")
			}, 5*time.Second, 5*time.Millisecond, "the held prompt runs once the turn ends")
			msgs, err := env.messages.List(t.Context(), sess.ID)
			require.NoError(t, err)
			var submissions []string
			for _, msg := range msgs {
				if msg.Role == message.User && msg.Content().SubmissionID != "" {
					submissions = append(submissions, msg.Content().SubmissionID)
				}
			}
			require.Equal(t, []string{"QUEUED_FIRST"}, submissions)
			require.Zero(t, sa.QueuedPrompts(sess.ID))
		})
	}
}
