package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/stretchr/testify/require"
)

// fixedModelsAgent keeps the models a test gave it when the coordinator
// rebuilds them from config before each turn.
type fixedModelsAgent struct{ SessionAgent }

func (fixedModelsAgent) SetHelperModel(Model)         {}
func (fixedModelsAgent) SetTools([]fantasy.AgentTool) {}

// A headless run whose Claude turn ends while helpers Claude started itself
// are still working must wait for them, as it does for Crush's sub-agents,
// and end on the reply Claude gives once they report.
func TestHeadlessRunWaitsForClaudeHelpers(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is required for the fake native CLI")
	}
	delay := waitReportDelay
	waitReportDelay = 20 * time.Millisecond
	t.Cleanup(func() { waitReportDelay = delay })

	dir := t.TempDir()
	script := `#!` + python + `
import json, os, time
root = os.environ["CRUSH_HELPER_FIXTURE"]
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
def text(value):
    emit({"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":value}}})
read() # Initialization control request.
read() # The prompt.
emit({"type":"system","subtype":"init","session_id":"helpers"})
text("Starting a helper.")
emit({"type":"system","subtype":"task_started","task_id":"helper","task_type":"local_agent","is_backgrounded":True})
emit({"type":"result","subtype":"success","result":"Starting a helper."})
while not os.path.exists(os.path.join(root, "finish")):
    time.sleep(.005)
emit({"type":"system","subtype":"task_notification","task_id":"helper","status":"completed"})
emit({"type":"system","subtype":"init","session_id":"helpers"})
text("helpers reported")
emit({"type":"result","subtype":"success","result":"helpers reported"})
while True:
    read()
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	provider := cliagent.NewProvider(config.TypeClaudeCode, dir, dir, nil, nil, "", false, "", false)
	model, err := provider.LanguageModel(t.Context(), "fixture")
	require.NoError(t, err)
	model.(*cliagent.Model).Env = []string{"CRUSH_HELPER_FIXTURE=" + dir}

	c := newGateTestCoordinator(t, false)
	require.NoError(t, c.readyWg.Wait())
	env := fakeEnv{sessions: c.sessions, messages: c.messages}
	sa := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system")
	// The coordinator looks up each turn's provider in its config.
	limits := catwalk.Model{ContextWindow: 200000, DefaultMaxTokens: 10000}
	selected := config.SelectedModel{Provider: "mock", Model: "mock-model"}
	setTestModels(sa, Model{Model: model, CatwalkCfg: limits, ModelCfg: selected}, Model{Model: &finishStreamModel{text: "title"}, CatwalkCfg: limits, ModelCfg: selected})
	c.mainAgent = fixedModelsAgent{sa}
	c.tasks = &taskHub{c: c, tasks: make(map[string]*Task)}
	broker := pubsub.NewBroker[notify.RunComplete]()
	c.runComplete = broker
	t.Cleanup(broker.Shutdown)
	t.Cleanup(c.watchCLIReplies())
	sess, err := c.sessions.Create(t.Context(), "helpers")
	require.NoError(t, err)
	t.Cleanup(func() { _ = cliagent.CloseForTree(sess.ID) })

	ctx := t.Context()
	statuses := make(chan bool, 8)
	type outcome struct {
		result *fantasy.AgentResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := c.Run(WithRunStatus(ctx, func(waiting bool) { statuses <- waiting }), sess.ID, "start a helper")
		done <- outcome{result, err}
	}()

	select {
	case waiting := <-statuses:
		require.True(t, waiting)
	case got := <-done:
		t.Fatalf("the run ended while Claude's helper was still working (error: %v)", got.err)
	case <-time.After(10 * time.Second):
		t.Fatal("waiting was never reported")
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "finish"), nil, 0o600))
	select {
	case waiting := <-statuses:
		require.False(t, waiting)
	case <-time.After(10 * time.Second):
		t.Fatal("the end of waiting was never reported")
	}
	select {
	case got := <-done:
		require.NoError(t, got.err)
		require.Contains(t, got.result.Response.Content.Text(), "helpers reported")
	case <-time.After(10 * time.Second):
		t.Fatal("the run did not end after the helper reported")
	}
}
