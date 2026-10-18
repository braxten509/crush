package server

import (
	"github.com/charmbracelet/crush/internal/apigen"
	"github.com/charmbracelet/crush/internal/pubsub"
)

// Piece C (tree actions and branch copy, engine-design.md 5.6 and 5.11)
// binds its routes, wraps its events and maps its errors here.

// treeEndpoints documents and routes piece C's endpoints.
func (c *controllerV1) treeEndpoints() []apigen.Endpoint { return nil }

// wrapTreeEvent wraps piece C's fan-in events for SSE. It reports false for
// an event that is not C's.
func wrapTreeEvent(any) (*pubsub.Payload, bool) { return nil, false }

// treeErrors maps piece C's errors to their statuses and codes.
func treeErrors() []apiError { return nil }
