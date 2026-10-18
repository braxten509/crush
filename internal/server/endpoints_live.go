package server

import (
	"github.com/charmbracelet/crush/internal/apigen"
	"github.com/charmbracelet/crush/internal/pubsub"
)

// Piece B (live work, ownership and the smaller gaps, engine-design.md 5.3,
// 5.4, 5.5 and 5.9) binds its routes, wraps its events and maps its errors
// here.

// liveEndpoints documents and routes piece B's endpoints.
func (c *controllerV1) liveEndpoints() []apigen.Endpoint { return nil }

// wrapLiveEvent wraps piece B's fan-in events for SSE. It reports false for
// an event that is not B's.
func wrapLiveEvent(any) (*pubsub.Payload, bool) { return nil, false }

// liveErrors maps piece B's errors to their statuses and codes.
func liveErrors() []apiError { return nil }
