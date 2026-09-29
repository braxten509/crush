package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

func TestTaskRequestRoundTrip(t *testing.T) {
	t.Parallel()
	h := &taskHub{dir: t.TempDir(), tasks: map[string]*Task{}}
	go h.watch()

	data, _ := json.Marshal(TaskRequest{Session: "s1", Stop: "t9"})
	require.NoError(t, os.WriteFile(filepath.Join(h.dir, "x.req"), data, 0o600))
	var reply TaskReply
	require.Eventually(t, func() bool {
		b, err := os.ReadFile(filepath.Join(h.dir, "x.ack"))
		return err == nil && json.Unmarshal(b, &reply) == nil
	}, 3*time.Second, 50*time.Millisecond)
	require.Contains(t, reply.Error, `no task "t9"`)
	_, err := os.Stat(filepath.Join(h.dir, "x.req"))
	require.True(t, os.IsNotExist(err), "the request is consumed")
}

func TestTaskNotificationParses(t *testing.T) {
	t.Parallel()
	text := taskNotification(Task{ID: "t1", Name: "Review auth", CLI: "codex", Model: "gpt", Status: TaskDone}, "all good")
	name, status, ok := ParseTaskNotification(text)
	require.True(t, ok)
	require.Equal(t, "Review auth", name)
	require.Equal(t, "done", status)
	_, _, ok = ParseTaskNotification("hello")
	require.False(t, ok)
}

func TestTaskModelTuning(t *testing.T) {
	t.Parallel()
	claude := config.ProviderConfig{ID: "claude-code", Type: config.TypeClaudeCode}
	grok := config.ProviderConfig{ID: "grok-cli", Type: config.TypeGrokCLI}
	opus := catwalk.Model{ID: "opus", ReasoningLevels: []string{"low", "medium", "high", "xhigh", "max"}, DefaultReasoningEffort: "high"}

	m, err := taskModel(claude, opus, TaskRequest{})
	require.NoError(t, err)
	require.Equal(t, "high", m.ReasoningEffort, "no effort keeps the model's default")
	require.Empty(t, m.ServiceTier)

	m, err = taskModel(claude, opus, TaskRequest{Effort: "MAX", Fast: true})
	require.NoError(t, err)
	require.Equal(t, "max", m.ReasoningEffort)
	require.Equal(t, "fast", m.ServiceTier)

	_, err = taskModel(claude, opus, TaskRequest{Effort: "ultra"})
	require.ErrorContains(t, err, "takes effort low, medium, high, xhigh, max")
	_, err = taskModel(grok, catwalk.Model{ID: "grok-4.7"}, TaskRequest{Effort: "high"})
	require.ErrorContains(t, err, "has no effort levels")
	_, err = taskModel(grok, catwalk.Model{ID: "grok-4.7"}, TaskRequest{Fast: true})
	require.ErrorContains(t, err, "only for claude and codex")
}
