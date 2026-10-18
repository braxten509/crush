package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/proto/prototest"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/remote"
	"github.com/charmbracelet/crush/internal/secureentry"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Secure entry has one form per process, so these tests do not run in
// parallel.

// secureSecret is the value the tests enter.
const secureSecret = "S3CR3T-7f1c"

// secureAsk opens a secure form for sessionID as `crush ask` does, with one
// secure field labelled "API key" whose destination is dest, a new file.
func secureAsk(t *testing.T, sessionID, dest string) *secureentry.Form {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(dest), 0o700))
	require.NoError(t, os.WriteFile(dest, []byte("KEY=%s\n"), 0o600))
	req := question.Request{ID: uuid.NewString(), SessionID: sessionID, Questions: []question.Question{
		{ID: "key", Type: question.TypeSecureEntry, Label: "API key", Text: "Paste the API key"},
	}}
	form, err := secureentry.OpenForm(sessionID, req, map[string]secureentry.Spec{"key": {File: dest, Label: "API key", Occurrence: 1}})
	require.NoError(t, err)
	t.Cleanup(form.Cancel)
	return form
}

// secureSubmitPath is the submit route of formID.
func (h *shareHarness) secureSubmitPath(formID string) string {
	return h.wsPath("/secure-forms/" + formID + "/submit")
}

// secureValues is a submit body that enters value into the field "key".
func secureValues(value string) proto.SecureSubmit {
	return proto.SecureSubmit{Values: map[string]string{"key": value}}
}

// secureResult waits for a form's result.
func secureResult(t *testing.T, form *secureentry.Form) secureentry.FormResult {
	t.Helper()
	select {
	case result := <-form.Result():
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("the form did not close")
		return secureentry.FormResult{}
	}
}

// secureNoLeak checks that no part of the secret is in data.
func secureNoLeak(t *testing.T, where string, data []byte) {
	t.Helper()
	for _, part := range []string{secureSecret, "S3CR3T", "7f1c"} {
		require.NotContains(t, string(data), part, "%s has part of the secret", where)
	}
}

// secureCaptureLogs sends the process's default log to a buffer until the
// test ends.
func secureCaptureLogs(t *testing.T) *shareLog {
	t.Helper()
	logs := &shareLog{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return logs
}

// newSecureHarness serves a synthetic workspace connected to secure entry,
// as its first interactive agent start connects it.
func newSecureHarness(t *testing.T) *shareHarness {
	t.Helper()
	h := newShareHarness(t)
	backend.StartSecureEntryForTest(h.backend, h.ws)
	return h
}

// newSecureEngineHarness serves a workspace the engine created for apps
// clientIDs as it creates any, with a real app and database, connected to
// secure entry. It sets environment variables, so its test cannot run in
// parallel.
func newSecureEngineHarness(t *testing.T, clientIDs ...string) *shareHarness {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("CRUSH_GLOBAL_CONFIG", filepath.Join(home, "config", "crush"))
	t.Setenv("CRUSH_GLOBAL_DATA", filepath.Join(home, "data", "crush"))
	t.Setenv("CRUSH_DISABLE_PROVIDER_AUTO_UPDATE", "1")

	h := &shareHarness{}
	h.serve(t)
	h.backend.SetCreateGrace(200 * time.Millisecond)
	h.backend.SetDetachGrace(200 * time.Millisecond)
	args := proto.Workspace{Path: t.TempDir(), DataDir: t.TempDir()}
	var id string
	for _, clientID := range clientIDs {
		args.ClientID = clientID
		var created proto.Workspace
		h.expectOK(t, http.MethodPost, "/v1/workspaces", "", prototest.CreateRequest(args), &created)
		h.apps = append(h.apps, clientID)
		require.True(t, id == "" || id == created.ID, "one workspace per folder")
		id = created.ID
	}
	ws, err := h.backend.GetWorkspace(id)
	require.NoError(t, err)
	// Skip the full app shutdown, as the other engine tests do, but
	// release the database so its folder can be removed.
	dataDir := ws.Cfg.Config().Options.DataDirectory
	backend.SetWorkspaceShutdownFnForTest(ws, func() { _ = db.Release(dataDir) })
	h.ws = ws
	backend.StartSecureEntryForTest(h.backend, ws)
	return h
}

// TestSecureFormGoesOnlyToViewer: the app that views the form's chat gets
// secure_form; an app on another chat gets nothing.
func TestSecureFormGoesOnlyToViewer(t *testing.T) {
	h := newSecureHarness(t)
	viewer, viewerEvents := h.stream(t)
	other, otherEvents := h.stream(t)
	h.view(t, viewer, "chat-1")
	h.view(t, other, "chat-2")

	secureAsk(t, "chat-1", filepath.Join(t.TempDir(), "secret.env"))
	var form proto.SecureForm
	viewerEvents.waitFor(t, pubsub.PayloadTypeSecureForm, &form)
	require.Equal(t, "chat-1", form.SessionID)
	h.mark()
	otherEvents.quietUntilMarker(t, pubsub.PayloadTypeSecureForm)

	var listed []proto.SecureForm
	h.expectOK(t, http.MethodGet, h.wsPath("/secure-forms"), other, nil, &listed)
	require.Empty(t, listed)
	h.expectOK(t, http.MethodGet, h.wsPath("/secure-forms"), viewer, nil, &listed)
	require.Len(t, listed, 1)
	require.Equal(t, form.FormID, listed[0].FormID)
}

// TestSecureFormEventHasNoPath: the event and the list name the field by its
// label, never by its destination.
func TestSecureFormEventHasNoPath(t *testing.T) {
	h := newSecureHarness(t)
	viewer, events := h.stream(t)
	h.view(t, viewer, "chat-1")
	dest := filepath.Join(t.TempDir(), "deep", "secret.env")

	secureAsk(t, "chat-1", dest)
	ev := events.wait(t, pubsub.PayloadTypeSecureForm)
	_, listed := h.do(t, http.MethodGet, h.wsPath("/secure-forms"), viewer, nil)
	for where, data := range map[string][]byte{"the secure_form event": ev.line, "the list": listed} {
		require.Contains(t, string(data), "API key", where)
		require.NotContains(t, string(data), dest, where)
		require.NotContains(t, string(data), filepath.Dir(dest), where)
		require.NotContains(t, string(data), "secret.env", where)
	}
}

// TestSecureValueNeverLeaks: a submitted value reaches its file and nothing
// else: not the apps' streams, not the logs, not the app's events and not a
// phone that shares the chat.
func TestSecureValueNeverLeaks(t *testing.T) {
	logs := secureCaptureLogs(t)
	submitter, watcher := uuid.NewString(), uuid.NewString()
	h := newSecureEngineHarness(t, submitter, watcher)
	submitterEvents := h.streamAs(t, submitter)
	watcherEvents := h.streamAs(t, watcher)
	sess, err := h.ws.Sessions.Create(t.Context(), "Secure entry")
	require.NoError(t, err)
	h.view(t, submitter, sess.ID)
	h.view(t, watcher, sess.ID)

	// Everything the engine's app publishes.
	var fanIn bytes.Buffer
	var fanInMu sync.Mutex
	fanInCtx, stopFanIn := context.WithCancel(context.Background())
	appEvents := h.ws.App.Events(fanInCtx)
	fanInDone := make(chan struct{})
	go func() {
		defer close(fanInDone)
		for ev := range appEvents {
			fanInMu.Lock()
			fmt.Fprintf(&fanIn, "%+v\n", ev)
			fanInMu.Unlock()
		}
	}()
	t.Cleanup(func() {
		stopFanIn()
		<-fanInDone
	})

	// A phone that shares the chat, on a hub built on the engine's Source.
	phone, phoneURL, err := remote.StartTestShare(engineShareSource(h.ws), "")
	require.NoError(t, err)
	t.Cleanup(phone.Stop)
	phone.SetPresence(remote.Presence{SessionID: sess.ID, Focused: true})
	phoneCtx, stopPhone := context.WithCancel(context.Background())
	t.Cleanup(stopPhone)
	phoneReq, err := http.NewRequestWithContext(phoneCtx, http.MethodGet, phoneURL+"/v1/events", nil)
	require.NoError(t, err)
	phoneResp, err := http.DefaultClient.Do(phoneReq)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, phoneResp.StatusCode)
	phoneStream := &shareStream{events: make(chan shareEvent, 1024)}
	h.readers.Go(func() {
		defer phoneResp.Body.Close()
		phoneStream.read(phoneResp.Body)
	})
	require.Eventually(t, func() bool { return bytes.Contains(phoneStream.bytes(), []byte(`"focused":true`)) },
		5*time.Second, 10*time.Millisecond, "the phone shares the chat")

	dest := filepath.Join(t.TempDir(), "secret.env")
	form := secureAsk(t, sess.ID, dest)
	var sent proto.SecureForm
	submitterEvents.waitFor(t, pubsub.PayloadTypeSecureForm, &sent)
	watcherEvents.wait(t, pubsub.PayloadTypeSecureForm)
	h.expectOK(t, http.MethodPost, h.secureSubmitPath(sent.FormID), submitter, secureValues(secureSecret), nil)
	result := secureResult(t, form)
	require.Equal(t, secureentry.StatusSaved, result.Statuses["key"])
	data, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, "KEY="+secureSecret+"\n", string(data))

	// Let everything after the submit arrive: the closed events and the
	// marker on the apps' streams, a presence change on the phone's.
	submitterEvents.wait(t, pubsub.PayloadTypeSecureFormClosed)
	watcherEvents.wait(t, pubsub.PayloadTypeSecureFormClosed)
	h.mark()
	submitterEvents.wait(t, shareMarker)
	watcherEvents.wait(t, shareMarker)
	phone.SetPresence(remote.Presence{SessionID: sess.ID})
	require.Eventually(t, func() bool { return bytes.Contains(phoneStream.bytes(), []byte(`"focused":false`)) },
		5*time.Second, 10*time.Millisecond, "the phone got the change")

	secureNoLeak(t, "the submitting app's stream", submitterEvents.bytes())
	secureNoLeak(t, "the other app's stream", watcherEvents.bytes())
	secureNoLeak(t, "the phone's stream", phoneStream.bytes())
	secureNoLeak(t, "the server log", []byte(h.logs.String()))
	secureNoLeak(t, "the process log", []byte(logs.String()))
	fanInMu.Lock()
	secureNoLeak(t, "the app's events", fanIn.Bytes())
	fanInMu.Unlock()
}

// TestSecureSubmitDecodeErrorNotLogged: a body that does not decode gets 400
// with fixed text, and no part of it reaches a log.
func TestSecureSubmitDecodeErrorNotLogged(t *testing.T) {
	logs := secureCaptureLogs(t)
	h := newSecureHarness(t)
	viewer, events := h.stream(t)
	h.view(t, viewer, "chat-1")
	form := secureAsk(t, "chat-1", filepath.Join(t.TempDir(), "secret.env"))
	var sent proto.SecureForm
	events.waitFor(t, pubsub.PayloadTypeSecureForm, &sent)

	for _, body := range []string{
		`{"values":{"key":` + secureSecret + `}}`,
		`{"values":{"key":"` + secureSecret,
		`{"values":["` + secureSecret + `"]}`,
	} {
		status, out := h.do(t, http.MethodPost, h.secureSubmitPath(sent.FormID), viewer, body)
		require.Equal(t, http.StatusBadRequest, status, body)
		require.JSONEq(t, `{"message":"could not read the secure entry","code":"bad_request"}`, string(out))
	}
	for where, log := range map[string]string{"the server log": h.logs.String(), "the process log": logs.String()} {
		secureNoLeak(t, where, []byte(log))
		require.NotContains(t, log, "invalid character", where)
		require.NotContains(t, log, "unexpected EOF", where)
		require.NotContains(t, log, "cannot unmarshal", where)
	}
	require.False(t, form.Closed(), "the app can submit again")
}

// TestSecureSubmitByNonRecipientRefused: an app the form was not sent to
// cannot answer it.
func TestSecureSubmitByNonRecipientRefused(t *testing.T) {
	h := newSecureHarness(t)
	viewer, events := h.stream(t)
	other, _ := h.stream(t)
	h.view(t, viewer, "chat-1")
	dest := filepath.Join(t.TempDir(), "secret.env")
	form := secureAsk(t, "chat-1", dest)
	var sent proto.SecureForm
	events.waitFor(t, pubsub.PayloadTypeSecureForm, &sent)

	h.expectError(t, http.MethodPost, h.secureSubmitPath(sent.FormID), other, secureValues(secureSecret), http.StatusForbidden, "not_recipient")
	h.expectError(t, http.MethodPost, h.wsPath("/secure-forms/"+sent.FormID+"/cancel"), other, nil, http.StatusForbidden, "not_recipient")
	h.expectError(t, http.MethodPost, h.secureSubmitPath(uuid.NewString()), viewer, secureValues(secureSecret), http.StatusNotFound, "secure_form_not_found")
	require.False(t, form.Closed())
	data, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, "KEY=%s\n", string(data), "nothing was written")
}

// TestSecureFieldErrorHasField: a value secure entry refuses answers 422
// with the field's question ID, and the form stays open.
func TestSecureFieldErrorHasField(t *testing.T) {
	h := newSecureHarness(t)
	viewer, events := h.stream(t)
	h.view(t, viewer, "chat-1")
	form := secureAsk(t, "chat-1", filepath.Join(t.TempDir(), "secret.env"))
	var sent proto.SecureForm
	events.waitFor(t, pubsub.PayloadTypeSecureForm, &sent)

	perr := h.expectError(t, http.MethodPost, h.secureSubmitPath(sent.FormID), viewer, secureValues("bad\nvalue"), http.StatusUnprocessableEntity, "secure_field_error")
	require.Equal(t, proto.Error{Message: "enter a single-line value without control characters", Code: "secure_field_error", Field: "key"}, perr)
	require.False(t, form.Closed())

	h.expectOK(t, http.MethodPost, h.secureSubmitPath(sent.FormID), viewer, secureValues("good"), nil)
	require.Equal(t, secureentry.StatusSaved, secureResult(t, form).Statuses["key"])
}

// TestSecureFormClosedAnsweredElsewhere: when one viewer submits, it hears
// submitted and the other answered_elsewhere.
func TestSecureFormClosedAnsweredElsewhere(t *testing.T) {
	h := newSecureHarness(t)
	submitter, submitterEvents := h.stream(t)
	other, otherEvents := h.stream(t)
	h.view(t, submitter, "chat-1")
	h.view(t, other, "chat-1")
	form := secureAsk(t, "chat-1", filepath.Join(t.TempDir(), "secret.env"))
	var sent proto.SecureForm
	submitterEvents.waitFor(t, pubsub.PayloadTypeSecureForm, &sent)
	otherEvents.wait(t, pubsub.PayloadTypeSecureForm)

	h.expectOK(t, http.MethodPost, h.secureSubmitPath(sent.FormID), submitter, secureValues("v4lue"), nil)
	secureResult(t, form)
	var closed proto.SecureFormClosed
	submitterEvents.waitFor(t, pubsub.PayloadTypeSecureFormClosed, &closed)
	require.Equal(t, proto.SecureFormClosed{FormID: sent.FormID, Reason: "submitted"}, closed)
	otherEvents.waitFor(t, pubsub.PayloadTypeSecureFormClosed, &closed)
	require.Equal(t, proto.SecureFormClosed{FormID: sent.FormID, Reason: "answered_elsewhere"}, closed)
	h.expectError(t, http.MethodPost, h.wsPath("/secure-forms/"+sent.FormID+"/cancel"), other, nil, http.StatusNotFound, "secure_form_not_found")
}

// secureTripwire is a request body that counts its reads.
type secureTripwire struct{ reads atomic.Int32 }

func (b *secureTripwire) Read(p []byte) (int, error) {
	b.reads.Add(1)
	return copy(p, `{"values":{"key":"`+secureSecret+`"}}`), io.EOF
}

func (b *secureTripwire) Close() error { return nil }

// TestSecureRoutesRefuseNonUnixListener: on a TCP listener, the three
// secure entry routes answer 403 local_only before they read the body; on
// the engine's unix socket, submit works.
func TestSecureRoutesRefuseNonUnixListener(t *testing.T) {
	h := newSecureHarness(t)
	viewer, events := h.stream(t)
	h.view(t, viewer, "chat-1")
	dest := filepath.Join(t.TempDir(), "secret.env")
	form := secureAsk(t, "chat-1", dest)
	var sent proto.SecureForm
	events.waitFor(t, pubsub.PayloadTypeSecureForm, &sent)

	routes := []struct{ method, path string }{
		{http.MethodGet, h.wsPath("/secure-forms")},
		{http.MethodPost, h.secureSubmitPath(sent.FormID)},
		{http.MethodPost, h.wsPath("/secure-forms/" + sent.FormID + "/cancel")},
	}
	tcp := httptest.NewServer(h.srv.Handler())
	t.Cleanup(tcp.Close)
	for _, route := range routes {
		url := tcp.URL + route.path + "?client_id=" + viewer
		var body io.Reader
		if route.method == http.MethodPost {
			body = strings.NewReader(`{"values":{"key":"` + secureSecret + `"}}`)
		}
		req, err := http.NewRequestWithContext(t.Context(), route.method, url, body)
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		resp, err := tcp.Client().Do(req)
		require.NoError(t, err)
		out, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		require.NoError(t, err)
		require.Equal(t, http.StatusForbidden, resp.StatusCode, "%s %s: %s", route.method, route.path, out)
		require.Contains(t, string(out), `"code":"local_only"`, route.path)

		// The same request as a TCP listener hands it over: the body is
		// never read.
		tripwire := &secureTripwire{}
		ctx := context.WithValue(t.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4000})
		direct := httptest.NewRequestWithContext(ctx, route.method, shareBase+route.path+"?client_id="+viewer, tripwire)
		direct.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.srv.Handler().ServeHTTP(rec, direct)
		require.Equal(t, http.StatusForbidden, rec.Code, route.path)
		require.Zero(t, tripwire.reads.Load(), "%s read the body before it checked the listener", route.path)
	}
	require.False(t, form.Closed())
	data, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, "KEY=%s\n", string(data), "nothing was written over TCP")

	h.expectOK(t, http.MethodPost, h.secureSubmitPath(sent.FormID), viewer, secureValues(secureSecret), nil)
	require.Equal(t, secureentry.StatusSaved, secureResult(t, form).Statuses["key"])
	data, err = os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, "KEY="+secureSecret+"\n", string(data))
}
