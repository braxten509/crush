package skills

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeBuiltinSkillsAreReadableAndStable(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, skill := range DiscoverBuiltin() {
		original := skill.SkillFilePath
		native, err := NativeLocation(skill)
		require.NoError(t, err)
		require.Equal(t, original, skill.SkillFilePath)
		expected, err := builtinFS.ReadFile("builtin/" + strings.TrimPrefix(original, BuiltinPrefix))
		require.NoError(t, err)
		actual, err := os.ReadFile(native.SkillFilePath)
		require.NoError(t, err)
		require.Equal(t, expected, actual)
		again, err := NativeLocation(skill)
		require.NoError(t, err)
		require.Equal(t, native.SkillFilePath, again.SkillFilePath)
		require.NoError(t, os.Remove(native.SkillFilePath))
		_, err = NativeLocation(skill)
		require.NoError(t, err)
		restored, err := os.ReadFile(native.SkillFilePath)
		require.NoError(t, err)
		require.Equal(t, expected, restored)
	}
}
