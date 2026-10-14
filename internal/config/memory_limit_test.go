package config

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemoryLimitBytes(t *testing.T) {
	t.Parallel()
	const ram = 32 << 30
	require.Equal(t, uint64(16<<30), (*Options)(nil).MemoryLimitBytes(ram), "half the RAM until saved")
	require.Equal(t, uint64(16<<30), (&Options{}).MemoryLimitBytes(ram))

	gb := 12.0
	require.Equal(t, uint64(12<<30), (&Options{MemoryLimitGB: &gb}).MemoryLimitBytes(ram))
	none := 0.0
	require.Zero(t, (&Options{MemoryLimitGB: &none}).MemoryLimitBytes(ram), "0 is no limit")
}

func TestValidateMemoryLimitGB(t *testing.T) {
	t.Parallel()
	require.NoError(t, ValidateMemoryLimitGB(0))
	require.NoError(t, ValidateMemoryLimitGB(2))
	require.NoError(t, ValidateMemoryLimitGB(16.5))
	require.Error(t, ValidateMemoryLimitGB(1))
	require.Error(t, ValidateMemoryLimitGB(-4))
	require.Error(t, ValidateMemoryLimitGB(math.NaN()))
}
