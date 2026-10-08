//go:build linux && amd64

package filechange

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestKeeperReleasesNotificationWhenWatcherExitsBeforeReply(t *testing.T) {
	if os.Getenv("CRUSH_KEEPER_PENDING_CALL_TEST") == "1" {
		pid := os.Getpid()
		runtime.LockOSThread() // The filtered thread exits with this test.
		listener, err := installFilter([]unix.SockFilter{
			{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
			{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.SYS_GETPID, Jf: 1},
			{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_USER_NOTIF},
			{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
		})
		require.NoError(t, err)
		require.NoError(t, unix.Sendmsg(3, []byte{0}, unix.UnixRights(listener), nil, 0))
		unix.Close(listener)
		for range 2 {
			actual, _, errno := syscall.Syscall(unix.SYS_GETPID, 0, 0, 0)
			require.Zero(t, errno)
			require.Equal(t, uintptr(pid), actual)
		}
		return
	}

	control, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	require.NoError(t, err)
	defer unix.Close(control[0])
	childControl := os.NewFile(uintptr(control[1]), "keeper-test")
	defer childControl.Close()
	require.NoError(t, unix.SetsockoptTimeval(control[0], unix.SOL_SOCKET, unix.SO_RCVTIMEO, &unix.Timeval{Sec: 5}))
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestKeeperReleasesNotificationWhenWatcherExitsBeforeReply$", "-test.count=1")
	child.Env = append(os.Environ(), "CRUSH_KEEPER_PENDING_CALL_TEST=1")
	child.ExtraFiles = []*os.File{childControl}
	var output bytes.Buffer
	child.Stdout, child.Stderr = &output, &output
	require.NoError(t, child.Start())
	defer func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	}()
	require.NoError(t, childControl.Close())
	message, rights := make([]byte, 1), make([]byte, unix.CmsgSpace(4))
	_, n, _, _, err := unix.Recvmsg(control[0], message, rights, unix.MSG_CMSG_CLOEXEC)
	require.NoError(t, err)
	messages, err := unix.ParseSocketControlMessage(rights[:n])
	require.NoError(t, err)
	require.Len(t, messages, 1)
	fds, err := unix.ParseUnixRights(&messages[0])
	require.NoError(t, err)
	require.Len(t, fds, 1)

	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	require.NoError(t, err)
	relay := pair[0]
	defer func() {
		if relay >= 0 {
			unix.Close(relay)
		}
	}()
	require.NoError(t, unix.SetsockoptTimeval(relay, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &unix.Timeval{Sec: 5}))
	done := make(chan struct{})
	go func() {
		keepListener(fds[0], pair[1])
		close(done)
	}()
	var notification seccompNotification
	require.True(t, readPacket(relay, notificationBytes(&notification)))
	require.Equal(t, int32(unix.SYS_GETPID), notification.Data.Number)
	// Simulate Crush exiting after receiving a notification but before replying.
	require.NoError(t, unix.Close(relay))
	relay = -1
	require.NoError(t, child.Wait(), output.String())
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("The keeper did not exit after the filtered process exited")
	}
}
