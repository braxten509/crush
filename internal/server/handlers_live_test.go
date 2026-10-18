package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/cliupdate"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/memlimit"
	"github.com/charmbracelet/crush/internal/memwatch"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/selfupdate"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Tests of piece B's routes (engine-design.md 5.3, 5.4 and 5.9).

// liveCall sends one request to the test server and returns its status and
// body. A nil body sends none.
func liveCall(t *testing.T, h *e2eHarness, method, path string, body any) (int, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		require.NoError(t, err)
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, h.httpSrv.URL+path, r)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.httpSrv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, out
}

// liveJSON is liveCall that wants status and decodes the body into T.
func liveJSON[T any](t *testing.T, h *e2eHarness, method, path string, body any, status int) T {
	t.Helper()
	code, data := liveCall(t, h, method, path, body)
	require.Equal(t, status, code, "%s %s: %s", method, path, data)
	var out T
	if len(data) > 0 {
		require.NoError(t, json.Unmarshal(data, &out), "%s", data)
	}
	return out
}

// liveError checks an error answer's status, code and message.
func liveError(t *testing.T, h *e2eHarness, method, path string, body any, status int, code, message string) {
	t.Helper()
	got := liveJSON[proto.Error](t, h, method, path, body, status)
	require.Equal(t, code, got.Code)
	if message != "" {
		require.Equal(t, message, got.Message)
	}
}

// subscribeLiveSSE opens an SSE stream for clientID and returns its raw
// envelopes, so piece B's event types arrive without decodeSSEEnvelope.
func (h *e2eHarness) subscribeLiveSSE(t *testing.T, workspaceID, clientID string) <-chan pubsub.Payload {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	q := url.Values{"client_id": []string{clientID}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		h.httpSrv.URL+"/v1/workspaces/"+workspaceID+"/events?"+q.Encode(), nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := h.httpSrv.Client().Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	out := make(chan pubsub.Payload, 256)
	h.sseWG.Go(func() {
		defer resp.Body.Close()
		defer close(out)
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				return
			}
			data, ok := bytes.CutPrefix(bytes.TrimSpace(line), []byte("data:"))
			if !ok {
				continue
			}
			var p pubsub.Payload
			if json.Unmarshal(bytes.TrimSpace(data), &p) != nil {
				continue
			}
			select {
			case out <- p:
			case <-ctx.Done():
				return
			}
		}
	})
	return out
}

// nextLive waits for the next envelope of type typ and decodes its event.
func nextLive[T any](t *testing.T, events <-chan pubsub.Payload, typ pubsub.PayloadType, timeout time.Duration) (pubsub.Event[T], bool) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case p, ok := <-events:
			if !ok {
				return pubsub.Event[T]{}, false
			}
			if p.Type != typ {
				continue
			}
			var ev pubsub.Event[T]
			require.NoError(t, json.Unmarshal(p.Payload, &ev))
			return ev, true
		case <-deadline:
			return pubsub.Event[T]{}, false
		}
	}
}

// noLive reports whether no envelope of type typ arrives within wait.
func noLive(events <-chan pubsub.Payload, typ pubsub.PayloadType, wait time.Duration) bool {
	deadline := time.After(wait)
	for {
		select {
		case p, ok := <-events:
			if !ok {
				return true
			}
			if p.Type == typ {
				return false
			}
		case <-deadline:
			return true
		}
	}
}

// realLiveWorkspace creates a real workspace through the HTTP route for
// every client ID, all on one folder.
func realLiveWorkspace(t *testing.T, h *e2eHarness, clientIDs ...string) (proto.Workspace, *backend.Workspace) {
	t.Helper()
	t.Setenv("CRUSH_DISABLE_PROVIDER_AUTO_UPDATE", "1")
	dir, dataDir := t.TempDir(), t.TempDir()
	var created proto.Workspace
	for _, id := range clientIDs {
		created = h.postWorkspace(t, proto.Workspace{Path: dir, DataDir: dataDir, ClientID: id})
	}
	ws, err := h.backend.GetWorkspace(created.ID)
	require.NoError(t, err)
	return created, ws
}

// TestShellOutputStreamsToRequesterOnly: a `!` command's output reaches the
// app that ran it as ordered shell_output events whose chunks join into the
// final output; another app gets none (engine-design.md 5.9 a).
func TestShellOutputStreamsToRequesterOnly(t *testing.T) {
	h := newRealCreateHarness(t)
	runner, other := uuid.NewString(), uuid.NewString()
	created, ws := realLiveWorkspace(t, h, runner, other)
	runnerEvents := h.subscribeLiveSSE(t, created.ID, runner)
	otherEvents := h.subscribeLiveSSE(t, created.ID, other)
	h.waitForAttachedOn(t, ws, 2)

	sess := liveJSON[proto.Session](t, h, http.MethodPost, "/v1/workspaces/"+created.ID+"/sessions",
		proto.Session{Title: "shell"}, http.StatusOK)
	shellPath := "/v1/workspaces/" + created.ID + "/agent/sessions/" + sess.ID + "/shell"

	cmd := proto.ShellCommandRequest{Command: "printf 'one\\n'; sleep 0.1; printf 'two €\\n'; sleep 0.1; printf 'three\\n'"}
	resp := liveJSON[proto.ShellCommandResponse](t, h, http.MethodPost,
		shellPath+"?client_id="+runner+"&request_id=req-1", cmd, http.StatusOK)
	require.Equal(t, "one\ntwo €\nthree\n", resp.Output)
	require.Zero(t, resp.ExitCode)

	var joined strings.Builder
	for seq := 0; joined.Len() < len(resp.Output); seq++ {
		ev, ok := nextLive[proto.ShellOutput](t, runnerEvents, pubsub.PayloadTypeShellOutput, 5*time.Second)
		require.True(t, ok, "shell_output %d did not arrive; have %q", seq, joined.String())
		require.Equal(t, seq, ev.Payload.Seq, "events arrive in seq order")
		require.Equal(t, "req-1", ev.Payload.RequestID)
		require.Equal(t, sess.ID, ev.Payload.SessionID)
		joined.WriteString(ev.Payload.Chunk)
	}
	require.Equal(t, resp.Output, joined.String(), "the chunks join into the final output")
	require.True(t, noLive(otherEvents, pubsub.PayloadTypeShellOutput, 300*time.Millisecond), "another app gets no output")

	// Without a request ID nothing streams, and the answer is the same.
	resp = liveJSON[proto.ShellCommandResponse](t, h, http.MethodPost,
		shellPath+"?client_id="+runner, proto.ShellCommandRequest{Command: "printf 'quiet'"}, http.StatusOK)
	require.Equal(t, "quiet", resp.Output)
	require.True(t, noLive(runnerEvents, pubsub.PayloadTypeShellOutput, 300*time.Millisecond))

	liveError(t, h, http.MethodPost, shellPath+"?client_id="+runner+"&request_id="+strings.Repeat("r", 65),
		cmd, http.StatusBadRequest, "bad_request", "request_id must be at most 64 characters")
	liveError(t, h, http.MethodPost, shellPath+"?client_id="+uuid.NewString()+"&request_id=req-2",
		cmd, http.StatusConflict, "client_not_attached", "")
}

// TestEnvIsSetNeverReturnsValues: the answer holds booleans only, and names
// that are not bare variable names are refused (engine-design.md 5.9 b).
func TestEnvIsSetNeverReturnsValues(t *testing.T) {
	const secret = "s3cret-value-never-sent"
	t.Setenv("CRUSH_TEST_ENV_SET", secret)
	t.Setenv("CRUSH_TEST_ENV_EMPTY", "")
	h := newE2EHarness(t)
	path := "/v1/workspaces/" + h.workspace.ID + "/env/is-set"

	code, body := liveCall(t, h, http.MethodPost, path,
		proto.EnvCheck{Names: []string{"CRUSH_TEST_ENV_SET", "CRUSH_TEST_ENV_EMPTY", "CRUSH_TEST_ENV_UNSET"}})
	require.Equal(t, http.StatusOK, code, "%s", body)
	require.NotContains(t, string(body), secret)
	var raw map[string]map[string]any
	require.NoError(t, json.Unmarshal(body, &raw))
	require.Len(t, raw, 1, "the body holds only set: %s", body)
	for name, v := range raw["set"] {
		_, isBool := v.(bool)
		require.True(t, isBool, "%s: %v is not a boolean", name, v)
	}
	require.Equal(t, map[string]any{
		"CRUSH_TEST_ENV_SET":   true,
		"CRUSH_TEST_ENV_EMPTY": false,
		"CRUSH_TEST_ENV_UNSET": false,
	}, raw["set"])

	marker := filepath.Join(t.TempDir(), "ran")
	for _, bad := range []string{"$(id)", "$(touch " + marker + ")", "A=B", "${HOME}", "HOME\n", "", "1ABC", strings.Repeat("A", 129)} {
		liveError(t, h, http.MethodPost, path, proto.EnvCheck{Names: []string{bad}},
			http.StatusBadRequest, "bad_request", backend.ErrBadEnvName.Error())
	}
	require.NoFileExists(t, marker, "nothing is run")
	tooMany := make([]string, 65)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("NAME_%d", i)
	}
	liveError(t, h, http.MethodPost, path, proto.EnvCheck{Names: tooMany}, http.StatusBadRequest, "bad_request", "")
	liveError(t, h, http.MethodPost, "/v1/workspaces/"+uuid.NewString()+"/env/is-set",
		proto.EnvCheck{Names: []string{"HOME"}}, http.StatusNotFound, "not_found", "")
}

// TestClaimMemoryNoticeOncePerWindow: the pop-up claim is shared with every
// Crush window through the cache folder, so only the first claim in 10
// minutes wins (engine-design.md 5.9 c).
func TestClaimMemoryNoticeOncePerWindow(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("memory readings and the pop-up claim are Linux-only")
	}
	t.Setenv("CRUSH_CACHE_DIR", t.TempDir())
	h := newRealCreateHarness(t)

	first := liveJSON[proto.ClaimResult](t, h, http.MethodPost, "/v1/memory/claim-notice", nil, http.StatusOK)
	require.True(t, first.Claimed)
	second := liveJSON[proto.ClaimResult](t, h, http.MethodPost, "/v1/memory/claim-notice", nil, http.StatusOK)
	require.False(t, second.Claimed, "a second claim in the window loses")
	require.False(t, memwatch.ClaimNotify(config.GlobalCacheDir()), "a terminal window shares the claim")

	t.Setenv("CRUSH_CACHE_DIR", t.TempDir())
	again := liveJSON[proto.ClaimResult](t, h, http.MethodPost, "/v1/memory/claim-notice", nil, http.StatusOK)
	require.True(t, again.Claimed, "the claim lives in the cache folder, not in the engine")
}

// TestSetMemoryLimitMatchesTerminal: the route refuses and reports with the
// memory-limit dialog's texts (DLG-269, DLG-270), saves the limit, and
// applies it to the shared box (engine-design.md 5.9 c).
func TestSetMemoryLimitMatchesTerminal(t *testing.T) {
	h := newRealCreateHarness(t)
	t.Setenv("CRUSH_GLOBAL_DATA", t.TempDir())
	type applied struct {
		slice string
		limit uint64
	}
	var mu sync.Mutex
	var calls []applied
	var applyErr error
	t.Cleanup(config.SetMemoryLimitApplyForTest(func(slice string, limit uint64) error {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, applied{slice, limit})
		return applyErr
	}))
	lastApplied := func() applied {
		mu.Lock()
		defer mu.Unlock()
		require.NotEmpty(t, calls)
		return calls[len(calls)-1]
	}

	for value, text := range map[string]string{
		"abc":  "Enter a number of GB, or leave it empty for no limit",
		"-3":   "Enter a number of GB, or leave it empty for no limit",
		"1":    "Enter at least 2 GB, or leave it empty for no limit",
		"1 GB": "Enter at least 2 GB, or leave it empty for no limit",
	} {
		liveError(t, h, http.MethodPut, "/v1/memory/limit", proto.MemoryLimitRequest{Value: value},
			http.StatusBadRequest, "bad_request", text)
	}
	mu.Lock()
	require.Empty(t, calls, "a refused value is neither saved nor applied")
	mu.Unlock()
	_, saved := config.SavedMemoryLimitGB()
	require.False(t, saved)

	for _, tc := range []struct {
		value  string
		gb     float64
		notice string
	}{
		{"8 GB", 8, "Memory limit set to 8 GB for every Crush window"},
		{" 7.5 ", 7.5, "Memory limit set to 7.5 GB for every Crush window"},
		{"", 0, "Memory limit removed"},
		{"0", 0, "Memory limit removed"},
	} {
		got := liveJSON[proto.MemoryLimitResult](t, h, http.MethodPut, "/v1/memory/limit",
			proto.MemoryLimitRequest{Value: tc.value}, http.StatusOK)
		want := config.GBToBytes(tc.gb)
		require.Equal(t, proto.MemoryLimitResult{LimitBytes: want, Applied: true, Notice: tc.notice}, got, "%q", tc.value)
		require.Equal(t, applied{memlimit.Slice, want}, lastApplied())
		gb, ok := config.SavedMemoryLimitGB()
		require.True(t, ok)
		require.Equal(t, tc.gb, gb, "saved to the global config")
	}

	mu.Lock()
	applyErr = errors.New("no systemd here")
	mu.Unlock()
	got := liveJSON[proto.MemoryLimitResult](t, h, http.MethodPut, "/v1/memory/limit",
		proto.MemoryLimitRequest{Value: "4"}, http.StatusOK)
	require.Equal(t, proto.MemoryLimitResult{
		LimitBytes: 4 << 30,
		Applied:    false,
		Notice:     "saved, but couldn't apply the memory limit: no systemd here",
	}, got)
	gb, ok := config.SavedMemoryLimitGB()
	require.True(t, ok)
	require.Equal(t, 4.0, gb, "the limit is saved even when it could not be applied")

	status := liveJSON[proto.MemoryStatus](t, h, http.MethodGet, "/v1/memory", nil, http.StatusOK)
	require.Equal(t, uint64(4<<30), status.LimitBytes, "the status reports the saved limit")
}

// fakeUpdateCLI writes a stand-in AI tool whose `--version` prints current.
func fakeUpdateCLI(t *testing.T, name, current, latest string) cliupdate.CLI {
	t.Helper()
	bin := filepath.Join(t.TempDir(), strings.ToLower(strings.ReplaceAll(name, " ", "-")))
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = --version ]; then echo %q; fi\n", name+" "+current)
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o755))
	return cliupdate.CLI{
		Name:       name,
		Bin:        bin,
		Latest:     func(context.Context, string) (string, error) { return latest, nil },
		UpdateArgs: []string{"update"},
	}
}

// TestCLIUpdateAutoGate: an automatic check runs at most hourly across every
// Crush and leaves out declined versions; a manual one always runs and
// lists everything (engine-design.md 5.9 d).
func TestCLIUpdateAutoGate(t *testing.T) {
	h := newRealCreateHarness(t) // a fresh XDG_CACHE_HOME holds the shared state
	fake := fakeUpdateCLI(t, "Fake CLI", "1.0.0", "1.2.0")
	other := fakeUpdateCLI(t, "Other CLI", "2.0.0", "2.1.0")
	savedCLIs, savedEvery := cliupdate.CLIs, cliupdate.AutoCheckEvery
	cliupdate.CLIs = []cliupdate.CLI{fake, other}
	t.Cleanup(func() { cliupdate.CLIs, cliupdate.AutoCheckEvery = savedCLIs, savedEvery })
	bins := func(c proto.CLIUpdateCheck) []string {
		var out []string
		for _, u := range c.Updates {
			out = append(out, u.Bin)
		}
		return out
	}

	first := liveJSON[proto.CLIUpdateCheck](t, h, http.MethodGet, "/v1/updates/clis", nil, http.StatusOK)
	require.False(t, first.Skipped, "the first automatic check is due")
	require.Equal(t, []string{fake.Bin, other.Bin}, bins(first))
	require.Equal(t, proto.CLIUpdate{Name: "Fake CLI", Bin: fake.Bin, Path: fake.Bin, Current: "1.0.0", Latest: "1.2.0"}, first.Updates[0])

	liveJSON[struct{}](t, h, http.MethodPost, "/v1/updates/clis/decline",
		proto.CLIUpdateBins{Bins: []string{fake.Bin, "/not/offered"}}, http.StatusOK)

	code, body := liveCall(t, h, http.MethodGet, "/v1/updates/clis?manual=false", nil)
	require.Equal(t, http.StatusOK, code)
	require.JSONEq(t, `{"skipped":true,"updates":[]}`, string(body), "within the hour the automatic check is skipped")

	manual := liveJSON[proto.CLIUpdateCheck](t, h, http.MethodGet, "/v1/updates/clis?manual=true", nil, http.StatusOK)
	require.False(t, manual.Skipped, "a manual check is never skipped")
	require.Equal(t, []string{fake.Bin, other.Bin}, bins(manual), "a manual check lists declined versions too")

	cliupdate.AutoCheckEvery = 0 // the hour has passed
	auto := liveJSON[proto.CLIUpdateCheck](t, h, http.MethodGet, "/v1/updates/clis", nil, http.StatusOK)
	require.False(t, auto.Skipped)
	require.Equal(t, []string{other.Bin}, bins(auto), "the automatic check leaves out the declined version")

	liveError(t, h, http.MethodGet, "/v1/updates/clis?manual=maybe", nil, http.StatusBadRequest, "bad_request", "manual must be true or false")
	liveError(t, h, http.MethodPost, "/v1/updates/clis/install?client_id="+uuid.NewString(),
		proto.CLIUpdateBins{Bins: []string{fake.Bin}}, http.StatusConflict, "client_not_attached", "")
	liveError(t, h, http.MethodPost, "/v1/updates/clis/install", proto.CLIUpdateBins{Bins: []string{fake.Bin}},
		http.StatusBadRequest, "bad_request", "")
}

// TestCrushUpdateDeclined: a release declined through the route reports
// declined:true, read from the same record the decline wrote; a newer
// release is offered again (engine-design.md 5.9 d).
func TestCrushUpdateDeclined(t *testing.T) {
	h := newRealCreateHarness(t)
	var mu sync.Mutex
	tag := "v0.70.0"
	t.Cleanup(backend.SetCrushUpdateSourceForTest(
		func() string { return "/fake/crush-checkout" },
		func(context.Context, string) (*selfupdate.Release, error) {
			mu.Lock()
			defer mu.Unlock()
			return &selfupdate.Release{Tag: tag, Current: "v0.69.0", Dir: "/fake/crush-checkout"}, nil
		},
		func(context.Context, string, string) string { return "" },
	))

	check := liveJSON[proto.CrushUpdateCheck](t, h, http.MethodGet, "/v1/updates/crush?manual=true", nil, http.StatusOK)
	require.Equal(t, proto.CrushUpdateCheck{
		HasCheckout: true,
		Release:     &proto.CrushRelease{Tag: "v0.70.0", Current: "v0.69.0", Dir: "/fake/crush-checkout"},
	}, check)

	liveJSON[struct{}](t, h, http.MethodPost, "/v1/updates/crush/decline", proto.CrushTag{Tag: "v0.70.0"}, http.StatusOK)
	check = liveJSON[proto.CrushUpdateCheck](t, h, http.MethodGet, "/v1/updates/crush", nil, http.StatusOK)
	require.True(t, check.Declined, "the declined release reports declined")

	mu.Lock()
	tag = "v0.71.0"
	mu.Unlock()
	check = liveJSON[proto.CrushUpdateCheck](t, h, http.MethodGet, "/v1/updates/crush", nil, http.StatusOK)
	require.False(t, check.Declined, "a newer release is offered again")

	liveError(t, h, http.MethodPost, "/v1/updates/crush/decline", proto.CrushTag{}, http.StatusBadRequest, "bad_request", "tag is required")

	t.Cleanup(backend.SetCrushUpdateSourceForTest(
		func() string { return "" },
		func(context.Context, string) (*selfupdate.Release, error) { panic("no checkout: not asked") },
		func(context.Context, string, string) string { return "" },
	))
	check = liveJSON[proto.CrushUpdateCheck](t, h, http.MethodGet, "/v1/updates/crush", nil, http.StatusOK)
	require.Equal(t, proto.CrushUpdateCheck{}, check, "no fork checkout: nothing to look for")
}

// scriptedFetcher is a usage fetcher whose answers the test hands out one
// call at a time.
type scriptedFetcher struct {
	calls   chan struct{}
	answers chan fetchAnswer
	done    chan struct{}
}

type fetchAnswer struct {
	limits []cliagent.Limit
	err    error
}

func newScriptedFetcher(t *testing.T) *scriptedFetcher {
	f := &scriptedFetcher{calls: make(chan struct{}, 16), answers: make(chan fetchAnswer), done: make(chan struct{})}
	t.Cleanup(func() { close(f.done) })
	return f
}

func (f *scriptedFetcher) fetch(ctx context.Context) ([]cliagent.Limit, error) {
	f.calls <- struct{}{}
	select {
	case a := <-f.answers:
		return a.limits, a.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-f.done:
		return nil, errors.New("test over")
	}
}

// answer waits for the next fetch and answers it.
func (f *scriptedFetcher) answer(t *testing.T, limits []cliagent.Limit, err error) {
	t.Helper()
	select {
	case <-f.calls:
	case <-time.After(5 * time.Second):
		t.Fatal("no usage fetch started")
	}
	f.answers <- fetchAnswer{limits, err}
}

// refreshLimits asks the route for a refresh of kind once the previous one
// ended: a refresh still running would take this one's place.
func refreshLimits(t *testing.T, h *e2eHarness, kind catwalk.Type) {
	t.Helper()
	require.Eventually(t, func() bool {
		for _, k := range cliagent.Snapshot() {
			if k.Kind == kind {
				return !k.Pending
			}
		}
		return false
	}, 5*time.Second, 5*time.Millisecond)
	liveJSON[struct{}](t, h, http.MethodPost, "/v1/limits/"+string(kind)+"/refresh", nil, http.StatusAccepted)
}

// limitsOf finds kind in the limits route's answer.
func limitsOf(t *testing.T, h *e2eHarness, kind catwalk.Type) proto.CLILimits {
	t.Helper()
	snap := liveJSON[proto.LimitsSnapshot](t, h, http.MethodGet, "/v1/limits", nil, http.StatusOK)
	for _, k := range snap.CLIs {
		if k.Kind == string(kind) {
			return k
		}
	}
	t.Fatalf("kind %s not listed in %+v", kind, snap)
	return proto.CLILimits{}
}

// limitsHarness is a server with one workspace whose app streams events,
// and kind's usage answered by the test.
func limitsHarness(t *testing.T, kind catwalk.Type) (*e2eHarness, *scriptedFetcher, <-chan pubsub.Payload) {
	t.Helper()
	fetcher := newScriptedFetcher(t)
	t.Cleanup(cliagent.SetLimitFetchersForTest(map[catwalk.Type]func(context.Context) ([]cliagent.Limit, error){
		kind: fetcher.fetch,
	}))
	h := newE2EHarness(t)
	clientID := uuid.NewString()
	// The first app of the engine's first workspace starts the refresh.
	require.NoError(t, backend.RegisterClientForTesting(h.backend, h.workspace, clientID))
	events := h.subscribeLiveSSE(t, h.workspace.ID, clientID)
	h.waitForAttached(t, 1)
	return h, fetcher, events
}

// TestLimitsCheckingThenReady: an AI tool's limits are checking until the
// first reading, not a reading of 0, then ready with read_at; each change
// is a limits event (engine-design.md 5.9 e).
func TestLimitsCheckingThenReady(t *testing.T) {
	const kind = catwalk.Type("fake-cli")
	h, fetcher, events := limitsHarness(t, kind)

	before := limitsOf(t, h, kind)
	require.Equal(t, proto.CLILimits{Kind: string(kind), State: "checking", Windows: []proto.LimitWindow{}}, before)
	code, body := liveCall(t, h, http.MethodGet, "/v1/limits", nil)
	require.Equal(t, http.StatusOK, code)
	require.NotContains(t, string(body), "read_at", "no reading, no read_at")

	start := time.Now()
	reset := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	fetcher.answer(t, []cliagent.Limit{{Name: "5h", Used: 42, ResetsAt: reset}}, nil)
	ev, ok := nextLive[proto.CLILimits](t, events, pubsub.PayloadTypeLimits, 5*time.Second)
	require.True(t, ok, "the reading sends a limits event")
	require.Equal(t, "ready", ev.Payload.State)
	require.Equal(t, string(kind), ev.Payload.Kind)
	require.False(t, ev.Payload.ReadAt.Before(start.Add(-time.Second)), "read_at is the reading's time")
	require.Len(t, ev.Payload.Windows, 1)
	require.Equal(t, "5h", ev.Payload.Windows[0].Name)
	require.Equal(t, 42.0, ev.Payload.Windows[0].Used)
	require.True(t, ev.Payload.Windows[0].ResetsAt.Equal(reset))

	after := limitsOf(t, h, kind)
	require.Equal(t, "ready", after.State)
	require.True(t, after.ReadAt.Equal(ev.Payload.ReadAt))
	require.Len(t, after.Windows, 1)
	require.Equal(t, 42.0, after.Windows[0].Used)

	refreshLimits(t, h, kind)
	fetcher.answer(t, []cliagent.Limit{{Name: "5h", Used: 50, ResetsAt: reset}}, nil)
	ev, ok = nextLive[proto.CLILimits](t, events, pubsub.PayloadTypeLimits, 5*time.Second)
	require.True(t, ok, "every change sends an event")
	require.Equal(t, 50.0, ev.Payload.Windows[0].Used)
	require.False(t, ev.Payload.ReadAt.Before(after.ReadAt))

	liveError(t, h, http.MethodPost, "/v1/limits/not-a-cli/refresh", nil, http.StatusBadRequest, "bad_request",
		"This CLI does not report usage limits")
}

// TestLimitsErrorState: a failed refresh with no reading is error with its
// message; the time it backs off from is not a reading
// (engine-design.md 5.9 e).
func TestLimitsErrorState(t *testing.T) {
	const kind = catwalk.Type("fake-cli")
	h, fetcher, events := limitsHarness(t, kind)

	fetcher.answer(t, nil, errors.New("usage endpoint is down"))
	ev, ok := nextLive[proto.CLILimits](t, events, pubsub.PayloadTypeLimits, 5*time.Second)
	require.True(t, ok, "a failed refresh sends a limits event")
	require.Equal(t, proto.CLILimits{Kind: string(kind), State: "error", Error: "usage endpoint is down", Windows: []proto.LimitWindow{}}, ev.Payload)
	require.Equal(t, ev.Payload, limitsOf(t, h, kind))
	code, body := liveCall(t, h, http.MethodGet, "/v1/limits", nil)
	require.Equal(t, http.StatusOK, code)
	require.NotContains(t, string(body), "read_at", "the backoff time is not a reading")

	// A refresh that returns no windows is a failure too, not a reading of 0.
	refreshLimits(t, h, kind)
	fetcher.answer(t, nil, nil)
	ev, ok = nextLive[proto.CLILimits](t, events, pubsub.PayloadTypeLimits, 5*time.Second)
	require.True(t, ok)
	require.Equal(t, "error", ev.Payload.State)
	require.Equal(t, "No usage windows were returned by this CLI", ev.Payload.Error)
	require.True(t, ev.Payload.ReadAt.IsZero())

	// A reading clears the error.
	refreshLimits(t, h, kind)
	fetcher.answer(t, []cliagent.Limit{{Name: "Weekly", Used: 5}}, nil)
	ev, ok = nextLive[proto.CLILimits](t, events, pubsub.PayloadTypeLimits, 5*time.Second)
	require.True(t, ok)
	require.Equal(t, "ready", ev.Payload.State)
	require.Empty(t, ev.Payload.Error)
}
