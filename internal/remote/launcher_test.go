package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testLauncher(t *testing.T, started *[]string) (*launcher, http.Handler, *time.Time) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "remote-password")
	require.NoError(t, SetPassword(file, "correct horse"))
	clock := time.Unix(1_800_000_000, 0)
	l := &launcher{
		host: "pc", passwordFile: file, now: func() time.Time { return clock },
		command: func(id string) (*exec.Cmd, error) {
			*started = append(*started, id)
			return exec.Command("true"), nil
		},
	}
	g := &gate{user: ownerUser, whois: func(context.Context, string) (peer, error) { return peer{User: ownerUser, Name: "pixel-10"}, nil }}
	return l, l.routes(g), &clock
}

func launch(h http.Handler, password string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"password": password})
	r := httptest.NewRequest(http.MethodPost, "/v1/launch", strings.NewReader(string(body)))
	r.RemoteAddr = phoneAddr + ":1234"
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestLauncherOpensAWindowOnlyForThePassword(t *testing.T) {
	t.Parallel()
	var started []string
	_, h, _ := testLauncher(t, &started)

	w := launch(h, "wrong one")
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Contains(t, w.Body.String(), "Wrong password.")
	require.Empty(t, started)

	w = launch(h, "correct horse")
	require.Equal(t, http.StatusOK, w.Code)
	var out map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	require.Len(t, started, 1)
	require.Equal(t, started[0], out["launch"])
	require.Len(t, out["launch"], 16)
}

func TestLauncherLocksOutAfterWrongPasswords(t *testing.T) {
	t.Parallel()
	var started []string
	_, h, clock := testLauncher(t, &started)
	for range maxWrongTries {
		require.Equal(t, http.StatusUnauthorized, launch(h, "guess").Code)
	}
	// Locked: even the right password waits out the minute.
	w := launch(h, "correct horse")
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.Empty(t, started)
	*clock = clock.Add(lockout + time.Second)
	require.Equal(t, http.StatusOK, launch(h, "correct horse").Code)
	require.Len(t, started, 1)
}

func TestLauncherRefusesOtherDevicesBrowsersAndNoPassword(t *testing.T) {
	t.Parallel()
	var started []string
	l, _, _ := testLauncher(t, &started)
	other := &gate{user: ownerUser, whois: func(context.Context, string) (peer, error) { return peer{User: ownerUser + 1}, nil }}
	require.Equal(t, http.StatusForbidden, launch(l.routes(other), "correct horse").Code)

	g := &gate{user: ownerUser, whois: func(context.Context, string) (peer, error) { return peer{User: ownerUser}, nil }}
	r := httptest.NewRequest(http.MethodPost, "/v1/launch", strings.NewReader(`{"password":"correct horse"}`))
	r.RemoteAddr = phoneAddr + ":1234"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://evil.example")
	w := httptest.NewRecorder()
	l.routes(g).ServeHTTP(w, r)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Empty(t, started)

	require.NoError(t, os.Remove(l.passwordFile))
	w = launch(l.routes(g), "correct horse")
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.Contains(t, w.Body.String(), "crush remote password")

	r = httptest.NewRequest(http.MethodGet, "/v1/launcher", nil)
	r.RemoteAddr = phoneAddr + ":1234"
	w = httptest.NewRecorder()
	l.routes(g).ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"password_set":false`)
}

func TestPasswordIsStoredAsAHashOwnerOnly(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "remote-password")
	require.Error(t, SetPassword(file, "short"))
	require.NoError(t, SetPassword(file, "correct horse"))
	b, err := os.ReadFile(file)
	require.NoError(t, err)
	require.NotContains(t, string(b), "correct horse")
	require.True(t, strings.HasPrefix(string(b), "$2"))
	st, err := os.Stat(file)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), st.Mode().Perm())
}

func TestDefaultLaunchCommandPassesTheLaunchID(t *testing.T) {
	t.Setenv("CRUSH_LAUNCH_COMMAND", `echo "$CRUSH_REMOTE_LAUNCH $CRUSH_EXE"`)
	cmd, err := DefaultLaunchCommand("/bin/crush", t.TempDir())("abc123")
	require.NoError(t, err)
	out, err := cmd.Output()
	require.NoError(t, err)
	require.Equal(t, "abc123 /bin/crush\n", string(out))
}
