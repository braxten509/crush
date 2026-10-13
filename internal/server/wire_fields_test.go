package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// savingSessions records the session handed to Save and echoes it back.
type savingSessions struct {
	session.Service
	saved session.Session
}

func (s *savingSessions) Save(_ context.Context, sess session.Session) (session.Session, error) {
	s.saved = sess
	return sess, nil
}

// fixedHistory returns a fixed list of history files.
type fixedHistory struct {
	history.Service
	files []history.File
}

func (h *fixedHistory) ListBySession(context.Context, string) ([]history.File, error) {
	return h.files, nil
}

func newWireFieldsController(t *testing.T, a *app.App) (*controllerV1, string) {
	t.Helper()
	b := backend.New(context.Background(), nil, nil)
	ws := &backend.Workspace{ID: uuid.New().String(), Path: t.TempDir(), App: a}
	backend.InsertWorkspaceForTest(b, ws)
	return &controllerV1{backend: b, server: &Server{backend: b}}, ws.ID
}

// TestPutSessionKeepsSnakeCaseFields is the regression for renaming a
// session over the API: the handler decoded the body into session.Session,
// which has no JSON tags, so every snake_case field (token counts, the
// summary message, the parent session, todo active forms) arrived as zero
// and was written back to the database.
func TestPutSessionKeepsSnakeCaseFields(t *testing.T) {
	t.Parallel()
	sessions := &savingSessions{}
	c, wsID := newWireFieldsController(t, &app.App{Sessions: sessions})

	want := session.Session{
		ID:               "s1",
		ParentSessionID:  "parent",
		Title:            "Renamed",
		MessageCount:     4,
		PromptTokens:     100,
		CompletionTokens: 200,
		SummaryMessageID: "summary",
		Cost:             0.5,
		Todos:            []session.Todo{{Content: "c", Status: "pending", ActiveForm: "Doing c"}},
		KeptSkills:       []session.KeptSkill{{ID: "k", Name: "K", ByAgent: true}},
		CreatedAt:        1,
		UpdatedAt:        2,
	}
	body, err := json.Marshal(proto.SessionFromDomain(want))
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPut,
		"/v1/workspaces/"+wsID+"/sessions/s1", bytes.NewReader(body))
	req.SetPathValue("id", wsID)
	req.SetPathValue("sid", "s1")
	rec := httptest.NewRecorder()
	c.endpointHandler(rpc.SaveSession)(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	require.Equal(t, want, sessions.saved)
}

// TestSessionHistoryUsesWireFields is the regression for the history
// endpoint encoding history.File directly: it has no JSON tags, so the
// client's proto.File lost SessionID, CreatedAt and UpdatedAt (the UI sorts
// the modified-files list by UpdatedAt).
func TestSessionHistoryUsesWireFields(t *testing.T) {
	t.Parallel()
	file := history.File{
		ID: "f1", SessionID: "s1", Path: "a.go", Content: "x",
		Version: 2, CreatedAt: 10, UpdatedAt: 20,
	}
	c, wsID := newWireFieldsController(t, &app.App{History: &fixedHistory{files: []history.File{file}}})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
		"/v1/workspaces/"+wsID+"/sessions/s1/history", nil)
	req.SetPathValue("id", wsID)
	req.SetPathValue("sid", "s1")
	rec := httptest.NewRecorder()
	c.endpointHandler(rpc.SessionHistory)(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got []proto.File
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []history.File{file}, proto.FilesToDomain(got))
}
