package backend

import (
	"context"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/proto/prototest"
	"github.com/stretchr/testify/require"
)

// TestFallbackSwitchTellsTheAppsTheModelChanged: a chat that the fallback
// order moves to another provider mid-chat tells the apps, so the desktop's
// usage meters show the provider it runs on now and not the one it left
// (owner, 2026-10-22). The workspace is opened the way the desktop opens a
// configured project: CreateWorkspace starts its agent (app.New), and the
// app never calls InitAgent again, as it does only at onboarding.
func TestFallbackSwitchTellsTheAppsTheModelChanged(t *testing.T) {
	r := newLiveRig(t)
	p := r.project(t)
	app := newClientID(t)
	ws, _, err := r.b.CreateWorkspace(prototest.CreateRequest(proto.Workspace{
		Path: p.dir, DataDir: p.dataDir, ClientID: app, YOLO: true,
	}))
	require.NoError(t, err)
	require.NoError(t, r.b.AttachClient(ws.ID, app))
	require.NotNil(t, ws.coordinator(), "the desktop's open starts the agent")

	// The chat runs on a Claude account that has run out; the order's next
	// block is the fake model, the one that answers in tests.
	cfg := ws.Cfg.Config()
	cfg.Providers.Set(string(config.TypeClaudeCode), config.ProviderConfig{
		ID: string(config.TypeClaudeCode), Name: "Claude Code", Type: config.TypeClaudeCode,
		Models: []catwalk.Model{{ID: "opus", Name: "Claude Opus"}},
	})
	cfg.Options.Fallback = &config.FallbackOrder{On: true, Blocks: []config.FallbackBlock{
		{Type: config.FallbackUse, Provider: string(config.TypeClaudeCode), Model: "opus", Account: "claude-2", MoveAtPercent: 1},
		{Type: config.FallbackUse, Provider: "fake", Model: "fake-model"},
	}}
	cfg.Models[config.SelectedModelTypeLarge] = config.SelectedModel{Provider: string(config.TypeClaudeCode), Model: "opus", Account: "claude-2"}
	t.Cleanup(cliagent.SetLimitFetchersForTest(map[catwalk.Type]func(context.Context) ([]cliagent.Limit, error){
		config.TypeClaudeCode: func(context.Context) ([]cliagent.Limit, error) {
			return []cliagent.Limit{{Name: "5h", Used: 100, ResetsAt: time.Now().Add(time.Hour)}}, nil
		},
		catwalk.Type("openai-compat"): func(context.Context) ([]cliagent.Limit, error) {
			return []cliagent.Limit{{Name: "5h", Used: 0, ResetsAt: time.Now().Add(time.Hour)}}, nil
		},
	}))
	_, err = cliagent.RefreshAccountLimits(t.Context(), config.TypeClaudeCode, "claude-2", true)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	events := ws.Events(ctx)
	chat := r.chat(t, ws, "C")
	r.send(t, ws, app, chat, "s1", "hello")

	require.Eventually(t, func() bool {
		return ws.Cfg.Config().Models[config.SelectedModelTypeLarge].Provider == "fake"
	}, 20*time.Second, 20*time.Millisecond, "the fallback order switches the chat to the fake model")
	awaitConfigChanged(t, events, ws.ID)
}
