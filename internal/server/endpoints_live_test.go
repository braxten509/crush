package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestHelperEventReachesSSE: a helper event on the app's fan-in reaches the
// stream as type helper, with every field (engine-design.md 5.3).
func TestHelperEventReachesSSE(t *testing.T) {
	h := newE2EHarness(t)
	clientID := uuid.NewString()
	events := h.subscribeLiveSSE(t, h.workspace.ID, clientID)
	h.waitForAttached(t, 1)

	started := time.Date(2026, 10, 18, 9, 30, 0, 0, time.UTC)
	task := agent.Task{
		ID: "t7", SessionID: "chat-1", ChildID: "child-1", Name: "worker", CLI: "codex", Model: "gpt-5",
		Effort: "high", Fast: true, ReadOnly: true, Status: agent.TaskDone,
		Started: started, Ended: started.Add(time.Minute), Delivered: true, StartedBy: clientID,
	}
	h.app.SendEvent(pubsub.Event[agent.Task]{Type: pubsub.UpdatedEvent, Payload: task})

	ev, ok := nextLive[proto.Helper](t, events, pubsub.PayloadTypeHelper, 5*time.Second)
	require.True(t, ok, "the task event must reach the stream as a helper event")
	require.Equal(t, pubsub.UpdatedEvent, ev.Type)
	got := ev.Payload
	require.True(t, got.Started.Equal(task.Started))
	require.True(t, got.Ended.Equal(task.Ended))
	got.Started, got.Ended = task.Started, task.Ended
	require.Equal(t, proto.Helper{
		ID: "t7", SessionID: "chat-1", ChildSessionID: "child-1", Name: "worker", CLI: "codex", Model: "gpt-5",
		Effort: "high", Fast: true, ReadOnly: true, Status: "done",
		Started: task.Started, Ended: task.Ended, Delivered: true, StartedBy: clientID,
	}, got)
}

// TestListHelpersScopedToChat: each chat's list holds only its own helper
// (engine-design.md 5.3).
func TestListHelpersScopedToChat(t *testing.T) {
	l := newLiveWork(t)
	one, two := l.chat(t, "one"), l.chat(t, "two")
	helperOne := l.helper(t, one, "one")
	helperTwo := l.helper(t, two, "two")

	for chat, want := range map[string]agent.Task{one: helperOne, two: helperTwo} {
		got := liveJSON[[]proto.Helper](t, l.h, http.MethodGet,
			"/v1/workspaces/"+l.ws.ID+"/sessions/"+chat+"/helpers", nil, http.StatusOK)
		require.Len(t, got, 1, "chat %s lists only its own helper", chat)
		require.Equal(t, want.ID, got[0].ID)
		require.Equal(t, chat, got[0].SessionID)
		require.Equal(t, want.ChildID, got[0].ChildSessionID)
		require.Equal(t, "running", got[0].Status)
		require.Equal(t, l.app, got[0].StartedBy)

		code, body := liveCall(t, l.h, http.MethodGet,
			"/v1/workspaces/"+l.ws.ID+"/sessions/"+got[0].ChildSessionID+"/helpers", nil)
		require.Equal(t, http.StatusOK, code)
		require.JSONEq(t, `[]`, string(body), "a helper's child chat lists none")
	}
	liveError(t, l.h, http.MethodGet, "/v1/workspaces/"+uuid.NewString()+"/sessions/"+one+"/helpers", nil,
		http.StatusNotFound, "not_found", "")
}

// TestStopHelperOtherChatRefused: a stop through another chat answers 404
// helper_not_found and the helper runs on; through its own chat it stops,
// and a second stop of the ended helper answers 200 (engine-design.md 5.3).
func TestStopHelperOtherChatRefused(t *testing.T) {
	l := newLiveWork(t)
	own, other := l.chat(t, "own"), l.chat(t, "other")
	helper := l.helper(t, own, "h1")
	stop := func(chat, query string) string {
		return "/v1/workspaces/" + l.ws.ID + "/sessions/" + chat + "/helpers/" + helper.ID + "/stop" + query
	}

	liveError(t, l.h, http.MethodPost, stop(other, ""), nil, http.StatusNotFound, "helper_not_found",
		"helper not found in this chat")
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, 1, l.model.heldNow("h1-helper"), "the helper keeps running")

	liveError(t, l.h, http.MethodPost, stop(own, "?client_id="+uuid.NewString()), nil,
		http.StatusConflict, "client_not_attached", "")
	liveError(t, l.h, http.MethodPost, stop(own, "?client_id=not-a-uuid"), nil, http.StatusBadRequest, "bad_request", "")

	liveJSON[struct{}](t, l.h, http.MethodPost, stop(own, "?client_id="+l.app), nil, http.StatusOK)
	l.model.waitHeld(t, "h1-helper", 0)
	require.Eventually(t, func() bool {
		got := liveJSON[[]proto.Helper](t, l.h, http.MethodGet,
			"/v1/workspaces/"+l.ws.ID+"/sessions/"+own+"/helpers", nil, http.StatusOK)
		return len(got) == 1 && got[0].Status == "stopped"
	}, 10*time.Second, 20*time.Millisecond, "the user's stop marks it stopped")
	liveJSON[struct{}](t, l.h, http.MethodPost, stop(own, ""), nil, http.StatusOK)
}

// TestListJobsScopedToChat: a chat's background jobs are absent from
// another chat's list (engine-design.md 5.4).
func TestListJobsScopedToChat(t *testing.T) {
	l := newLiveWork(t)
	chatA, chatB := l.chat(t, "A"), l.chat(t, "B")
	jobA := l.job(t, chatA, "ja")

	jobs := liveJSON[[]proto.Job](t, l.h, http.MethodGet,
		"/v1/workspaces/"+l.ws.ID+"/sessions/"+chatA+"/jobs", nil, http.StatusOK)
	require.Len(t, jobs, 1)
	require.Equal(t, jobA.JobID, jobs[0].JobID)
	require.Equal(t, chatA, jobs[0].SessionID)
	require.Equal(t, "sleep 120", jobs[0].Command)
	require.Equal(t, l.app, jobs[0].StartedBy)
	require.False(t, jobs[0].Started.IsZero())

	code, body := liveCall(t, l.h, http.MethodGet, "/v1/workspaces/"+l.ws.ID+"/sessions/"+chatB+"/jobs", nil)
	require.Equal(t, http.StatusOK, code)
	require.JSONEq(t, `[]`, string(body), "chat A's job is absent from chat B's list")
}

// TestKillJobOtherChatRefused: a kill through another chat answers 404
// job_not_found and the job runs on; through its own chat it ends
// (engine-design.md 5.4).
func TestKillJobOtherChatRefused(t *testing.T) {
	l := newLiveWork(t)
	own, other := l.chat(t, "own"), l.chat(t, "other")
	job := l.job(t, own, "jk")
	kill := func(chat string) string {
		return "/v1/workspaces/" + l.ws.ID + "/sessions/" + chat + "/jobs/kill?client_id=" + l.app
	}

	liveError(t, l.h, http.MethodPost, kill(other), proto.JobRef{JobID: job.JobID}, http.StatusNotFound,
		"job_not_found", "job not found in this chat")
	time.Sleep(200 * time.Millisecond)
	require.Len(t, agent.SessionBackgroundProcesses(own), 1, "the job is alive")

	for _, bad := range []proto.JobRef{{}, {JobID: job.JobID, PID: 42}, {PID: -1}} {
		liveError(t, l.h, http.MethodPost, kill(own), bad, http.StatusBadRequest, "bad_request",
			"set exactly one of job_id and pid")
	}

	liveJSON[struct{}](t, l.h, http.MethodPost, kill(own), proto.JobRef{JobID: job.JobID}, http.StatusOK)
	require.Eventually(t, func() bool { return len(agent.SessionBackgroundProcesses(own)) == 0 },
		20*time.Second, 20*time.Millisecond, "the job ends")
}

// chatSessions is a session service whose chats are named by their ID and
// carry the given models.
func chatSessions(t *testing.T, models map[string]config.SelectedModel) *stubSessions {
	t.Helper()
	s := &stubSessions{}
	for id, m := range models {
		data, err := json.Marshal(m)
		require.NoError(t, err)
		s.all = append(s.all, session.Session{ID: id, Model: data})
	}
	return s
}

// TestAgentCapabilitiesRoute: can_background is the terminal's Ctrl+B hint
// for the asked chat's model kind (engine-design.md 5.4).
func TestAgentCapabilitiesRoute(t *testing.T) {
	t.Setenv("CRUSH_GLOBAL_DATA", t.TempDir())
	h := newE2EHarness(t)
	path := "/v1/workspaces/" + h.workspace.ID + "/agent/capabilities"

	got := liveJSON[proto.AgentCapabilities](t, h, http.MethodGet, path, nil, http.StatusOK)
	require.Equal(t, proto.AgentCapabilities{}, got, "no chat and no last pick: no kind, nothing moves")

	cases := []struct {
		provider string
		want     bool
	}{
		{string(config.TypeClaudeCode), true},
		{string(config.TypeCodexCLI), true},
		{string(config.TypeOpenCodeCLI), true},
		{string(config.TypeGrokCLI), false},
		{string(config.TypeAGYCLI), false},
		{config.AbacusProviderID, true},
		{"openai", false},
	}
	chats := map[string]config.SelectedModel{}
	for _, tc := range cases {
		chats[tc.provider] = config.SelectedModel{Provider: tc.provider, Model: "m"}
	}
	h.app.Sessions = chatSessions(t, chats)
	for _, tc := range cases {
		got := liveJSON[proto.AgentCapabilities](t, h, http.MethodGet, path+"?session="+tc.provider, nil, http.StatusOK)
		require.Equal(t, tc.provider, got.Kind, "the kind is the chat model's provider, as the hint reads it")
		require.Equal(t, tc.want, got.CanBackground, tc.provider)
		require.Equal(t, cliagent.CanBackground(catwalk.Type(got.Kind)) || got.Kind == config.AbacusProviderID, got.CanBackground)
	}

	code, body := liveCall(t, h, http.MethodGet, path, nil)
	require.Equal(t, http.StatusOK, code)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(body, &raw))
	require.Contains(t, raw, "kind")
	require.Contains(t, raw, "can_background")
	liveError(t, h, http.MethodGet, "/v1/workspaces/"+uuid.NewString()+"/agent/capabilities", nil,
		http.StatusNotFound, "not_found", "")
}
