package cliagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOpenCodeSteersThroughItsServer: ACP refuses a second prompt while one
// runs; OpenCode's own server accepts it with delivery:"steer". The message
// must reach that endpoint with the local server's Basic auth.
func TestOpenCodeSteersThroughItsServer(t *testing.T) {
	var called atomic.Bool
	var gotText, gotDelivery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/session/ses1/prompt", r.URL.Path)
		user, pass, ok := r.BasicAuth()
		require.True(t, ok)
		require.Equal(t, "opencode", user)
		require.Equal(t, "secret", pass)
		var body struct {
			Text     string `json:"text"`
			Delivery string `json:"delivery"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		gotText, gotDelivery = body.Text, body.Delivery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	t.Cleanup(server.Close)

	var events []Event
	r := &acpRun{
		ctx:           context.Background(),
		steerURL:      server.URL,
		steerPassword: "secret",
		t: Turn{Emit: func(e Event) error {
			events = append(events, e)
			return nil
		}},
	}
	sid := "ses1"
	r.session.Store(&sid)

	require.True(t, r.steer("QUEUED"))
	require.True(t, called.Load())
	require.Equal(t, "QUEUED", gotText)
	require.Equal(t, "steer", gotDelivery)
	require.Equal(t, []Event{{Type: EventUserMessage, Text: "QUEUED"}}, events)
}
