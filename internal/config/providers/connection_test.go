package providers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"charm.land/catwalk/v3/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config/resolve"
	"github.com/stretchr/testify/require"
)

func TestTestConnection(t *testing.T) {
	t.Parallel()
	identity := resolve.IdentityResolver()

	t.Run("openai-compatible endpoint lists models with a bearer key", func(t *testing.T) {
		t.Parallel()
		var gotPath, gotAuth, gotExtra string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath, gotAuth, gotExtra = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-Extra")
		}))
		defer srv.Close()

		err := TestConnection(Endpoint{
			ID:           "custom",
			Type:         catwalk.TypeCompletions,
			APIKey:       "sk-test",
			BaseURL:      srv.URL + "/v1",
			ExtraHeaders: map[string]string{"X-Extra": "yes"},
		}, identity)
		require.NoError(t, err)
		require.Equal(t, "/v1/models", gotPath)
		require.Equal(t, "Bearer sk-test", gotAuth)
		require.Equal(t, "yes", gotExtra)
	})

	t.Run("rejected key is an error", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()

		err := TestConnection(Endpoint{ID: "custom", Type: catwalk.TypeMessages, APIKey: "k", BaseURL: srv.URL}, identity)
		require.ErrorContains(t, err, "failed to connect to provider custom")
	})

	t.Run("providers without a test endpoint are checked offline", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, TestConnection(Endpoint{ID: string(catwalk.InferenceProviderMiniMax)}, identity))
		// A custom entry still typed catwalk v2's "bedrock" keeps the
		// offline Bedrock key check.
		legacyBedrock := catwalk.Type("bedrock")
		require.NoError(t, TestConnection(Endpoint{ID: "b", Type: legacyBedrock, APIKey: "ABSK123"}, identity))
		require.EqualError(t, TestConnection(Endpoint{ID: "b", Type: legacyBedrock, APIKey: "nope"}, identity), "not a valid bedrock api key")
		require.EqualError(t,
			TestConnection(Endpoint{ID: string(catwalk.InferenceProviderAlibabaSingapore), APIKey: "nope"}, identity),
			"invalid API key format for provider "+string(catwalk.InferenceProviderAlibabaSingapore))
	})

	// Since catwalk v3, provider types without an SDK of their own (custom
	// types such as ollama or litellm, or ones Crush does not know) are
	// probed on an OpenAI-style /models endpoint.
	t.Run("unknown provider types list models OpenAI-style", func(t *testing.T) {
		t.Parallel()
		var gotPath, gotAuth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		}))
		defer srv.Close()

		err := TestConnection(Endpoint{ID: "x", Type: "unknown", APIKey: "k", BaseURL: srv.URL + "/v1"}, identity)
		require.NoError(t, err)
		require.Equal(t, "/v1/models", gotPath)
		require.Equal(t, "Bearer k", gotAuth)
	})

	t.Run("an unreachable endpoint fails the request", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		srv.Close()

		err := TestConnection(Endpoint{ID: "x", Type: "unknown", BaseURL: srv.URL}, identity)
		require.ErrorContains(t, err, "failed to create request for provider x")
	})
}
