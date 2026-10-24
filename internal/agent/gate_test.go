package agent

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// gate is a command that keeps running until the test opens it, without
// sleeping (owner, 2026-10-23: test waits are 0.2-0.5 s or less): `cat`
// blocks reading a named pipe whose write end the test holds, and ends the
// moment the test closes it. Cleanup closes it, and so does the test
// process dying, so a failing test never leaves it running.
func gate(t *testing.T) (command string, open func()) {
	t.Helper()
	fifo := filepath.Join(t.TempDir(), "gate")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))
	// Read-write never blocks on a pipe, and holding a writer keeps cat
	// reading however late it starts.
	writer, err := os.OpenFile(fifo, os.O_RDWR, 0)
	require.NoError(t, err)
	var once sync.Once
	open = func() { once.Do(func() { _ = writer.Close() }) }
	t.Cleanup(open)
	return "cat '" + fifo + "'", open
}
