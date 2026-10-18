package remote

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRemoteLaunchIDPerShare: each share of one process reports its own
// launch id, and StartContext still reports the window's (LaunchID).
//
// Not parallel: it replaces discoverTailnet and the process-wide launch id.
func TestRemoteLaunchIDPerShare(t *testing.T) {
	oldDiscover, oldLaunch := discoverTailnet, LaunchID()
	t.Cleanup(func() {
		discoverTailnet = oldDiscover
		SetLaunchID(oldLaunch)
	})
	discoverTailnet = func(context.Context) (self, func(context.Context, string) (peer, error), func() (net.Listener, error), error) {
		whois := func(context.Context, string) (peer, error) { return peer{User: ownerUser, Name: "pixel"}, nil }
		listen := func() (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") }
		return self{Host: "cachy", User: ownerUser, Online: true}, whois, listen, nil
	}
	SetLaunchID("window-launch")

	src := newFakeSource()
	running := func(s *Server, err error) *Server {
		t.Helper()
		require.NoError(t, err)
		t.Cleanup(s.Stop)
		return s
	}
	a := running(StartShare(t.Context(), src, "launch-a"))
	b := running(StartShare(t.Context(), src, "launch-b"))
	w := running(StartContext(t.Context(), src))

	// What the phone reads from each share's own route.
	launchOf := func(s *Server) string {
		t.Helper()
		ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			r.RemoteAddr = net.JoinHostPort(phoneAddr, "40000")
			s.http.Handler.ServeHTTP(rw, r)
		}))
		defer ts.Close()
		resp, err := http.Get(ts.URL + "/v1/share")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var share Share
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&share))
		return share.Launch
	}
	require.Equal(t, "launch-a", launchOf(a))
	require.Equal(t, "launch-b", launchOf(b))
	require.Equal(t, "window-launch", launchOf(w), "StartContext reports the process-wide launch id")

	// A share keeps the id it started with.
	SetLaunchID("changed-later")
	require.Equal(t, "launch-a", launchOf(a))
}
