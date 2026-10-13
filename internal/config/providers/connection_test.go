package providers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
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
			Type:         catwalk.TypeOpenAICompat,
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

		err := TestConnection(Endpoint{ID: "custom", Type: catwalk.TypeAnthropic, APIKey: "k", BaseURL: srv.URL}, identity)
		require.ErrorContains(t, err, "failed to connect to provider custom")
	})

	t.Run("providers without a test endpoint are checked offline", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, TestConnection(Endpoint{ID: string(catwalk.InferenceProviderMiniMax)}, identity))
		require.NoError(t, TestConnection(Endpoint{ID: "b", Type: catwalk.TypeBedrock, APIKey: "ABSK123"}, identity))
		require.EqualError(t, TestConnection(Endpoint{ID: "b", Type: catwalk.TypeBedrock, APIKey: "nope"}, identity), "not a valid bedrock api key")
		require.EqualError(t,
			TestConnection(Endpoint{ID: string(catwalk.InferenceProviderAlibabaSingapore), APIKey: "nope"}, identity),
			"invalid API key format for provider "+string(catwalk.InferenceProviderAlibabaSingapore))
	})

	t.Run("unknown provider types fail the request", func(t *testing.T) {
		t.Parallel()
		err := TestConnection(Endpoint{ID: "x", Type: "unknown"}, identity)
		require.ErrorContains(t, err, "failed to create request for provider x")
	})
}
