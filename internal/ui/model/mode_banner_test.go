package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSysadminBannerSaysWhatItCosts guards the wording. The badge alone
// names the mode; the message is the only place that says what entering it
// actually gives away.
func TestSysadminBannerSaysWhatItCosts(t *testing.T) {
	t.Parallel()
	require.Contains(t, sysadminModeBannerMsg, "unblocked")
}
