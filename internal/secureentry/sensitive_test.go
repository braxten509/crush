package secureentry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSensitiveDestinationRegisteredBeforeSaving(t *testing.T) {
	t.Parallel()
	path := template(t, "KEY=%s")
	called := false
	require.True(t, ReviewFile(path, func() { called = true }))
	require.True(t, called)
	target, err := Prepare(Spec{File: path, Occurrence: 1})
	require.NoError(t, err)
	called = false
	require.True(t, Sensitive(path))
	require.False(t, ReviewFile(path, func() { called = true }))
	require.False(t, called)
	require.NoError(t, target.Save([]byte("synthetic-secret")))
	target.Close()
	require.True(t, Sensitive(path), "closing the dialog must not re-enable snapshots")
	alias := filepath.Join(t.TempDir(), "alias")
	require.NoError(t, os.Symlink(filepath.Dir(path), alias))
	require.True(t, Sensitive(filepath.Join(alias, filepath.Base(path))))
}
