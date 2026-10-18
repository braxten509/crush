package server

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/client"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/remote"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/stretchr/testify/require"
)

// waitForMark waits for the prompt mark want on an app's event stream.
func waitForMark(t *testing.T, events <-chan any, want proto.PromptPending) {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			require.True(t, ok, "the event stream ended before %+v", want)
			if mark, isMark := ev.(pubsub.Event[proto.PromptPending]); isMark && mark.Payload == want {
				return
			}
		case <-deadline:
			t.Fatalf("no prompt mark %+v", want)
		}
	}
}

// TestPromptPendingReachesEveryApp: a turn in chat C asks for a permission.
// Both apps' streams get the mark, also the app viewing chat D; it clears
// after a grant, and again after a turn that asks is cancelled. The phone
// hub, which reads the engine's Source, never gets it.
func TestPromptPendingReachesEveryApp(t *testing.T) {
	h := modelHarness(t)
	project := t.TempDir()
	var chatHash atomic.Value
	chatHash.Store("")
	note := filepath.Join(project, "note.txt")
	m := newChatModel(t, func(r chatRequest) chatAnswer {
		switch {
		case r.title || r.sessionHash != chatHash.Load():
			return chatAnswer{text: "ok"}
		case r.lastRole == "tool":
			return chatAnswer{text: "done"}
		default:
			return chatAnswer{toolName: "write", toolArgs: fmt.Sprintf(`{"file_path":%q,"content":"hi"}`, note)}
		}
	})
	ws, _ := newModelWorkspace(t, h, m, project)
	ctx := t.Context()

	// Two apps on the workspace; B views chat D.
	host := strings.TrimPrefix(h.httpSrv.URL, "http://")
	streams := make([]<-chan any, 2)
	apps := make([]*client.Client, 2)
	for i := range apps {
		c, err := client.NewClient(project, "tcp", host)
		require.NoError(t, err)
		joined, err := c.CreateWorkspace(ctx, proto.Workspace{Path: project})
		require.NoError(t, err)
		require.Equal(t, ws.ID, joined.ID)
		t.Cleanup(func() { _ = c.RetireClient(context.Background()) })
		streams[i], err = c.SubscribeEvents(ctx, ws.ID)
		require.NoError(t, err)
		apps[i] = c
	}
	chatC, err := ws.Sessions.Create(ctx, "C")
	require.NoError(t, err)
	chatD, err := ws.Sessions.Create(ctx, "D")
	require.NoError(t, err)
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		t.Logf("model requests:\n%s", m.requests())
		msgs, err := ws.Messages.List(context.Background(), chatC.ID)
		if err != nil {
			return
		}
		for _, msg := range msgs {
			calls := []string{}
			for _, call := range msg.ToolCalls() {
				calls = append(calls, call.Name+" "+call.Input)
			}
			results := []string{}
			for _, res := range msg.ToolResults() {
				results = append(results, res.Content)
			}
			t.Logf("message %s: %q calls=%v results=%v finish=%v", msg.Role, msg.Content().String(), calls, results, msg.FinishReason())
		}
	})
	require.NoError(t, apps[0].SetCurrentSession(ctx, ws.ID, chatC.ID))
	require.NoError(t, apps[1].SetCurrentSession(ctx, ws.ID, chatD.ID))

	// The phone hub's view: the events of the engine's Source.
	var phoneMarks atomic.Int32
	phone := remote.NewSource(workspace.NewAppWorkspace(ws.App, ws.App.Store())).Events(ctx)
	go func() {
		for ev := range phone {
			if _, isMark := ev.Payload.(proto.PromptPending); isMark {
				phoneMarks.Add(1)
			}
		}
	}()

	// The first agent start starts the watcher.
	status, body := doAPI(t, h, http.MethodPost, "/v1/workspaces/"+ws.ID+"/agent/init?client_id="+apps[0].ClientID(),
		proto.AgentInitRequest{Interactive: true})
	require.Equal(t, http.StatusOK, status, string(body))
	chatHash.Store(session.HashID(chatC.ID))

	send := func(prompt string) {
		t.Helper()
		status, body := doAPI(t, h, http.MethodPost, "/v1/workspaces/"+ws.ID+"/agent",
			map[string]any{"session_id": chatC.ID, "prompt": prompt})
		require.Equal(t, http.StatusAccepted, status, string(body))
	}
	expect := func(pending bool) {
		t.Helper()
		want := proto.PromptPending{SessionID: chatC.ID, Kind: "permission", Pending: pending}
		for _, events := range streams {
			waitForMark(t, events, want)
		}
	}

	send("write the note")
	expect(true)
	req, ok := ws.Permissions.ActiveRequest()
	require.True(t, ok)
	require.Equal(t, chatC.ID, req.SessionID)
	require.True(t, ws.Permissions.Grant(req))
	expect(false)
	agentPath := "/v1/workspaces/" + ws.ID + "/agent/sessions/" + chatC.ID
	require.Eventually(t, func() bool {
		return !callAPI[proto.AgentSession](t, h, http.MethodGet, agentPath, nil).IsBusy
	}, 30*time.Second, 50*time.Millisecond, "the granted turn finishes")
	require.FileExists(t, note)

	send("write it again")
	expect(true)
	status, body = doAPI(t, h, http.MethodPost, agentPath+"/cancel", nil)
	require.Equal(t, http.StatusOK, status, string(body))
	expect(false)

	require.Zero(t, phoneMarks.Load(), "the mark never reaches the phone")
}
