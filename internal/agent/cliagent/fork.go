package cliagent

import (
	"context"
	"errors"
)

// ForkPoint is where a branch cuts an AI tool's own memory: the tool's
// session and its marker for the last copied message.
type ForkPoint struct{ Native, Marker, MarkerKind string }

// Forker is implemented per tool in phase 4. Until then every tool takes
// the written-copy handoff.
type Forker interface {
	// Fork makes a new native session from p, or returns ErrNoFork to use the written copy.
	Fork(ctx context.Context, p ForkPoint) (native string, err error)
}

// ErrNoFork means the tool can't fork its memory at the point given.
var ErrNoFork = errors.New("fork not available")
