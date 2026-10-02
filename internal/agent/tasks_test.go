package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
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
	require.Contains(t, reply.Error, `no task "t9"`)
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

func TestAbacusAvailableForBackgroundTasks(t *testing.T) {
	provider := config.ProviderConfig{ID: config.AbacusProviderID, Type: catwalk.TypeOpenAICompat, Models: []catwalk.Model{{ID: "claude-opus-5-5-thinking"}}}
	h := &taskHub{c: &coordinator{cfg: config.NewTestStore(&config.Config{Providers: csync.NewMapFrom(map[string]config.ProviderConfig{"abacus": provider})})}}
	providers := h.taskProviders()
	require.Len(t, providers, 1)
	require.Equal(t, "abacus", taskProviderName(providers[0]))
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
	abacus := config.ProviderConfig{ID: config.AbacusProviderID, Type: catwalk.TypeOpenAICompat}
	m, err = taskModel(abacus, catwalk.Model{ID: "claude-opus-5-5-thinking", ReasoningLevels: opus.ReasoningLevels}, TaskRequest{Effort: "MAX"})
	require.NoError(t, err)
	require.Equal(t, "max", m.ReasoningEffort)
	m, err = taskModel(abacus, catwalk.Model{ID: "gpt-6.1-sol", ReasoningLevels: opus.ReasoningLevels}, TaskRequest{Effort: "high", Fast: true})
	require.NoError(t, err)
	require.Equal(t, "fast", m.ServiceTier)
}
