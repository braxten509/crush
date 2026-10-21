package server

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// A watch=1 stream hears the workspace but holds no claim on it, so the
// desktop listening to another folder never keeps that engine running.
func TestWorkspaceEvents_WatchStreamHearsWithoutAClaim(t *testing.T) {
	t.Parallel()
	h := newE2EHarness(t)
	ws := h.workspace

	cid := uuid.New().String()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.httpSrv.URL+"/v1/workspaces/"+ws.ID+"/events?watch=1&client_id="+cid, nil)
	require.NoError(t, err)
	resp, err := h.httpSrv.Client().Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// No claim: the engine has no client here to report a chat for.
	require.Error(t, h.backend.SetCurrentSession(t.Context(), ws.ID, cid, "S1"), "a watch stream must not hold the workspace")

	h.backend.SendAll(ws.ID, backend.EngineEvent{
		Type:    pubsub.PayloadTypePromptPending,
		Kind:    pubsub.UpdatedEvent,
		Payload: proto.PromptPending{SessionID: "S1", Kind: "question", Pending: true},
	})
	lines := make(chan string, 1)
	h.sseWG.Add(1)
	go func() {
		defer h.sseWG.Done()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "data: ") {
				lines <- scanner.Text()
				return
			}
		}
	}()
	select {
	case line := <-lines:
		require.Contains(t, line, `"prompt_pending"`)
	case <-time.After(5 * time.Second):
		t.Fatal("the watch stream heard nothing")
	}
}
