package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"charm.land/catwalk/v3/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/stretchr/testify/require"
)

func TestTaskRequestRoundTrip(t *testing.T) {
	t.Parallel()
	h := &taskHub{dir: t.TempDir(), tasks: map[string]*Task{}}
	go h.watch()
	t.Cleanup(func() { _ = os.RemoveAll(h.dir) })

	data, _ := json.Marshal(TaskRequest{Session: "s1", Stop: "t9"})
	require.NoError(t, os.WriteFile(filepath.Join(h.dir, "x.req"), data, 0o600))
	var reply TaskReply
	require.Eventually(t, func() bool {
		b, err := os.ReadFile(filepath.Join(h.dir, "x.ack"))
		return err == nil && json.Unmarshal(b, &reply) == nil
	}, 3*time.Second, 50*time.Millisecond)
	require.NotEmpty(t, reply.Error)
	_, err := os.Stat(filepath.Join(h.dir, "x.req"))
	require.True(t, os.IsNotExist(err), "the request is consumed")
}

func TestTaskReplyDoesNotFollowSymlinks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim")
	require.NoError(t, os.WriteFile(victim, []byte("unchanged"), 0o600))
	require.NoError(t, os.Symlink(victim, filepath.Join(dir, "attack.tmp")))
	require.NoError(t, os.Symlink(victim, filepath.Join(dir, "attack.ack")))
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	defer root.Close()
	require.NoError(t, writeTaskReply(root, "attack", []byte("reply")))
	data, err := os.ReadFile(victim)
	require.NoError(t, err)
	require.Equal(t, "unchanged", string(data))
	data, err = os.ReadFile(filepath.Join(dir, "attack.ack"))
	require.NoError(t, err)
	require.Equal(t, "reply", string(data))
}

func TestTaskWatcherUsesPinnedDirectory(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	dir := filepath.Join(parent, "requests")
	require.NoError(t, os.Mkdir(dir, 0o700))
	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	h := &taskHub{dir: dir, root: root, tasks: map[string]*Task{}}
	require.NoError(t, os.Rename(dir, dir+"-original"))
	require.NoError(t, os.Mkdir(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "replaced.req"), []byte(`{"stop":"missing"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir+"-original", "pinned.req"), []byte(`{"stop":"missing"}`), 0o600))
	done := make(chan struct{})
	go func() { h.watch(); close(done) }()
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(dir+"-original", "pinned.ack"))
		return err == nil
	}, 3*time.Second, 10*time.Millisecond)
	_, err = os.Stat(filepath.Join(dir, "replaced.ack"))
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, os.RemoveAll(dir))
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watcher did not stop")
	}
}

func TestAPIProvidersAvailableForBackgroundTasks(t *testing.T) {
	t.Setenv("CRUSH_GLOBAL_CONFIG", t.TempDir())
	h := &taskHub{c: &coordinator{cfg: config.NewTestStore(&config.Config{Providers: csync.NewMapFrom(map[string]config.ProviderConfig{
		"abacus":      {ID: config.AbacusProviderID, Type: catwalk.TypeCompletions, Models: []catwalk.Model{{ID: "claude-opus-5-5-thinking"}}},
		"gemini":      {ID: "gemini", Type: catwalk.Type("google"), Models: []catwalk.Model{{ID: "gemini-3.8-flash", Reasoning: catwalk.Reasoning{Thinking: catwalk.ThinkingToggleable, EffortLevels: catwalk.NewEffortLevels("low", "medium", "high")}}}},
		"claude-code": {ID: "claude-code", Type: config.TypeClaudeCode, Models: []catwalk.Model{{ID: "opus"}}},
		"kimi-coding": {ID: "kimi-coding", Type: catwalk.TypeMessages, FlatRate: true, Models: []catwalk.Model{{ID: "k3.1-pro"}}},
		"off":         {ID: "off", Type: catwalk.TypeResponses, Disable: true, Models: []catwalk.Model{{ID: "gpt"}}},
		"empty":       {ID: "empty", Type: catwalk.TypeResponses},
	})})}}
	var names []string
	for _, p := range h.taskProviders() {
		names = append(names, taskProviderName(p))
	}
	require.Equal(t, []string{"abacus", "claude", "gemini", "kimi-coding"}, names, "API providers join the agent CLIs; disabled and model-less ones don't")

	gemini, _ := h.c.cfg.Config().Providers.Get("gemini")
	_, err := taskModel(gemini, gemini.Models[0], TaskRequest{Fast: true})
	require.Error(t, err, "fast mode is not for gemini")
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
	opus := catwalk.Model{ID: "opus", Reasoning: catwalk.Reasoning{Thinking: catwalk.ThinkingToggleable, EffortLevels: catwalk.NewEffortLevels("low", "medium", "high", "xhigh", "max"), DefaultEffortLevel: "high"}}

	m, err := taskModel(claude, opus, TaskRequest{})
	require.NoError(t, err)
	require.Equal(t, "high", m.ReasoningEffort, "no effort keeps the model's default")
	require.Empty(t, m.ServiceTier)

	m, err = taskModel(claude, opus, TaskRequest{Effort: "MAX", Fast: true})
	require.NoError(t, err)
	require.Equal(t, "max", m.ReasoningEffort)
	require.Equal(t, "fast", m.ServiceTier)

	_, err = taskModel(claude, opus, TaskRequest{Effort: "ultra"})
	require.Error(t, err)
	_, err = taskModel(grok, catwalk.Model{ID: "grok-4.7"}, TaskRequest{Effort: "high"})
	require.Error(t, err)
	_, err = taskModel(grok, catwalk.Model{ID: "grok-4.7"}, TaskRequest{Fast: true})
	require.Error(t, err)
	abacus := config.ProviderConfig{ID: config.AbacusProviderID, Type: catwalk.TypeCompletions}
	m, err = taskModel(abacus, catwalk.Model{ID: "claude-opus-5-5-thinking", Reasoning: catwalk.Reasoning{Thinking: catwalk.ThinkingToggleable, EffortLevels: opus.Reasoning.EffortLevels}}, TaskRequest{Effort: "MAX"})
	require.NoError(t, err)
	require.Equal(t, "max", m.ReasoningEffort)
	m, err = taskModel(abacus, catwalk.Model{ID: "gpt-6.1-sol", Reasoning: catwalk.Reasoning{Thinking: catwalk.ThinkingToggleable, EffortLevels: opus.Reasoning.EffortLevels}}, TaskRequest{Effort: "high", Fast: true})
	require.NoError(t, err)
	require.Equal(t, "fast", m.ServiceTier)
}

// Every kind of task result parses back to its name and status and keeps its
// task id; the chat, the phone and agents read them.
func TestTaskResultsParse(t *testing.T) {
	t.Parallel()

	key, process := detachedKey{pid: 42, started: time.Unix(0, 0)}, detachedProc{session: "s", command: "npm run dev"}
	now := time.Now()
	work := []checkInWork{{id: "Job 004", name: "Background process: run", started: now.Add(-time.Minute), ctx: t.Context(), progress: func() string { return "3/10" }}}
	for _, test := range []struct {
		text, name, status, id string
	}{
		{taskNotification(Task{ID: "t3", Name: "review", CLI: "codex", Model: "gpt-6-astra", Status: TaskDone}, "  all good  "), "review", string(TaskDone), "t3"},
		{taskResult{Name: AskName, Status: AskAnswered, Body: "Q1: yes"}.String(), AskName, AskAnswered, ""},
		{detachedNotification(key, process, time.Unix(9, 0)), BackgroundProcessName, "ended", ""},
		{checkInNotification(work, now), "Background process: run", string(TaskRunning), "Job 004"},
		{taskResult{ID: "x", Name: "n", CLI: "c/m", Status: "s", Body: "b"}.String(), "n", "s", "x"},
	} {
		name, status, ok := ParseTaskNotification(test.text)
		require.True(t, ok, test.text)
		require.Equal(t, test.name, name)
		require.Equal(t, test.status, status)
		require.Contains(t, test.text, test.id)
	}
}
