package server

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/stretchr/testify/require"
)

func TestLifecycleErrorCodes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{backend.ErrProtocolMismatch, http.StatusUpgradeRequired, "protocol_mismatch"},
		{backend.ErrWrongProject, http.StatusConflict, "wrong_project"},
		{backend.ErrChannelOptInMismatch, http.StatusConflict, "channel_mismatch"},
		{db.ErrDataDirLocked, http.StatusLocked, "project_locked"},
	} {
		status, code, field := errorInfo(fmt.Errorf("wrapped: %w", tc.err))
		require.Equal(t, tc.status, status, tc.code)
		require.Equal(t, tc.code, code)
		require.Empty(t, field)
	}
}
