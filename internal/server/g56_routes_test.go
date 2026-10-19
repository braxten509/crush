package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/commands"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/stretchr/testify/require"
)

// TestServersRoute: a chat's background job that listens on a port is
// listed with its chat, job and program (G-18).
func TestServersRoute(t *testing.T) {
	l := newLiveWork(t)
	chat := l.chat(t, "web")
	l.start(t, chat, "job", "listen-web")

	// Until the job's start returns (about 1 s), its process is listed on
	// its own, without the job's ID and name.
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
		return len(got) == 1 && got[0].JobID != ""
	}, 20*time.Second, 100*time.Millisecond, "the listening job shows up")
	jobs := agent.SessionBackgroundProcesses(chat)
	require.Len(t, jobs, 1)
	require.Positive(t, got[0].Port)
	require.Equal(t, jobs[0].JobID, got[0].JobID)
	require.Equal(t, "python3 -m http.server 0 --bind 127.0.0.1", got[0].Command)
	require.Equal(t, "listen-web", got[0].Name)
	require.Equal(t, "python3", got[0].Program)
}

// TestProvidersRefreshRoute: the refresh runs `crush update-providers`'
// work for each source, reports each outcome, and reloads so the new
// catalog is known at once (G-9).
func TestProvidersRefreshRoute(t *testing.T) {
	g := newG56Workspace(t)
	events := g.h.subscribeLiveSSE(t, g.ws.ID, g.app)
	g.h.waitForAttachedOn(t, mustWorkspace(t, g), 1)

	before := config.ModelCatalog()
	t.Cleanup(func() { restoreModelCatalog(t, before) })

	extra := catwalk.Provider{ID: "g56-extra", Name: "G56 extra", Type: catwalk.TypeOpenAICompat, APIEndpoint: "http://127.0.0.1:9/v1",
		Models: []catwalk.Model{{ID: "g56-model", Name: "G56 model", ContextWindow: 1000, DefaultMaxTokens: 100}}}
	var catalog []catwalk.Provider
	for _, p := range before {
		if string(p.ID) != "hyper" {
			catalog = append(catalog, p)
		}
	}
	catalog = append(catalog, extra)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/providers" {
			_ = json.NewEncoder(w).Encode(catalog)
			return
		}
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(backend.SetProviderSourcesForTest(srv.URL, srv.URL))

	got := liveJSON[proto.ProvidersRefresh](t, g.h, http.MethodPost, "/v1/providers/refresh", nil, http.StatusOK)
	require.Len(t, got.Results, 2)
	require.Equal(t, proto.ProviderRefreshResult{Source: "catwalk", OK: true}, got.Results[0])
	require.Equal(t, "hyper", got.Results[1].Source)
	require.False(t, got.Results[1].OK)
	require.Contains(t, got.Results[1].Error, "failed to fetch provider from Hyper")

	_, err := os.Stat(filepath.Join(filepath.Dir(config.GlobalConfigData()), "providers.json"))
	require.NoError(t, err, "the catalog is cached where crush update-providers caches it")
	require.True(t, slices.ContainsFunc(mustWorkspace(t, g).Cfg.KnownProviders(), func(p catwalk.Provider) bool { return p.ID == extra.ID }),
		"the reload knows the new provider")
	_, ok := nextLive[proto.ConfigChanged](t, events, pubsub.PayloadTypeConfigChanged, 10*time.Second)
	require.True(t, ok, "config_changed")
}

// restoreModelCatalog makes list the process's model catalog again.
func restoreModelCatalog(t *testing.T, list []catwalk.Provider) {
	dir := t.TempDir()
	var catalog []catwalk.Provider
	for _, p := range list {
		if string(p.ID) == "hyper" {
			data, err := json.Marshal(p)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "hyper.json"), data, 0o600))
			require.NoError(t, config.UpdateHyper(filepath.Join(dir, "hyper.json")))
			continue
		}
		catalog = append(catalog, p)
	}
	data, err := json.Marshal(catalog)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "providers.json"), data, 0o600))
	require.NoError(t, config.UpdateProviders(filepath.Join(dir, "providers.json")))
	require.NoError(t, config.ReloadModelCatalog())
}

// TestSkillsSearchRoute: the route answers with skills.sh's own JSON for
// the search, refuses a one-letter query as the terminal does, and passes
// a failed search's text on (G-14, through the G-24 seam's setter).
func TestSkillsSearchRoute(t *testing.T) {
	g := newG56Workspace(t)
	var queries []string
	t.Cleanup(backend.SetSkillsSearchForTest(func(_ context.Context, q string) ([]skills.DirectorySkill, error) {
		queries = append(queries, q)
		if q == "broken" {
			return nil, errors.New("skill download: HTTP 503")
		}
		return []skills.DirectorySkill{{ID: "acme/tools/pdf", Name: "pdf", Source: "acme/tools", SkillID: "pdf", Installs: 42}}, nil
	}))

	code, data := liveCall(t, g.h, http.MethodGet, g.path("/skills/search?q=pdf"), nil)
	require.Equal(t, http.StatusOK, code, "%s", data)
	require.JSONEq(t, `[{"id":"acme/tools/pdf","name":"pdf","source":"acme/tools","skillId":"pdf","installs":42}]`, string(data))
	require.Equal(t, []string{"pdf"}, queries)

	liveError(t, g.h, http.MethodGet, g.path("/skills/search?q=p"), nil, http.StatusBadRequest, "bad_request",
		"type at least two characters to search skills.sh")
	liveError(t, g.h, http.MethodGet, g.path("/skills/search?q=broken"), nil, http.StatusBadGateway, "search_failed",
		"skill download: HTTP 503")
	require.Equal(t, []string{"pdf", "broken"}, queries, "a short query is not sent")
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
