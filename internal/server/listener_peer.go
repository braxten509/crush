package server

import (
	"log/slog"
	"net"
)

// peerCheckListener wraps a unix socket listener and refuses connections
// from any user but uid (engine-design.md 5.1 d). It is installed only on
// Linux and Mac.
type peerCheckListener struct {
	net.Listener
	uid int
}

// Accept loops: it accepts, checks the peer, closes refused peers and keeps
// accepting. It never returns an error for a refused peer.
func (l peerCheckListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		unixConn, ok := conn.(*net.UnixConn)
		if !ok {
			slog.Warn("Refused a connection: peer unknown", "error", "not a unix socket connection")
			_ = conn.Close()
			continue
		}
		uid, err := peerUID(unixConn)
		if err != nil {
			slog.Warn("Refused a connection: peer unknown", "error", err)
			_ = conn.Close()
			continue
		}
		if uid != l.uid {
			slog.Warn("Refused a connection from another user", "uid", uid)
			_ = conn.Close()
			continue
		}
		return conn, nil
	}
}

// peerUID reports the user on the other end of c. The default is
// build-tagged (peercred_*.go); it is a var so tests can inject one.
var peerUID = socketPeerUID
