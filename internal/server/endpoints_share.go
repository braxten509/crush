package server

import (
	"github.com/charmbracelet/crush/internal/apigen"
	"github.com/charmbracelet/crush/internal/pubsub"
)

// Piece D (phone sharing and secure entry, engine-design.md 5.7 and 5.8)
// binds its routes, wraps its events and maps its errors here.

// shareEndpoints documents and routes piece D's endpoints.
func (c *controllerV1) shareEndpoints() []apigen.Endpoint { return nil }

// wrapShareEvent wraps piece D's fan-in events for SSE. It reports false
// for an event that is not D's.
func wrapShareEvent(any) (*pubsub.Payload, bool) { return nil, false }

// shareErrors maps piece D's errors to their statuses and codes.
func shareErrors() []apiError { return nil }
