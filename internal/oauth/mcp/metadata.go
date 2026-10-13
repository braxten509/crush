package mcpoauth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// metadataFixupRoundTripper normalizes trailing-slash issuers in OAuth
// metadata responses. Some servers return an issuer with a trailing slash
// that doesn't match the URL the metadata was fetched from, causing the
// SDK's strict RFC 8414 validation to reject it. Based on Bruno Krugel's
// fix from PR #3396.
type metadataFixupRoundTripper struct {
	base http.RoundTripper
}

func newMetadataFixupRoundTripper(base http.RoundTripper) *metadataFixupRoundTripper {
	return &metadataFixupRoundTripper{base: base}
}

func (rt *metadataFixupRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := rt.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if !isMetadataEndpoint(req.URL.Path) || resp.StatusCode != http.StatusOK || resp.Body == nil {
		return resp, nil
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read metadata response: %w", err)
	}

	var raw map[string]any
	if json.Unmarshal(body, &raw) != nil {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return resp, nil
	}

	issuer, ok := raw["issuer"].(string)
	if !ok || !strings.HasSuffix(issuer, "/") {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return resp, nil
	}

	raw["issuer"] = strings.TrimSuffix(issuer, "/")
	fixed, err := json.Marshal(raw)
	if err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return resp, nil
	}

	slog.Debug("Normalized OAuth metadata issuer trailing slash", "url", req.URL.String())
	resp.Body = io.NopCloser(bytes.NewReader(fixed))
	resp.ContentLength = int64(len(fixed))
	resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(fixed)))
	return resp, nil
}

// newOAuthMetadataClient creates an HTTP client for the OAuth flow that
// smooths over two nonstandard behaviors seen behind corporate proxies:
//
//  1. Trailing-slash issuers in metadata responses, normalized by
//     metadataFixupRoundTripper so they pass the SDK's strict RFC 8414
//     validation.
//  2. Metadata discovery requests that get 3xx-redirected to an
//     unreachable internal host (e.g. a cluster address behind a proxy).
//     Well-known discovery is never supposed to hop hosts via redirects
//     (the authorization server location comes from the metadata body,
//     not a Location header), so for metadata endpoints we rewrite the
//     redirect back to the original MCP host. Token, registration, and
//     authorize requests are left untouched, so a separately hosted
//     identity provider keeps working.
func newOAuthMetadataClient(base http.RoundTripper, serverURL string) *http.Client {
	var originalHost, originalScheme string
	if u, err := url.Parse(serverURL); err == nil {
		originalHost = u.Host
		originalScheme = u.Scheme
	}
	return &http.Client{
		Transport: newMetadataFixupRoundTripper(base),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Supplying CheckRedirect replaces net/http's default, so
			// re-enforce its 10-redirect cap here.
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			if originalHost != "" && isMetadataEndpoint(req.URL.Path) && req.URL.Host != originalHost {
				slog.Debug("Rewriting OAuth metadata redirect back to original host",
					"from", req.URL.Host, "to", originalHost)
				req.URL.Host = originalHost
				req.URL.Scheme = originalScheme
				req.Host = originalHost
			}
			return nil
		},
	}
}

func isMetadataEndpoint(path string) bool {
	return strings.Contains(path, "/.well-known/oauth-authorization-server") ||
		strings.Contains(path, "/.well-known/oauth-protected-resource")
}

// stripResourceParam removes the "resource" query parameter from an
// authorization URL. Some authorization servers reject it in the
// authorize request but accept it during token exchange. Based on Bruno
// Krugel's fix from PR #3396.
func stripResourceParam(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	if q.Has("resource") {
		q.Del("resource")
		u.RawQuery = q.Encode()
		slog.Debug("Stripped resource parameter from authorization URL")
	}
	return u.String()
}
