//go:build linux && amd64

package filechange

import (
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestKeeperReleasesNotificationWhenWatcherExitsBeforeReply(t *testing.T) {
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	require.NoError(t, err)
	relay := pair[0]
	defer func() {
		if relay >= 0 {
			unix.Close(relay)
		}
	}()
	require.NoError(t, unix.SetsockoptTimeval(relay, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &unix.Timeval{Sec: 5}))
	type installed struct {
		listener int
		err      error
	}
	type callResult struct {
		pid   uintptr
		errno syscall.Errno
	}
	ready := make(chan installed, 1)
	results := make(chan [2]callResult, 1)
	go func() {
		runtime.LockOSThread() // The filtered thread exits with this goroutine.
		listener, err := installFilter([]unix.SockFilter{
			{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
			{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.SYS_GETPID, Jf: 1},
			{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_USER_NOTIF},
			{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
		})
		ready <- installed{listener, err}
		if err != nil {
			return
		}
		var calls [2]callResult
		for index := range calls {
			calls[index].pid, _, calls[index].errno = syscall.Syscall(unix.SYS_GETPID, 0, 0, 0)
		}
		results <- calls
	}()
	started := <-ready
	if started.err != nil {
		unix.Close(pair[1])
		t.Fatalf("Install notification filter: %v", started.err)
	}
	done := make(chan struct{})
	go func() {
		keepListener(started.listener, pair[1])
		close(done)
	}()
	var notification seccompNotification
	require.True(t, readPacket(relay, notificationBytes(&notification)))
	require.Equal(t, int32(unix.SYS_GETPID), notification.Data.Number)
	// Simulate Crush exiting after receiving a notification but before replying.
	require.NoError(t, unix.Close(relay))
	relay = -1
	select {
	case calls := <-results:
		for _, call := range calls {
			require.Zero(t, call.errno)
			require.Equal(t, uintptr(os.Getpid()), call.pid)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("The keeper did not release the interrupted review and subsequent call")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("The keeper did not exit after the filtered thread exited")
	}
}
