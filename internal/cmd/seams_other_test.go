//go:build !crushtest

package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/memwatch"
	"github.com/stretchr/testify/require"
)

// TestSeamsIgnoredWithoutTag: a build without the crushtest tag ignores
// CRUSH_TEST_SEAMS at start; nothing it names is installed (9.1).
func TestSeamsIgnoredWithoutTag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seams.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
		"memory":{"percent":99,"total_ram_bytes":4242},
		"limits":{"claude-cli":"not signed in"}}`), 0o600))
	t.Setenv("CRUSH_TEST_SEAMS", path)

	installTestSeams()

	require.NotEqual(t, uint64(4242), memwatch.TotalRAM(), "memory is read from this computer")
	require.False(t, cliagent.LimitFetchersReplaced(), "the usage fetchers are the real ones")
}
