package client

import (
	"errors"
	"io/fs"
	"net"
	"syscall"
)

// IsUnreachable reports a dead engine: a dial error with ECONNREFUSED or
// ENOENT (the socket file is gone, or no process listens on it, as
// server.IsStaleSocketErr classifies it), or a 503 shutting_down answer
// from an engine that is exiting. A timeout, a 404 and every other answer
// are not: the engine is alive.
//
// The stale-socket check is repeated here rather than imported, so this
// package does not depend on the server.
func IsUnreachable(err error) bool {
	if err == nil {
		return false
	}
	// Every 503 the engine sends is shutting_down; checkStatus keeps the
	// status as this sentinel.
	if errors.Is(err, ErrServerShuttingDown) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return false
	}
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, fs.ErrNotExist)
}
