package sandbox

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithin(t *testing.T) {
	t.Parallel()
	require.True(t, Within("/a/b", "/a"))
	require.True(t, Within("/a", "/a"))
	require.False(t, Within("/ab", "/a"))
	require.False(t, Within("/", "/a"))
}
