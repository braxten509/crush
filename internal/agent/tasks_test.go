package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

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
