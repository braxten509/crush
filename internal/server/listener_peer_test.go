//go:build !windows

package server

import (
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPeerCheckRefusesOtherUID(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "p.sock")
	raw, err := net.Listen("unix", path) //nolint:noctx
	require.NoError(t, err)
	ln := peerCheckListener{Listener: raw, uid: os.Getuid()}

	// The first peer is another user, the second cannot be identified, the
	// third is us.
	var calls atomic.Int32
	old := peerUID
	peerUID = func(*net.UnixConn) (int, error) {
		switch calls.Add(1) {
		case 1:
			return os.Getuid() + 1, nil
		case 2:
			return -1, errors.New("no credentials")
		default:
			return os.Getuid(), nil
		}
	}

	type accepted struct {
		conn net.Conn
		err  error
	}
	results := make(chan accepted, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		results <- accepted{conn, err}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
		peerUID = old
	})

	for _, peer := range []string{"another user", "an unknown peer"} {
		conn, err := net.Dial("unix", path) //nolint:noctx
		require.NoError(t, err)
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
		_, err = conn.Read(make([]byte, 1))
		_ = conn.Close()
		require.ErrorIs(t, err, io.EOF, "the connection from %s is closed", peer)
		select {
		case r := <-results:
			t.Fatalf("Accept returned for %s: %v, %v", peer, r.conn, r.err)
		default:
		}
	}

	// The listener keeps serving: our own connection gets through.
	conn, err := net.Dial("unix", path) //nolint:noctx
	require.NoError(t, err)
	defer conn.Close()
	var r accepted
	select {
	case r = <-results:
	case <-time.After(5 * time.Second):
		t.Fatal("the listener stopped serving after refusing peers")
	}
	require.NoError(t, r.err)
	defer r.conn.Close()
	_, err = r.conn.Write([]byte("k"))
	require.NoError(t, err)
	buf := make([]byte, 1)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, err = io.ReadFull(conn, buf)
	require.NoError(t, err)
	require.Equal(t, "k", string(buf))
	require.EqualValues(t, 3, calls.Load())
}

func TestPeerUIDIsOwnUID(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("the same-user check exists only on Linux and Mac")
	}
	t.Parallel()

	path := filepath.Join(shortTempDir(t), "u.sock")
	ln, err := net.Listen("unix", path) //nolint:noctx
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	clientSide, err := net.Dial("unix", path) //nolint:noctx
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientSide.Close() })
	serverSide, err := ln.Accept()
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSide.Close() })

	for name, conn := range map[string]net.Conn{"server side": serverSide, "client side": clientSide} {
		uid, err := socketPeerUID(conn.(*net.UnixConn))
		require.NoError(t, err, name)
		require.Equal(t, os.Getuid(), uid, name)
	}
}
