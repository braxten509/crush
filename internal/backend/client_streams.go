package backend

import (
	"context"
	"log/slog"

	"github.com/charmbracelet/crush/internal/pubsub"
)

// EngineEvent is an event the engine sends to app streams itself, with
// [Backend.SendTo] or [Backend.SendAll] (engine-design.md, section 0.1). It
// never enters the app event fan-in, so the phone never sees it.
type EngineEvent struct {
	Type    pubsub.PayloadType
	Kind    pubsub.EventType // created, updated or deleted
	Payload any
}

// clientStreamCapacity is how many engine events one stream holds. A full
// stream drops new ones.
const clientStreamCapacity = 256

// clientStream is one SSE stream of one app that takes engine events.
type clientStream struct {
	clientID string
	events   chan any
}

// SubscribeClient registers one SSE stream of clientID for engine-sent events.
// The channel closes when ctx ends.
func (b *Backend) SubscribeClient(ctx context.Context, workspaceID, clientID string) (<-chan any, error) {
	if _, err := validateClientID(clientID); err != nil {
		return nil, err
	}
	ws, ok := b.workspaces.Get(workspaceID)
	if !ok {
		return nil, ErrWorkspaceNotFound
	}
	s := &clientStream{clientID: clientID, events: make(chan any, clientStreamCapacity)}
	ws.streamMu.Lock()
	if ws.streams == nil {
		ws.streams = make(map[*clientStream]struct{})
	}
	ws.streams[s] = struct{}{}
	ws.streamMu.Unlock()
	go func() {
		<-ctx.Done()
		// Senders hold streamMu while they queue, so the channel never
		// closes under one of them.
		ws.streamMu.Lock()
		delete(ws.streams, s)
		close(s.events)
		ws.streamMu.Unlock()
	}()
	return s.events, nil
}

// SendTo queues ev on every live stream of clientID and returns how many took it.
func (b *Backend) SendTo(workspaceID, clientID string, ev EngineEvent) int {
	return b.sendEngineEvent(workspaceID, ev, func(s *clientStream) bool { return s.clientID == clientID })
}

// SendAll queues ev on every live stream of the workspace.
func (b *Backend) SendAll(workspaceID string, ev EngineEvent) int {
	return b.sendEngineEvent(workspaceID, ev, func(*clientStream) bool { return true })
}

// sendEngineEvent queues ev on every live stream of the workspace that to
// picks, without blocking, and returns how many took it.
func (b *Backend) sendEngineEvent(workspaceID string, ev EngineEvent, to func(*clientStream) bool) int {
	ws, ok := b.workspaces.Get(workspaceID)
	if !ok {
		return 0
	}
	ws.streamMu.Lock()
	defer ws.streamMu.Unlock()
	taken := 0
	for s := range ws.streams {
		if !to(s) {
			continue
		}
		select {
		case s.events <- ev:
			taken++
		default:
			// Log the type only: a payload can carry anything.
			slog.Warn("Dropped an engine event: the stream is full", "type", ev.Type)
		}
	}
	return taken
}
