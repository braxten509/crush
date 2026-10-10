package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/projects"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSavedSessionEndpointsListOnlyCurrentServerFolder(t *testing.T) {
	h := newRealCreateHarness(t)
	h.backend.SetCreateGrace(time.Hour)
	first := h.postWorkspace(t, proto.Workspace{Path: t.TempDir(), DataDir: t.TempDir(), ClientID: uuid.NewString()})
	second := h.postWorkspace(t, proto.Workspace{Path: t.TempDir(), DataDir: t.TempDir(), ClientID: uuid.NewString()})
	for _, ws := range []proto.Workspace{first, second} {
		require.NoError(t, projects.Register(ws.Path, ws.DataDir))
		running, err := h.backend.GetWorkspace(ws.ID)
		require.NoError(t, err)
		chat, err := running.Sessions.Create(t.Context(), "Saved chat")
		require.NoError(t, err)
		_, err = running.Messages.Create(t.Context(), chat.ID, message.CreateMessageParams{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hello"}}})
		require.NoError(t, err)
		_, err = running.Sessions.Create(t.Context(), "Empty chat")
		require.NoError(t, err)
	}
	endpoint := h.httpSrv.URL + "/v1/workspaces/" + first.ID + "/saved-sessions"
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, endpoint, nil)
	require.NoError(t, err)
	response, err := h.httpSrv.Client().Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	var entries []session.Session
	require.NoError(t, json.NewDecoder(response.Body).Decode(&entries))
	response.Body.Close()
	require.Len(t, entries, 1)
	require.Equal(t, "Saved chat", entries[0].Title)
	require.Equal(t, first.Path, entries[0].Directory)
	require.Equal(t, first.DataDir, entries[0].DataDirectory)

	req, err = http.NewRequestWithContext(t.Context(), http.MethodGet, h.httpSrv.URL+"/v1/workspaces/"+second.ID+"/saved-sessions", nil)
	require.NoError(t, err)
	response, err = h.httpSrv.Client().Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.NoError(t, json.NewDecoder(response.Body).Decode(&entries))
	response.Body.Close()
	require.Len(t, entries, 1)
	require.Equal(t, "Saved chat", entries[0].Title)
	require.Equal(t, second.Path, entries[0].Directory)
	require.Equal(t, second.DataDir, entries[0].DataDirectory)
	foreign := entries[0]
	require.NotEmpty(t, foreign.ID)
	foreign.Title = "Renamed in its original folder"
	data, err := json.Marshal(session.CatalogChange{Session: foreign})
	require.NoError(t, err)
	req, err = http.NewRequestWithContext(t.Context(), http.MethodPost, endpoint, bytes.NewReader(data))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	response, err = h.httpSrv.Client().Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	response.Body.Close()
	source, err := h.backend.GetWorkspace(second.ID)
	require.NoError(t, err)
	renamed, err := source.Sessions.Get(t.Context(), foreign.ID)
	require.NoError(t, err)
	require.Equal(t, foreign.Title, renamed.Title)
	require.EqualValues(t, 1, renamed.MessageCount)
}
