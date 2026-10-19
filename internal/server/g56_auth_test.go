package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/login"
	"github.com/charmbracelet/crush/internal/oauth"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/stretchr/testify/require"
)

// unixCaller serves g's engine on a unix socket, as the desktop app
// reaches it, and calls it there.
type unixCaller struct {
	client *http.Client
}

func newUnixCaller(t *testing.T, g *g56Workspace) *unixCaller {
	t.Helper()
	dir, err := os.MkdirTemp("", "g56")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "e.sock")
	ln, err := net.Listen("unix", sock) //nolint:noctx
	require.NoError(t, err)
	hs := &http.Server{Handler: g.h.srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = hs.Serve(ln) }()
	t.Cleanup(func() { _ = hs.Close() })
	return &unixCaller{client: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		},
	}}}
}

func (u *unixCaller) call(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		require.NoError(t, err)
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, "http://engine"+path, r)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := u.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, out
}

func unixJSON[T any](t *testing.T, u *unixCaller, method, path string, body any, status int) T {
	t.Helper()
	code, data := u.call(t, method, path, body)
	require.Equal(t, status, code, "%s %s: %s", method, path, data)
	var out T
	if len(data) > 0 {
		require.NoError(t, json.Unmarshal(data, &out), "%s", data)
	}
	return out
}

// captureLogs sends every log line of the engine to the returned buffer
// until the test ends.
func captureLogs(t *testing.T, g *g56Workspace) *shareLog {
	t.Helper()
	logs := &shareLog{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	prev := slog.Default()
	slog.SetDefault(logger)
	g.h.srv.SetLogger(logger)
	t.Cleanup(func() { slog.SetDefault(prev) })
	return logs
}

// fakeFlow is a sign-in that finishes when release closes.
type fakeFlow struct {
	release chan struct{}
	token   *oauth.Token
}

func (f *fakeFlow) Start(context.Context) (string, string, error) {
	return "https://login.test/device", "ABCD-1234", nil
}

func (f *fakeFlow) Wait(ctx context.Context) (*oauth.Token, error) {
	select {
	case <-f.release:
		return f.token, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *fakeFlow) Close() {}

// TestAuthFlowSavesLikeCrushLogin: a sign-in started through the route
// saves its token under the provider `crush login` saves it under, with
// the same fields, reports waiting, saving and done, and logs no part of
// the token (G-5).
func TestAuthFlowSavesLikeCrushLogin(t *testing.T) {
	g := newG56Workspace(t)
	logs := captureLogs(t, g)
	u := newUnixCaller(t, g)
	events := g.h.subscribeLiveSSE(t, g.ws.ID, g.app)
	g.h.waitForAttachedOn(t, mustWorkspace(t, g), 1)

	flow := &fakeFlow{release: make(chan struct{}), token: &oauth.Token{
		AccessToken: "access-g56-secret", RefreshToken: "refresh-g56-secret", ExpiresIn: 3600, ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}}
	var platforms []string
	t.Cleanup(backend.SetAuthForTest(func(platform string) (login.Flow, error) {
		platforms = append(platforms, platform)
		return flow, nil
	}, nil))

	started := unixJSON[proto.AuthFlow](t, u, http.MethodPost, g.path("/auth/flows"), proto.AuthFlowRequest{Platform: "copilot"}, http.StatusOK)
	require.Equal(t, []string{"copilot"}, platforms)
	require.NotEmpty(t, started.FlowID)
	require.Equal(t, "https://login.test/device", started.URL)
	require.Equal(t, "ABCD-1234", started.UserCode)
	require.False(t, started.CodeEntry)

	next := func(want string) {
		t.Helper()
		ev, ok := nextLive[proto.AuthFlowEvent](t, events, pubsub.PayloadTypeAuthFlow, 10*time.Second)
		require.True(t, ok, "auth_flow %s", want)
		require.Equal(t, started.FlowID, ev.Payload.FlowID)
		require.Equal(t, want, ev.Payload.State, ev.Payload.Error)
	}
	next(proto.AuthFlowWaiting)
	close(flow.release)
	next(proto.AuthFlowSaving)
	next(proto.AuthFlowDone)

	store := mustWorkspace(t, g).Cfg
	pc, ok := store.Config().Providers.Get(login.ProviderID(login.PlatformCopilot))
	require.True(t, ok)
	require.NotNil(t, pc.OAuthToken)
	require.Equal(t, "access-g56-secret", pc.OAuthToken.AccessToken)
	require.Equal(t, "access-g56-secret", pc.APIKey, "as applyOAuthLogin sets it for crush login")

	data, err := os.ReadFile(store.GlobalDataPath())
	require.NoError(t, err)
	var saved struct {
		Providers map[string]struct {
			APIKey string       `json:"api_key"`
			OAuth  *oauth.Token `json:"oauth"`
		} `json:"providers"`
	}
	require.NoError(t, json.Unmarshal(data, &saved))
	require.Equal(t, "access-g56-secret", saved.Providers["copilot"].APIKey)
	require.NotNil(t, saved.Providers["copilot"].OAuth)
	require.Equal(t, "refresh-g56-secret", saved.Providers["copilot"].OAuth.RefreshToken)

	require.NotContains(t, logs.String(), "g56-secret", "no token in the logs")

	// The flow is over: its code and cancel routes no longer know it.
	code, _ := u.call(t, http.MethodPost, g.path("/auth/flows/"+started.FlowID+"/cancel"), nil)
	require.Equal(t, http.StatusNotFound, code)
}

// TestVerifyKey: a working key is ok, a refused one carries the reason,
// neither is saved or logged (G-5).
func TestVerifyKey(t *testing.T) {
	g := newG56Workspace(t)
	logs := captureLogs(t, g)
	u := newUnixCaller(t, g)

	models := newKeyCheckServer(t, "Bearer good-g56-key")
	setProviderBaseURL(t, g, models)

	path := g.path("/auth/verify-key")
	good := unixJSON[proto.VerifyKey](t, u, http.MethodPost, path, proto.VerifyKeyRequest{ProviderID: "fake", APIKey: "good-g56-key"}, http.StatusOK)
	require.Equal(t, proto.VerifyKey{OK: true}, good)

	bad := unixJSON[proto.VerifyKey](t, u, http.MethodPost, path, proto.VerifyKeyRequest{ProviderID: "fake", APIKey: "bad-g56-key"}, http.StatusOK)
	require.False(t, bad.OK)
	require.Equal(t, "failed to connect to provider fake: 401 Unauthorized", bad.Error)

	code, out := u.call(t, http.MethodPost, path, proto.VerifyKeyRequest{ProviderID: "nobody", APIKey: "x"})
	require.Equal(t, http.StatusNotFound, code, "%s", out)

	// A Google check puts the key in the address it asks; a failed one
	// must not send it back.
	const marker = "G56keyMARKER/+="
	mustWorkspace(t, g).Cfg.Config().Providers.Set("gmarker", config.ProviderConfig{
		ID: "gmarker", Name: "G56 Google", Type: catwalk.TypeGoogle, BaseURL: "http://127.0.0.1:9",
	})
	code, out = u.call(t, http.MethodPost, path, proto.VerifyKeyRequest{ProviderID: "gmarker", APIKey: marker})
	require.Equal(t, http.StatusOK, code, "%s", out)
	var google proto.VerifyKey
	require.NoError(t, json.Unmarshal(out, &google))
	require.False(t, google.OK)
	require.Contains(t, google.Error, "failed to create request for provider gmarker")
	for _, form := range []string{marker, url.QueryEscape(marker), "G56keyMARKER"} {
		require.NotContains(t, string(out), form, "the key is not in the answer")
		require.NotContains(t, logs.String(), form, "the key is not in the logs")
	}

	// A provider that answers 401 with the key, base64-encoded, as its
	// status line's reason and as its body: only the status code is told.
	echoed := base64.StdEncoding.EncodeToString([]byte(marker))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				if _, err := http.ReadRequest(bufio.NewReader(conn)); err != nil {
					return
				}
				fmt.Fprintf(conn, "HTTP/1.1 401 %s\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", echoed, len(echoed), echoed)
			}()
		}
	}()
	mustWorkspace(t, g).Cfg.Config().Providers.Set("smarker", config.ProviderConfig{
		ID: "smarker", Name: "G56 echo", Type: catwalk.TypeOpenAICompat, BaseURL: "http://" + ln.Addr().String() + "/v1",
	})
	code, out = u.call(t, http.MethodPost, path, proto.VerifyKeyRequest{ProviderID: "smarker", APIKey: marker})
	require.Equal(t, http.StatusOK, code, "%s", out)
	var echo proto.VerifyKey
	require.NoError(t, json.Unmarshal(out, &echo))
	require.Equal(t, proto.VerifyKey{Error: "failed to connect to provider smarker: 401 Unauthorized"}, echo)
	for _, form := range []string{marker, url.QueryEscape(marker), "G56keyMARKER", echoed,
		base64.URLEncoding.EncodeToString([]byte(marker)), base64.RawStdEncoding.EncodeToString([]byte(marker))} {
		require.NotContains(t, string(out), form, "the echoed key is not in the answer")
		require.NotContains(t, logs.String(), form, "the echoed key is not in the logs")
	}

	pc, _ := mustWorkspace(t, g).Cfg.Config().Providers.Get("fake")
	require.Equal(t, "test-key", pc.APIKey, "a checked key is not saved")
	require.NotContains(t, logs.String(), "g56-key", "no key in the logs")
}

// secretFlow is a sign-in whose provider answers hold a secret: its start
// fails, its token exchange fails, or its pasted code is refused, each
// with the secret in the error.
type secretFlow struct {
	startErr, waitErr, codeErr error
}

func (f *secretFlow) Start(context.Context) (string, string, error) {
	return "https://login.test/browser", "", f.startErr
}

func (f *secretFlow) Wait(ctx context.Context) (*oauth.Token, error) {
	if f.waitErr != nil {
		return nil, f.waitErr
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (f *secretFlow) Close()          {}
func (f *secretFlow) TakesCode() bool { return f.codeErr != nil }
func (f *secretFlow) CompleteWithCode(context.Context, string) (*oauth.Token, error) {
	return nil, f.codeErr
}

// TestAuthFlowErrorsHideSecrets: what a provider answers on a failed
// sign-in (a token exchange's body, a start error, a refused code) and a
// pasted code reach no event, log line or answer; the apps get fixed
// texts (G-5).
func TestAuthFlowErrorsHideSecrets(t *testing.T) {
	g := newG56Workspace(t)
	logs := captureLogs(t, g)
	u := newUnixCaller(t, g)
	events := g.h.subscribeLiveSSE(t, g.ws.ID, g.app)
	g.h.waitForAttachedOn(t, mustWorkspace(t, g), 1)

	const marker = "G56-SECRET-MARKER"
	var next *secretFlow
	t.Cleanup(backend.SetAuthForTest(func(string) (login.Flow, error) { return next, nil }, nil))
	var answers strings.Builder
	call := func(path string, body any) (int, []byte) {
		code, out := u.call(t, http.MethodPost, g.path(path), body)
		answers.Write(out)
		return code, out
	}
	failed := func(flowID string) string {
		t.Helper()
		for {
			ev, ok := nextLive[proto.AuthFlowEvent](t, events, pubsub.PayloadTypeAuthFlow, 10*time.Second)
			require.True(t, ok)
			require.NotContains(t, ev.Payload.Error, marker)
			if ev.Payload.FlowID == flowID && ev.Payload.State == proto.AuthFlowFailed {
				return ev.Payload.Error
			}
		}
	}

	next = &secretFlow{startErr: errors.New("start refused: " + marker)}
	code, out := call("/auth/flows", proto.AuthFlowRequest{Platform: "hyper"})
	require.Equal(t, http.StatusBadGateway, code, "%s", out)
	require.Contains(t, string(out), "failed to initiate device auth")

	next = &secretFlow{waitErr: &oauth.TokenExchangeError{StatusCode: 400, Body: `{"error":"` + marker + `"}`}}
	code, out = call("/auth/flows", proto.AuthFlowRequest{Platform: "copilot"})
	require.Equal(t, http.StatusOK, code, "%s", out)
	var flow proto.AuthFlow
	require.NoError(t, json.Unmarshal(out, &flow))
	require.Equal(t, "token exchange failed", failed(flow.FlowID))

	next = &secretFlow{codeErr: errors.New("code refused: " + marker)}
	code, out = call("/auth/flows", proto.AuthFlowRequest{Platform: "grok"})
	require.Equal(t, http.StatusOK, code, "%s", out)
	require.NoError(t, json.Unmarshal(out, &flow))
	require.True(t, flow.CodeEntry)
	code, out = call("/auth/flows/"+flow.FlowID+"/code", map[string]any{"code": 7, "note": marker})
	require.Equal(t, http.StatusBadRequest, code, "a malformed body: %s", out)
	code, out = call("/auth/flows/"+flow.FlowID+"/code", proto.AuthCodeRequest{Code: marker})
	require.Equal(t, http.StatusOK, code, "%s", out)
	require.Equal(t, "the code was not accepted", failed(flow.FlowID))

	require.NotContains(t, answers.String(), marker, "no answer holds the secret")
	require.NotContains(t, logs.String(), marker, "no log line holds the secret")
}

// TestAuthRoutesRefuseTCP: on a TCP listener every sign-in route answers
// 403 local_only and starts nothing (G-5).
func TestAuthRoutesRefuseTCP(t *testing.T) {
	g := newG56Workspace(t)
	t.Cleanup(backend.SetAuthForTest(func(string) (login.Flow, error) {
		t.Error("a sign-in started over TCP")
		return nil, login.ErrNoCodeEntry
	}, func(string, string) error {
		t.Error("a key was checked over TCP")
		return nil
	}))
	for _, route := range []struct {
		path string
		body any
	}{
		{"/auth/flows", proto.AuthFlowRequest{Platform: "copilot"}},
		{"/auth/flows/f1/code", proto.AuthCodeRequest{Code: "123"}},
		{"/auth/flows/f1/cancel", nil},
		{"/auth/verify-key", proto.VerifyKeyRequest{ProviderID: "fake", APIKey: "k"}},
	} {
		liveError(t, g.h, http.MethodPost, g.path(route.path), route.body, http.StatusForbidden, "local_only", "")
	}
}

// newKeyCheckServer answers GET /v1/models with 200 for the one accepted
// Authorization header and 401 for any other.
func newKeyCheckServer(t *testing.T, accepted string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/models") || r.Header.Get("Authorization") != accepted {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/v1"
}

// setProviderBaseURL points the workspace's fake provider at url.
func setProviderBaseURL(t *testing.T, g *g56Workspace, url string) {
	t.Helper()
	store := mustWorkspace(t, g).Cfg
	pc, ok := store.Config().Providers.Get("fake")
	require.True(t, ok)
	pc.BaseURL = url
	store.Config().Providers.Set("fake", pc)
}
