package agent

import (
	"testing"

	"github.com/charmbracelet/crush/internal/memwatch"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

// The chat shows the memory notice once each time memory gets high, as a
// system line the model never sees.
func TestMemoryNoticeOncePerEpisode(t *testing.T) {
	env := testEnv(t)
	sess, err := env.sessions.Create(t.Context(), "session")
	require.NoError(t, err)

	notices := func() []string {
		msgs, err := env.messages.List(t.Context(), sess.ID)
		require.NoError(t, err)
		var out []string
		for _, m := range msgs {
			require.Equal(t, message.System, m.Role)
			out = append(out, m.Content().Text)
		}
		return out
	}

	showMemoryNotice(t.Context(), env.messages, sess.ID, memwatch.Status{Percent: 70})
	require.Empty(t, notices(), "no notice while memory is fine")

	first := memwatch.Status{Percent: 88, High: true, Episode: 1}
	showMemoryNotice(t.Context(), env.messages, sess.ID, first)
	showMemoryNotice(t.Context(), env.messages, sess.ID, memwatch.Status{Percent: 90, High: true, Episode: 1})
	require.Equal(t, []string{first.ChatNotice()}, notices(), "one notice per episode")

	again := memwatch.Status{Percent: 86, High: true, Episode: 2}
	showMemoryNotice(t.Context(), env.messages, sess.ID, again)
	require.Equal(t, []string{first.ChatNotice(), again.ChatNotice()}, notices())
}
