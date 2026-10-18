package server

import (
	"github.com/charmbracelet/crush/internal/apigen"
	"github.com/charmbracelet/crush/internal/pubsub"
)

// Piece A (engine lifecycle and wire, engine-design.md 5.1, 5.2, 5.10 and
// 5.14) binds its routes, wraps its events and maps its errors here.

// lifecycleEndpoints documents and routes piece A's endpoints.
func (c *controllerV1) lifecycleEndpoints() []apigen.Endpoint { return nil }

// wrapLifecycleEvent wraps piece A's fan-in events for SSE. It reports
// false for an event that is not A's.
func wrapLifecycleEvent(any) (*pubsub.Payload, bool) { return nil, false }

// lifecycleErrors maps piece A's errors to their statuses and codes.
func lifecycleErrors() []apiError { return nil }
