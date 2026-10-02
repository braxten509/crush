package remote

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/stretchr/testify/require"
)

func testServer(src Source) *Server {
	return &Server{
		src: src,
		hub: newHub(src, "host", 7373),
		gate: &gate{user: ownerUser, whois: func(context.Context, string) (peer, error) {
			return peer{User: ownerUser}, nil
		}},
	}
}

func TestMutatingRoutesRejectBrowserOriginsAndNonJSON(t *testing.T) {
	t.Parallel()
	paths := []string{
		"/v1/send", "/v1/cancel", "/v1/interrupt", "/v1/background",
		"/v1/queue/clear", "/v1/model", "/v1/effort", "/v1/command",
		"/v1/permission", "/v1/question", "/v1/rename",
		"/v1/tasks/task/stop", "/v1/processes/123/kill",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			for _, origin := range []string{"http://evil.example", "null", "http://100.64.0.1:7373", ""} {
				s := testServer(newFakeSource())
				r := httptest.NewRequest(http.MethodPost, "http://100.64.0.1:7373"+path, strings.NewReader(`{"command":"yolo"}`))
				r.RemoteAddr = phoneAddr + ":1234"
				r.Header.Set("Origin", origin)
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				s.routes().ServeHTTP(w, r)
				require.Equal(t, http.StatusForbidden, w.Code, "origin %q", origin)
			}
			for _, contentType := range []string{"text/plain", "application/x-www-form-urlencoded", "multipart/form-data; boundary=x", "", "application/json; charset"} {
				s := testServer(newFakeSource())
				r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"command":"yolo"}`))
				r.RemoteAddr = phoneAddr + ":1234"
				if contentType != "" {
					r.Header.Set("Content-Type", contentType)
				}
				w := httptest.NewRecorder()
				s.routes().ServeHTTP(w, r)
				require.Equal(t, http.StatusUnsupportedMediaType, w.Code, "content type %q", contentType)
			}
		})
	}
}

func TestGuardAcceptsNativeJSONRequests(t *testing.T) {
	t.Parallel()
	for _, contentType := range []string{"application/json", "application/json; charset=utf-8"} {
		s := testServer(newFakeSource())
		called := false
		handler := s.guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusNoContent)
		}))
		r := httptest.NewRequest(http.MethodPost, "/v1/send", strings.NewReader(`{"text":"hello"}`))
		r.RemoteAddr = phoneAddr + ":1234"
		r.Header.Set("Content-Type", contentType)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, http.StatusNoContent, w.Code)
		require.True(t, called)
	}
}

func TestPromptsMustBelongToSharedChat(t *testing.T) {
	t.Parallel()
	for _, sharedSession := range []string{"s1", ""} {
		pendingSession := "s2"
		if sharedSession == "" {
			pendingSession = ""
		}
		for _, answer := range []string{"allow", "session", "deny"} {
			f := newFakeSource()
			f.setPermission(&permission.PermissionRequest{ID: "p2", SessionID: pendingSession})
			s := testServer(f)
			s.SetPresence(Presence{SessionID: sharedSession})
			r := httptest.NewRequest(http.MethodPost, "/v1/permission", strings.NewReader(`{"session":"`+sharedSession+`","id":"p2","answer":"`+answer+`"}`))
			w := httptest.NewRecorder()
			s.handlePermission(w, r)
			require.Equal(t, http.StatusConflict, w.Code)
			require.Empty(t, f.granted)
			_, pending := f.PendingPermission()
			require.True(t, pending)
		}
		for _, cancel := range []string{"false", "true"} {
			f := newFakeSource()
			f.setQuestion(&question.Request{ID: "q2", SessionID: pendingSession})
			s := testServer(f)
			s.SetPresence(Presence{SessionID: sharedSession})
			r := httptest.NewRequest(http.MethodPost, "/v1/question", strings.NewReader(`{"session":"`+sharedSession+`","id":"q2","cancel":`+cancel+`}`))
			w := httptest.NewRecorder()
			s.handleQuestion(w, r)
			require.Equal(t, http.StatusConflict, w.Code)
			require.Empty(t, f.answers)
			_, pending := f.PendingQuestion()
			require.True(t, pending)
		}
	}
}

type replacedQuestionSource struct {
	*fakeSource
	newSession string
}

func (f *replacedQuestionSource) PendingQuestion() (question.Request, bool) {
	old, ok := f.fakeSource.PendingQuestion()
	f.setQuestion(&question.Request{ID: "new", SessionID: f.newSession})
	return old, ok
}

func TestStaleQuestionResponseCannotResolveReplacement(t *testing.T) {
	t.Parallel()
	for _, newSession := range []string{"s1", "s2"} {
		for _, cancel := range []string{"false", "true"} {
			f := &replacedQuestionSource{fakeSource: newFakeSource(), newSession: newSession}
			f.setQuestion(&question.Request{ID: "old", SessionID: "s1"})
			s := testServer(f)
			s.SetPresence(Presence{SessionID: "s1"})
			r := httptest.NewRequest(http.MethodPost, "/v1/question", strings.NewReader(`{"session":"s1","id":"old","cancel":`+cancel+`,"answers":[{"question_id":"old-question","yes":true}]}`))
			w := httptest.NewRecorder()
			s.handleQuestion(w, r)
			require.Equal(t, http.StatusConflict, w.Code)
			require.Empty(t, f.answers)
			q, pending := f.fakeSource.PendingQuestion()
			require.True(t, pending)
			require.Equal(t, "new", q.ID)
			require.Equal(t, newSession, q.SessionID)
		}
	}
}

func TestUploadsWithSameNameKeepSeparateFiles(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	first, err := saveUploads([]upload{{Name: "../photo.jpg", Data: base64.StdEncoding.EncodeToString([]byte("first"))}})
	require.NoError(t, err)
	second, err := saveUploads([]upload{{Name: "photo.jpg", Data: base64.StdEncoding.EncodeToString([]byte("second"))}})
	require.NoError(t, err)
	require.NotEqual(t, filepath.Dir(first[0].FilePath), filepath.Dir(second[0].FilePath))
	for _, attachment := range append(first, second...) {
		require.Equal(t, "photo.jpg", filepath.Base(attachment.FilePath))
		data, err := os.ReadFile(attachment.FilePath)
		require.NoError(t, err)
		require.Equal(t, attachment.Content, data)
	}
}

func TestHubRejectsRegistrationRacingClose(t *testing.T) {
	t.Parallel()
	for i := 0; i < 10000; i++ {
		h := newHub(newFakeSource(), "host", 7373)
		registered := make(chan *client, 1)
		go func() {
			c, _ := h.register("phone", false)
			registered <- c
		}()
		h.closeAll()
		c := <-registered
		h.mu.Lock()
		remaining := len(h.clients)
		h.mu.Unlock()
		require.Zero(t, remaining, "closed hub accepted a client")
		for range c.ch {
		}
	}
}
