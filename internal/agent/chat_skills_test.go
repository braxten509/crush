package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/stretchr/testify/require"
)

func writeTestSkill(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name, "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("---\nname: "+name+"\ndescription: Test skill "+name+".\n---\nFollow "+name+".\n"), 0o644))
	return path
}

// Agents turn chat skills on and off with `crush skill`; they may turn off
// only toggleable skills, so a skill the user set to plain on stays. Each change leaves a notice in the chat
// that never reaches the model.
func TestAgentTurnsChatSkillsOnAndOff(t *testing.T) {
	c := sessionRunTestCoordinator(t, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		return fakeSessionReply(call, "ok"), nil
	})
	dir := t.TempDir()
	teach, drawio := writeTestSkill(t, dir, "teach"), writeTestSkill(t, dir, "drawio")
	c.cfg.Config().Options.SkillsPaths = []string{dir}
	sess, err := c.sessions.Create(t.Context(), "chat")
	require.NoError(t, err)
	h := c.tasks
	send := func(name string, on bool) TaskReply {
		data, err := json.Marshal(TaskRequest{Session: sess.ID, Skill: &SkillRequest{Name: name, On: on}})
		require.NoError(t, err)
		return h.handle(data)
	}
	kept := func() []session.KeptSkill {
		got, err := c.sessions.Get(t.Context(), sess.ID)
		require.NoError(t, err)
		return got.KeptSkills
	}
	lastNotice := func() *message.Message {
		msgs, err := c.messages.List(t.Context(), sess.ID)
		require.NoError(t, err)
		require.NotEmpty(t, msgs)
		last := &msgs[len(msgs)-1]
		require.Equal(t, message.System, last.Role)
		require.Empty(t, last.ToAIMessage(), "the notice is for the user only")
		return last
	}

	reply := send("Teach", true)
	require.Empty(t, reply.Error)
	require.Contains(t, reply.Message, teach, "the agent learns where to read the skill now")
	require.Equal(t, []session.KeptSkill{{ID: teach, Name: "teach", ByAgent: true}}, kept())
	require.Equal(t, "teach turned on by the agent", lastNotice().Content().Text)

	reply = send("teach", false)
	require.Empty(t, reply.Error)
	require.Empty(t, kept())
	require.Equal(t, "teach turned off by the agent", lastNotice().Content().Text)

	_, err = c.sessions.SetKeptSkill(t.Context(), sess.ID, session.KeptSkill{ID: drawio, Name: "drawio"}, true)
	require.NoError(t, err)
	reply = send("drawio", false)
	require.Contains(t, reply.Error, "the user turned drawio on")
	require.Equal(t, []session.KeptSkill{{ID: drawio, Name: "drawio"}}, kept())

	_, err = c.sessions.SetKeptSkill(t.Context(), sess.ID, session.KeptSkill{ID: drawio}, false)
	require.NoError(t, err)
	require.Empty(t, send("drawio", true).Error, "the agent may turn on a skill the user turned off")
	require.True(t, kept()[0].ByAgent)

	_, err = c.sessions.SetKeptSkill(t.Context(), sess.ID, session.KeptSkill{ID: drawio, Name: "drawio"}, true)
	require.NoError(t, err)
	require.False(t, kept()[0].ByAgent, "the user setting it to plain on takes it back")

	_, err = c.sessions.SetKeptSkill(t.Context(), sess.ID, session.KeptSkill{ID: teach, Name: "teach", ByAgent: true}, true)
	require.NoError(t, err)
	require.Empty(t, send("teach", false).Error, "the agent may turn off a skill the user made toggleable")
	require.Equal(t, []session.KeptSkill{{ID: drawio, Name: "drawio"}}, kept())

	require.Contains(t, send("no-such-skill", true).Error, "no skill named")
	require.Contains(t, h.handle([]byte(`{"session":"","skill":{"name":"teach","on":true}}`)).Error, "sub-agents")
}
