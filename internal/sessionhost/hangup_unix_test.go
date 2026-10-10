//go:build !windows

package sessionhost

import (
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestQuitOnHangup(t *testing.T) {
	quits := make(chan struct{}, 2)
	QuitOnHangup(func() { quits <- struct{}{} })
	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGHUP))
	select {
	case <-quits:
	case <-time.After(5 * time.Second):
		t.Fatal("a hangup didn't quit")
	}
	// A second hangup, as the shell and the crash guard both send one,
	// must neither quit again nor end the process mid-shutdown.
	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGHUP))
	select {
	case <-quits:
		t.Fatal("quit ran twice")
	case <-time.After(200 * time.Millisecond):
	}
}
