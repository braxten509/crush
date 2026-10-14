package dialog

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseMemoryLimit(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]float64{"": 0, "  ": 0, "16": 16, "12.5": 12.5, "16 GB": 16, "8gb": 8} {
		got, err := parseMemoryLimit(in)
		require.NoError(t, err, in)
		require.Equal(t, want, got, in)
	}
	for _, in := range []string{"lots", "1", "-3"} {
		_, err := parseMemoryLimit(in)
		require.Error(t, err, in)
	}
}

func TestFormatGB(t *testing.T) {
	t.Parallel()
	require.Equal(t, "16", formatGB(16<<30))
	require.Equal(t, "9.4", formatGB(94*(1<<30)/10))
}
