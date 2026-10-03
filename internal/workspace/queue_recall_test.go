package workspace

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/charmbracelet/crush/internal/client"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/stretchr/testify/require"
)

func TestClientWorkspaceRecallsPromptWithAttachments(t *testing.T) {
	attachment := message.Attachment{FileName: "photo.png", MimeType: "image/png", Content: []byte{1, 2, 3}}
	prompt := &proto.AgentMessage{SessionID: "s1", Prompt: "queued", Attachments: proto.AttachmentsFromMessage([]message.Attachment{attachment})}
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/workspaces/ws1/agent/sessions/s1/prompts/recall", r.URL.Path)
		if r.URL.Query().Get("acknowledge") == "true" {
			w.WriteHeader(http.StatusOK)
			return
		}
		switch requests.Add(1) {
		case 1:
			_ = json.NewEncoder(w).Encode(prompt)
		case 2:
			_ = json.NewEncoder(w).Encode(nil)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	address, err := url.Parse(srv.URL)
	require.NoError(t, err)
	c, err := client.NewClient(t.TempDir(), "tcp", address.Host)
	require.NoError(t, err)
	ws := NewClientWorkspace(c, proto.Workspace{ID: "ws1"})
	got, err := ws.AgentRecallQueuedPrompt(t.Context(), "s1")
	require.NoError(t, err)
	require.Equal(t, &message.QueuedPrompt{Prompt: "queued", Attachments: []message.Attachment{attachment}}, got)
	got, err = ws.AgentRecallQueuedPrompt(t.Context(), "s1")
	require.NoError(t, err)
	require.Nil(t, got)
	got, err = ws.AgentRecallQueuedPrompt(t.Context(), "s1")
	require.Error(t, err)
	require.Nil(t, got)
}

func TestClientRecallRetriesSameRequestAfterLostResponse(t *testing.T) {
	var requests atomic.Int32
	requestIDs := make(chan string, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("acknowledge") == "true" {
			return
		}
		requestIDs <- r.URL.Query().Get("request_id")
		if requests.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"prompt":"truncated`))
			return
		}
		_ = json.NewEncoder(w).Encode(proto.AgentMessage{Prompt: "recovered"})
	}))
	defer srv.Close()
	address, err := url.Parse(srv.URL)
	require.NoError(t, err)
	c, err := client.NewClient(t.TempDir(), "tcp", address.Host)
	require.NoError(t, err)
	ws := NewClientWorkspace(c, proto.Workspace{ID: "ws1"})
	got, err := ws.AgentRecallQueuedPrompt(t.Context(), "s1")
	require.Error(t, err)
	require.Nil(t, got)
	got, err = ws.AgentRecallQueuedPrompt(t.Context(), "s1")
	require.NoError(t, err)
	require.Equal(t, "recovered", got.Prompt)
	first, second := <-requestIDs, <-requestIDs
	require.NotEmpty(t, first)
	require.Equal(t, first, second, "retry must recover the same withdrawn prompt")
}
