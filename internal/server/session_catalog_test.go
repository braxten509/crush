package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
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

func TestSavedSessionReadsAnotherFoldersChatFromItsDatabase(t *testing.T) {
	h := newRealCreateHarness(t)
	h.backend.SetCreateGrace(time.Hour)
	first := h.postWorkspace(t, proto.Workspace{Path: t.TempDir(), DataDir: t.TempDir(), ClientID: uuid.NewString()})
	second := h.postWorkspace(t, proto.Workspace{Path: t.TempDir(), DataDir: t.TempDir(), ClientID: uuid.NewString()})
	for _, ws := range []proto.Workspace{first, second} {
		require.NoError(t, projects.Register(ws.Path, ws.DataDir))
	}
	running, err := h.backend.GetWorkspace(second.ID)
	require.NoError(t, err)
	chat, err := running.Sessions.Create(t.Context(), "Other folder")
	require.NoError(t, err)
	for _, text := range []string{"one", "two", "three"} {
		_, err = running.Messages.Create(t.Context(), chat.ID, message.CreateMessageParams{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: text}}})
		require.NoError(t, err)
	}
	get := func(path string, out any) int {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, h.httpSrv.URL+"/v1/workspaces/"+first.ID+path, nil)
		require.NoError(t, err)
		response, err := h.httpSrv.Client().Do(req)
		require.NoError(t, err)
		defer response.Body.Close()
		if response.StatusCode == http.StatusOK {
			require.NoError(t, json.NewDecoder(response.Body).Decode(out))
		}
		return response.StatusCode
	}
	dir := "?dir=" + url.QueryEscape(second.Path)

	var record proto.Session
	require.Equal(t, http.StatusOK, get("/saved-sessions/"+chat.ID+dir, &record))
	require.Equal(t, chat.ID, record.ID)
	require.Equal(t, "Other folder", record.Title)

	var messages []proto.Message
	require.Equal(t, http.StatusOK, get("/saved-sessions/"+chat.ID+"/messages"+dir+"&last=2", &messages))
	require.Len(t, messages, 2)
	require.Equal(t, "three", messages[1].Content().Text)

	// A folder that is not a registered project is never read.
	require.NotEqual(t, http.StatusOK, get("/saved-sessions/"+chat.ID+"?dir="+url.QueryEscape(t.TempDir()), &record))
}
