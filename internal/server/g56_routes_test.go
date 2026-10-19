package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/commands"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/stretchr/testify/require"
)

// TestServersRoute: a chat's background job that listens on a port is
// listed with its chat, job and program (G-18).
func TestServersRoute(t *testing.T) {
	l := newLiveWork(t)
	chat := l.chat(t, "web")
	job := l.job(t, chat, "listen-web")

	var got []proto.ServerPort
	require.Eventually(t, func() bool {
		all := liveJSON[[]proto.ServerPort](t, l.h, http.MethodGet, "/v1/workspaces/"+l.ws.ID+"/servers", nil, http.StatusOK)
		got = nil
		for _, s := range all {
			require.NotEmpty(t, s.SessionID)
			if s.SessionID == chat {
				got = append(got, s)
			}
		}
		return len(got) == 1
	}, 20*time.Second, 100*time.Millisecond, "the listening job shows up")
	require.Positive(t, got[0].Port)
	require.Equal(t, chat, got[0].SessionID)
	require.Equal(t, job.JobID, got[0].JobID)
	require.Equal(t, job.Command, got[0].Command)
	require.Equal(t, "listen-web", got[0].Name)
	require.Contains(t, got[0].Program, "python")
}

// TestCommandsRouteMatchesLoader:the commands route lists what the
// terminal's own loader finds, then the user skills (G-7).
func TestCommandsRouteMatchesLoader(t *testing.T) {
	g := newG56Workspace(t)
	dir := filepath.Join(g.ws.DataDir, "commands")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "team"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hello.md"), []byte("Say hi to $NAME in $LANG_CODE."), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "team", "plain.md"), []byte("No arguments here."), 0o600))

	got := liveJSON[[]proto.CustomCommand](t, g.h, http.MethodGet, g.path("/commands"), nil, http.StatusOK)

	loaded, err := commands.LoadCustomCommands(mustWorkspace(t, g).Cfg.Config())
	require.NoError(t, err)
	require.NotEmpty(t, loaded)
	require.GreaterOrEqual(t, len(got), len(loaded))
	require.Equal(t, backend.CustomCommandsToProto(loaded), got[:len(loaded)], "the loader's commands come first, as in the terminal")

	byID := map[string]proto.CustomCommand{}
	for _, c := range got {
		byID[c.ID] = c
	}
	hello := byID["project:hello"]
	require.Equal(t, "Say hi to $NAME in $LANG_CODE.", hello.Content)
	require.Equal(t, []proto.CustomCommandArgument{
		{Name: "NAME", Title: "NAME", Required: true},
		{Name: "LANG_CODE", Title: "LANG_CODE", Required: true},
	}, hello.Arguments)
	require.Nil(t, hello.Skill)
	require.Contains(t, byID, "project:team:plain")
	require.Empty(t, byID["project:team:plain"].Arguments)
}
