package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestCapabilitiesReportModelSupport: the capabilities carry what the
// coder model supports, read with the terminal's own functions (G-8).
func TestCapabilitiesReportModelSupport(t *testing.T) {
	h := newE2EHarness(t)
	path := "/v1/workspaces/" + h.workspace.ID + "/agent/capabilities"
	providers := map[string]config.ProviderConfig{
		"claude-code": {ID: "claude-code", Type: config.TypeClaudeCode},
		"openai":      {ID: "openai", Type: catwalk.TypeOpenAI},
	}
	h.workspace.Cfg = config.NewTestStore(&config.Config{Providers: csync.NewMapFrom(providers)})

	for _, tc := range []struct {
		provider string
		model    catwalk.Model
	}{
		{"claude-code", catwalk.Model{ID: "opus", CanReason: true, ReasoningLevels: []string{"low", "high"}}},
		{"claude-code", catwalk.Model{ID: "haiku"}},
		{"openai", catwalk.Model{ID: "thinker", CanReason: true}},
		{"openai", catwalk.Model{ID: "plain"}},
	} {
		h.app.AgentCoordinator = modelCoordinator{model: agent.Model{
			CatwalkCfg: tc.model,
			ModelCfg:   config.SelectedModel{Provider: tc.provider, Model: tc.model.ID},
		}}
		got := liveJSON[proto.AgentCapabilities](t, h, http.MethodGet, path, nil, http.StatusOK)
		pc := providers[tc.provider]
		require.Equal(t, config.SupportsFastMode(pc, tc.model), got.SupportsFast, tc.model.ID)
		require.Equal(t, config.SupportsUltracode(pc, tc.model), got.SupportsUltracode, tc.model.ID)
		require.Equal(t, config.SupportsThinkingToggle(pc, tc.model), got.SupportsThinkingToggle, tc.model.ID)
	}
	h.app.AgentCoordinator = modelCoordinator{model: agent.Model{
		CatwalkCfg: catwalk.Model{ID: "opus", ReasoningLevels: []string{"high"}},
		ModelCfg:   config.SelectedModel{Provider: "claude-code", Model: "opus"},
	}}
	got := liveJSON[proto.AgentCapabilities](t, h, http.MethodGet, path, nil, http.StatusOK)
	require.True(t, got.SupportsFast)
	require.True(t, got.SupportsUltracode)
	require.False(t, got.SupportsThinkingToggle)
}

// TestPendingQuestionRoute: the question a chat waits on can be read
// again; another chat, or none pending, answers 404 no_pending_question
// (G-12).
func TestPendingQuestionRoute(t *testing.T) {
	h := newE2EHarness(t)
	path := "/v1/workspaces/" + h.workspace.ID + "/questions/pending?session_id="

	liveError(t, h, http.MethodGet, path+"chat-1", nil, http.StatusNotFound, "no_pending_question", "")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	asked := make(chan error, 1)
	go func() {
		_, err := h.app.Questions.Ask(ctx, question.Request{ID: "batch-1", SessionID: "chat-1", Questions: []question.Question{{
			ID: "q1", Type: question.TypeMultiChoice, Text: "Which?", Description: "Pick some.",
			Choices: []question.Choice{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}, Selected: []string{"a"},
		}}})
		asked <- err
	}()
	require.Eventually(t, func() bool {
		_, ok := h.app.Questions.Pending()
		return ok
	}, 5*time.Second, 10*time.Millisecond)

	got := liveJSON[proto.QuestionRequest](t, h, http.MethodGet, path+"chat-1", nil, http.StatusOK)
	require.Equal(t, "batch-1", got.ID)
	require.Equal(t, "chat-1", got.SessionID)
	require.Len(t, got.Questions, 1)
	require.Equal(t, "Which?", got.Questions[0].Question)
	liveError(t, h, http.MethodGet, path+"chat-2", nil, http.StatusNotFound, "no_pending_question", "")
	liveError(t, h, http.MethodGet, path, nil, http.StatusBadRequest, "bad_request", "")

	cancel()
	<-asked
}

// TestReAuthenticateCarriesProvider:a re_authenticate agent event reaches
// the stream with the provider to sign in to (G-10).
func TestReAuthenticateCarriesProvider(t *testing.T) {
	h := newE2EHarness(t)
	events := h.subscribeLiveSSE(t, h.workspace.ID, uuid.NewString())
	h.waitForAttached(t, 1)

	h.app.SendEvent(pubsub.Event[notify.Notification]{Type: pubsub.CreatedEvent, Payload: notify.Notification{
		SessionID: "chat-1", Type: notify.TypeReAuthenticate, ProviderID: "openai",
	}})
	ev, ok := nextLive[proto.AgentEvent](t, events, pubsub.PayloadTypeAgentEvent, 5*time.Second)
	require.True(t, ok)
	require.Equal(t, proto.AgentEventType(notify.TypeReAuthenticate), ev.Payload.Type)
	require.Equal(t, "openai", ev.Payload.ProviderID)
	require.Equal(t, "chat-1", ev.Payload.SessionID)
}
