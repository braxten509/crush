package prompthistory

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAddKeepsNewestHundredNewestFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompt-history.json")
	for i := range 105 {
		require.NoError(t, add(path, fmt.Sprintf("prompt %d", i)))
	}
	require.NoError(t, add(path, "   ")) // blank prompts are not kept

	got := list(path)
	require.Len(t, got, Limit)
	require.Equal(t, "prompt 104", got[0])
	require.Equal(t, "prompt 5", got[Limit-1])
}
