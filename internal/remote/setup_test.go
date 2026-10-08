package remote

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type setupTestAPI struct {
	tailnetAPI
	value tailnetStatus
	err   error
}

func (s setupTestAPI) status(context.Context) (tailnetStatus, error) { return s.value, s.err }

func setupStatus(state string, owner int64, ip string) tailnetStatus {
	st := tailnetStatus{BackendState: state}
	st.Self.HostName, st.Self.UserID = "test-computer", owner
	if ip != "" {
		st.Self.TailscaleIPs = []netip.Addr{netip.MustParseAddr(ip)}
	}
	return st
}

func TestSetupStates(t *testing.T) {
	t.Parallel()
	for _, platform := range []string{"linux", "darwin"} {
		for _, tc := range []struct {
			name      string
			status    tailnetStatus
			installed bool
			err       error
			want      SetupState
			action    string
		}{
			{"missing", tailnetStatus{}, false, errors.New("missing socket"), SetupMissing, "install"},
			{"not started", tailnetStatus{}, true, errors.New("private login token"), SetupUnavailable, "start"},
			{"signed out", setupStatus("NeedsLogin", 0, ""), true, nil, SetupLogin, "connect"},
			{"stopped", setupStatus("Stopped", 7, "100.64.1.2"), true, nil, SetupStopped, "connect"},
			{"approval", setupStatus("NeedsMachineAuth", 7, "100.64.1.2"), true, nil, SetupApproval, ""},
			{"starting", setupStatus("Starting", 7, ""), true, nil, SetupWaiting, ""},
			{"ready", setupStatus("Running", 7, "100.64.1.2"), true, nil, SetupReady, ""},
			{"daemon without CLI", setupStatus("Running", 7, "100.64.1.2"), false, nil, SetupReady, ""},
			{"tagged", setupStatus("Running", 0, "100.64.1.2"), true, nil, SetupWaiting, ""},
			{"IPv6 only", setupStatus("Running", 7, "fd7a:115c:a1e0::1"), true, nil, SetupWaiting, ""},
			{"not a tailnet IP", setupStatus("Running", 7, "192.168.1.2"), true, nil, SetupWaiting, ""},
		} {
			t.Run(platform+"/"+tc.name, func(t *testing.T) {
				info := inspectSetup(t.Context(), platform, setupTestAPI{value: tc.status, err: tc.err}, tc.installed)
				require.Equal(t, tc.want, info.State)
				require.Equal(t, tc.action, info.Action)
				require.NotContains(t, info.Detail, "private login token")
			})
		}
	}
}

func TestMacTailscaleDiscovery(t *testing.T) {
	t.Parallel()
	for _, available := range []string{
		"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
		"/Applications/Tailscale.app/Contents/MacOS/tailscale",
		"tailscale",
	} {
		path, err := tailscaleExecutable("darwin", func(name string) (string, error) {
			if name == available || name == "tailscale" {
				return name, nil
			}
			return "", exec.ErrNotFound
		})
		require.NoError(t, err)
		require.Equal(t, available, path)
	}
	_, err := tailscaleExecutable("linux", func(name string) (string, error) {
		require.Equal(t, "tailscale", name)
		return "", exec.ErrNotFound
	})
	require.Error(t, err)
}

func TestCLIAccountChecks(t *testing.T) {
	t.Parallel()
	// Exercise the actual CLI JSON adapter used by macOS without a Tailscale
	// installation, login, app launch, or network connection.
	path := filepath.Join(t.TempDir(), "tailscale")
	require.NoError(t, os.WriteFile(path, []byte(`#!/bin/sh
case "$*" in
  'status --json') printf '%s' '{"BackendState":"Running","Self":{"HostName":"mac","UserID":7,"TailscaleIPs":["100.64.1.2"]}}' ;;
  'whois --json 100.71.87.38:5000') printf '%s' '{"Node":{"ComputedName":"phone"},"UserProfile":{"ID":7}}' ;;
  'whois --json 100.99.1.2:5000') printf '%s' '{"Node":{"Name":"stranger"},"UserProfile":{"ID":8}}' ;;
  *) printf '%s' '{"Node":{"Name":"unknown"}}' ;;
esac
`), 0700))
	api := cliAPI{path: path}
	me, err := api.self(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(7), me.User)
	require.Equal(t, "mac", me.Host)
	g := gate{whois: api.whois, user: me.User}
	p, err := g.check(t.Context(), "100.71.87.38:5000")
	require.NoError(t, err)
	require.Equal(t, "phone", p.Name)
	_, err = g.check(t.Context(), "100.99.1.2:5000")
	require.Error(t, err)
	_, err = g.check(t.Context(), "100.99.1.3:5000")
	require.Error(t, err, "missing ownership must never authorize access")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = api.self(ctx)
	require.Error(t, err)
}

func TestCLIErrorDoesNotRevealOutput(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "tailscale")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\necho 'secret-auth-url' >&2\nexit 1\n"), 0700))
	_, err := (cliAPI{path: path}).self(t.Context())
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret-auth-url")
}

func TestStatusUsesSameJSONForBothPlatforms(t *testing.T) {
	t.Parallel()
	var st tailnetStatus
	require.NoError(t, json.Unmarshal([]byte(`{"BackendState":"Running","Self":{"UserID":19,"HostName":"linux","TailscaleIPs":["fd7a:115c:a1e0::1","100.64.1.3"]}}`), &st))
	require.Equal(t, SetupReady, setupFromStatus(st).State)
	require.Equal(t, "100.64.1.3", st.self().IPv4.String())
}

func TestServiceStartDoesNotResetSettings(t *testing.T) {
	t.Parallel()
	for _, available := range []string{"systemctl", "rc-service", ""} {
		name, args, err := serviceStartCommand(func(name string) (string, error) {
			if name == available {
				return "/bin/" + name, nil
			}
			return "", exec.ErrNotFound
		})
		if available == "" {
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
		require.Equal(t, "/bin/"+available, name)
		require.Contains(t, args, "start")
		require.Contains(t, args, "tailscaled")
		require.NotContains(t, strings.Join(args, " "), "reset")
	}
}

func TestInstallerDownloadsRejectBadResponses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body, contentType string
		status                  int
		ok                      bool
	}{
		{"script", "#!/bin/sh\nexit 0\n", "text/plain", 200, true},
		{"empty", "", "text/plain", 200, false},
		{"too large", strings.Repeat("x", 65), "text/plain", 200, false},
		{"web page", "<html>error</html>", "text/html", 200, false},
		{"server error", "unavailable", "text/plain", 503, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			file := filepath.Join(t.TempDir(), "installer")
			err := downloadSetupFileWithClient(t.Context(), srv.Client(), srv.URL, file, 64)
			if !tc.ok {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			data, err := os.ReadFile(file)
			require.NoError(t, err)
			require.Equal(t, tc.body, string(data))
			stat, err := os.Stat(file)
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0600), stat.Mode().Perm())
			require.Error(t, downloadSetupFileWithClient(t.Context(), srv.Client(), srv.URL, file, 64), "never overwrite an existing file")
		})
	}
	require.Error(t, downloadSetupFileWithClient(t.Context(), http.DefaultClient, "http://example.com/install.sh", filepath.Join(t.TempDir(), "installer"), 64))
}

func TestInstallerRejectsUnsafeRedirects(t *testing.T) {
	t.Parallel()
	first, err := http.NewRequest(http.MethodGet, "https://pkgs.tailscale.com/stable/Tailscale-latest-macos.pkg", nil)
	require.NoError(t, err)
	for _, address := range []string{
		"http://pkgs.tailscale.com/file.pkg",
		"https://example.com/file.pkg",
		"https://pkgs.tailscale.com:8443/file.pkg",
	} {
		req, err := http.NewRequest(http.MethodGet, address, nil)
		require.NoError(t, err)
		require.Error(t, installerRedirect(req, []*http.Request{first}))
	}
	latest, err := http.NewRequest(http.MethodGet, "https://pkgs.tailscale.com/stable/Tailscale-1.2.3-macos.pkg", nil)
	require.NoError(t, err)
	require.NoError(t, installerRedirect(latest, []*http.Request{first}))
}

func TestInstallerVerificationBeforeExecution(t *testing.T) {
	t.Parallel()
	for _, platform := range []string{"linux", "darwin"} {
		for _, failure := range []string{"download", "verify", ""} {
			var commands []string
			run := func(name string, args ...string) error {
				commands = append(commands, name+" "+strings.Join(args, " "))
				if name == "/usr/sbin/spctl" && failure == "verify" {
					return errors.New("signature rejected")
				}
				return nil
			}
			download := func(_ context.Context, address, destination string, _ int64) error {
				if failure == "download" {
					return errors.New("interrupted download")
				}
				if platform == "darwin" {
					require.Equal(t, "https://pkgs.tailscale.com/stable/Tailscale-latest-macos.pkg", address)
				} else {
					require.Equal(t, "https://tailscale.com/install.sh", address)
				}
				return os.WriteFile(destination, []byte("test fixture"), 0600)
			}
			err := installTailscaleWithDownload(t.Context(), platform, io.Discard, run, download)
			if failure == "download" {
				require.Error(t, err)
				require.Empty(t, commands)
				continue
			}
			if platform == "darwin" {
				require.Contains(t, commands[0], "/usr/sbin/spctl --assess --type install")
				if failure == "verify" {
					require.Error(t, err)
					require.Len(t, commands, 1)
					continue
				}
				require.Len(t, commands, 3)
				require.Contains(t, commands[1], "/usr/sbin/installer -pkg")
				require.Equal(t, "/usr/bin/open -a Tailscale", commands[2])
			} else {
				require.Len(t, commands, 1)
				require.True(t, strings.HasPrefix(commands[0], "sh "))
			}
			require.NoError(t, err)
		}
	}
}
