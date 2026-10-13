package serverctl

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/charmbracelet/crush/internal/client"
)

// quickHealthProbe issues a single readiness request with the caller's
// context and returns nil iff the server is responsive right now.
func quickHealthProbe(ctx context.Context, hostURL *url.URL) error {
	httpClient, reqURL, err := readinessHTTPClient(hostURL)
	if err != nil {
		return err
	}
	return probeHealth(ctx, httpClient, reqURL, hostURL)
}

// serverReadyTimeout returns the total budget for the readiness probe.
// Overridable via CRUSH_SERVER_READY_TIMEOUT (parsed as a Go duration).
func serverReadyTimeout() time.Duration {
	const def = 10 * time.Second
	v := os.Getenv("CRUSH_SERVER_READY_TIMEOUT")
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return def
	}
	return d
}

// waitForServerReady polls GET /v1/health until the server responds with
// any 2xx status or the total timeout elapses. Each attempt uses a short
// per-attempt timeout so a hung listener doesn't burn the whole budget.
//
// The HTTP transport is built to mirror how *client.Client dials so the
// same unix socket / npipe / tcp setups all work uniformly here.
func waitForServerReady(ctx context.Context, hostURL *url.URL) error {
	httpClient, reqURL, err := readinessHTTPClient(hostURL)
	if err != nil {
		return err
	}

	const perAttempt = 100 * time.Millisecond
	deadline := time.Now().Add(serverReadyTimeout())

	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return lastErr
			}
			return fmt.Errorf("timed out waiting for server readiness")
		}

		attemptCtx, cancel := context.WithTimeout(ctx, perAttempt)
		err := probeHealth(attemptCtx, httpClient, reqURL, hostURL)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(perAttempt):
		}
	}
}

// readinessHTTPClient builds an *http.Client whose transport dials the
// server using the same scheme-aware logic as *client.Client (unix
// socket, named pipe, or tcp).
func readinessHTTPClient(hostURL *url.URL) (*http.Client, string, error) {
	c, err := client.NewClient("", hostURL.Scheme, hostURL.Host)
	if err != nil {
		return nil, "", err
	}

	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return c.Dial(ctx, network, addr)
	}
	if hostURL.Scheme == "unix" || hostURL.Scheme == "npipe" {
		tr.DisableCompression = true
	}

	httpClient := &http.Client{Transport: tr}

	// For unix sockets / named pipes we still need a syntactically valid
	// HTTP URL; the actual address is resolved by the dialer.
	host := hostURL.Host
	if hostURL.Scheme == "unix" || hostURL.Scheme == "npipe" {
		host = client.DummyHost
	}
	reqURL := (&url.URL{Scheme: "http", Host: host, Path: "/v1/health"}).String()
	return httpClient, reqURL, nil
}

// probeHealth issues a single GET to the readiness endpoint and treats
// any 2xx response as success.
func probeHealth(ctx context.Context, h *http.Client, reqURL string, hostURL *url.URL) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	if hostURL.Scheme == "unix" || hostURL.Scheme == "npipe" {
		req.Host = client.DummyHost
	}
	rsp, err := h.Do(req)
	if err != nil {
		return err
	}
	defer rsp.Body.Close()
	_, _ = io.Copy(io.Discard, rsp.Body)
	if rsp.StatusCode < 200 || rsp.StatusCode >= 300 {
		return fmt.Errorf("server health check failed: %s", rsp.Status)
	}
	return nil
}
