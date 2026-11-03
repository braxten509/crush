package server

import (
	"net/http"
	"testing"

	"github.com/charmbracelet/crush/internal/localmodel"
	"github.com/stretchr/testify/require"
)

func TestLocalModelRoutesAreLocalOnly(t *testing.T) {
	t.Setenv("CRUSH_LOCAL_MODEL_DIRS", t.TempDir())
	g := newG56Workspace(t)
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/v1/local-models"},
		{http.MethodPost, "/v1/local-models/load"},
		{http.MethodPost, "/v1/local-models/unload"},
	} {
		liveError(t, g.h, route.method, route.path, map[string]string{"model_id": "unknown"}, http.StatusForbidden, "local_only", "")
	}
	u := newUnixCaller(t, g)
	status := unixJSON[localmodel.Status](t, u, http.MethodGet, "/v1/local-models", nil, http.StatusOK)
	require.Empty(t, status.Models)
	code, _ := u.call(t, http.MethodPost, "/v1/local-models/load", map[string]string{"model_id": "/arbitrary/file"})
	require.Equal(t, http.StatusBadRequest, code)
}
