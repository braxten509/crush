package resolve

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// mapResolver resolves values by table lookup; unknown values pass
// through and "fail" errors.
type mapResolver map[string]string

func (m mapResolver) ResolveValue(v string) (string, error) {
	if v == "fail" {
		return "", errors.New("boom")
	}
	if out, ok := m[v]; ok {
		return out, nil
	}
	return v, nil
}

func TestValueHelpers(t *testing.T) {
	t.Parallel()
	r := mapResolver{"$A": "a", "$EMPTY": ""}

	t.Run("args", func(t *testing.T) {
		t.Parallel()
		got, err := Args([]string{"$A", "$EMPTY", "x"}, r)
		require.NoError(t, err)
		require.Equal(t, []string{"a", "", "x"}, got)

		got, err = Args(nil, r)
		require.NoError(t, err)
		require.Nil(t, got)

		_, err = Args([]string{"ok", "fail"}, r)
		require.EqualError(t, err, "arg 1: boom")
	})

	t.Run("env list", func(t *testing.T) {
		t.Parallel()
		got, err := EnvList(map[string]string{"B": "$A", "A": "$EMPTY"}, r)
		require.NoError(t, err)
		require.Equal(t, []string{"A=", "B=a"}, got)

		got, err = EnvList(nil, r)
		require.NoError(t, err)
		require.Nil(t, got)

		_, err = EnvList(map[string]string{"K": "fail"}, r)
		require.EqualError(t, err, "env K: boom")
	})

	t.Run("env map", func(t *testing.T) {
		t.Parallel()
		got, err := EnvMap(map[string]string{"B": "$A", "A": "$EMPTY"}, r)
		require.NoError(t, err)
		require.Equal(t, map[string]string{"A": "", "B": "a"}, got)

		got, err = EnvMap(nil, r)
		require.NoError(t, err)
		require.Equal(t, map[string]string{}, got)

		_, err = EnvMap(map[string]string{"K": "fail"}, r)
		require.EqualError(t, err, `env "K": boom`)
	})

	t.Run("headers", func(t *testing.T) {
		t.Parallel()
		got, err := Headers(map[string]string{"X-A": "$A", "X-Empty": "$EMPTY"}, r)
		require.NoError(t, err)
		require.Equal(t, map[string]string{"X-A": "a"}, got)

		got, err = Headers(nil, r)
		require.NoError(t, err)
		require.Equal(t, map[string]string{}, got)

		_, err = Headers(map[string]string{"B": "fail", "A": "fail"}, r)
		require.EqualError(t, err, "header A: boom", "the first failure in sorted order is reported")
	})
}
