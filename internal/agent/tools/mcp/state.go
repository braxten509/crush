package mcp

import (
	"context"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/pubsub"
)

// State represents the current state of an MCP client
type State int

const (
	StateDisabled State = iota
	StateStarting
	StateConnected
	StateError
	StateNeedsAuth
)

func (s State) String() string {
	switch s {
	case StateDisabled:
		return "disabled"
	case StateStarting:
		return "starting"
	case StateConnected:
		return "connected"
	case StateError:
		return "error"
	case StateNeedsAuth:
		return "needs auth"
	default:
		return "unknown"
	}
}

// EventType represents the type of MCP event
type EventType uint

const (
	EventStateChanged EventType = iota
	EventToolsListChanged
	EventPromptsListChanged
	EventResourcesListChanged
	// EventChannelMessage is published when a channel server pushes a
	// notifications/claude/channel event. ChannelMessage carries the rendered,
	// escaped <channel> element ready for injection into the session.
	EventChannelMessage
)

// Event represents an event in the MCP system
type Event struct {
	Type   EventType
	Name   string
	State  State
	Error  error
	Counts Counts
	// ChannelMessage is set only for EventChannelMessage: the fully rendered
	// and escaped <channel>...</channel> element to inject into the session.
	ChannelMessage string
}

// Counts number of available tools, prompts, etc.
type Counts struct {
	Tools     int
	Prompts   int
	Resources int
}

// ClientInfo holds information about an MCP client's state.
type ClientInfo struct {
	Name        string
	State       State
	Error       error
	Client      *ClientSession
	Counts      Counts
	ConnectedAt time.Time

	// Config is the configuration the server last successfully connected
	// with. Reconcile compares it against the live config to decide whether
	// a connected server needs a restart. It is recorded by updateState on
	// StateConnected and cleared on StateDisabled, so it never has to be
	// synced by hand.
	Config config.MCPConfig

	// PendingConfig is the configuration an in-flight initialization is
	// connecting with. It is set on StateStarting so a config change that
	// arrives mid-connect is compared against the attempt actually in
	// progress rather than the last successful one, which would leave the
	// server skipped as "starting" and never restarted for the new config.
	PendingConfig *config.MCPConfig

	// Channel reports whether this server is an active channel (declared the
	// claude/channel capability and opted in via --channels or
	// channel_enabled in config).
	Channel bool
}

// SubscribeEvents returns a channel for MCP events.
//
// Channel message events (EventChannelMessage) are excluded: they carry no
// workspace or session identity, and the MCP broker is process-global. Without
// this filter, every workspace that calls SubscribeEvents would receive every
// other workspace's channel events — a cross-workspace injection path.
// Consumers that deliver channel messages use SubscribeChannelEvents and are
// responsible for scoping each event to the workspaces that declared and
// opted in the originating server.
func SubscribeEvents(ctx context.Context) <-chan pubsub.Event[Event] {
	raw := broker.Subscribe(ctx)
	filtered := make(chan pubsub.Event[Event], 64)
	go func() {
		defer close(filtered)
		for ev := range raw {
			if ev.Payload.Type == EventChannelMessage {
				continue
			}
			select {
			case filtered <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return filtered
}

// SubscribeChannelEvents returns a channel carrying only channel message
// events (EventChannelMessage). The MCP broker is process-global, so these
// events are not scoped to any workspace: every consumer must check that the
// originating server is declared in the target workspace's MCP config and
// opted in (ChannelOptIn) before delivering, otherwise one workspace's
// channel messages leak into another — the injection path SubscribeEvents
// filters out.
func SubscribeChannelEvents(ctx context.Context) <-chan pubsub.Event[Event] {
	raw := broker.Subscribe(ctx)
	channelOnly := make(chan pubsub.Event[Event], 64)
	go func() {
		defer close(channelOnly)
		for ev := range raw {
			if ev.Payload.Type != EventChannelMessage {
				continue
			}
			select {
			case channelOnly <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return channelOnly
}

// GetStates returns the current state of all MCP clients
func GetStates() map[string]ClientInfo {
	return states.Copy()
}

// GetState returns the state of a specific MCP client
func GetState(name string) (ClientInfo, bool) {
	return states.Get(name)
}

// stateOpt mutates the ClientInfo a transition is about to publish. Config
// recording is opt-in: only the sites that actually own a config (the connect
// and starting paths) pass one, so the many error and count-refresh call sites
// can't accidentally clobber the recorded config by passing a zero value.
type stateOpt func(*ClientInfo)

// withConfig records the config now in effect. Used on StateConnected.
func withConfig(m config.MCPConfig) stateOpt {
	return func(i *ClientInfo) {
		i.Config = m
		i.PendingConfig = nil
	}
}

// withPending records the config an in-flight attempt is connecting with.
// Used on StateStarting.
func withPending(m config.MCPConfig) stateOpt {
	return func(i *ClientInfo) {
		mc := m
		i.PendingConfig = &mc
	}
}

// updateState updates the state of an MCP client and publishes an event.
//
// Config bookkeeping is split between the caller and the state machine:
//   - Callers that own a config opt in via withConfig (StateConnected) or
//     withPending (StateStarting). Everyone else leaves the recorded config
//     untouched, so an error or count refresh can't wipe it.
//   - The state machine owns the transitions with fixed semantics:
//     StateDisabled clears both so a later re-enable with an unchanged config
//     is seen as new rather than skipped as already initialized.
func updateState(name string, state State, err error, client *ClientSession, counts Counts, opts ...stateOpt) {
	prev, _ := states.Get(name)
	info := prev
	info.Name = name
	info.State = state
	info.Error = err
	info.Client = client
	info.Counts = counts
	info.Channel = client != nil && client.channel
	for _, opt := range opts {
		opt(&info)
	}
	switch state {
	case StateConnected:
		info.ConnectedAt = time.Now()
	case StateDisabled:
		info.Config = config.MCPConfig{}
		info.PendingConfig = nil
	case StateError:
		// A session that has errored is dead to us: close it so the child
		// process and its stdio pipes are released, and clear its registry
		// entries so the agent stops advertising capabilities it can no
		// longer call (without that, crush_info / the `/mcp` menu and the
		// tool list handed to the LLM diverge). Crucially, close exactly the
		// session that errored (the client argument): if the registry
		// already holds a DIFFERENT session — a newer, healthy one another
		// path installed — leave it and its registrations alone. Closing
		// "whatever is in the map" here let a stale error transition (e.g. a
		// refresh that raced a renewal) tear down the healthy replacement.
		switch {
		case client != nil:
			if cur, ok := sessions.Get(name); ok && cur == client {
				sessions.Del(name)
				allTools.Del(name)
				allPrompts.Del(name)
				allResources.Del(name)
			}
			closeSession(name, client)
		default:
			// No specific session errored (e.g. connect itself failed);
			// anything still registered under this name is unusable.
			if old, ok := sessions.Take(name); ok {
				closeSession(name, old)
			}
			allTools.Del(name)
			allPrompts.Del(name)
			allResources.Del(name)
		}
		// Never publish a dead session on the state.
		info.Client = nil
	}
	states.Set(name, info)

	// Publish state change event
	broker.Publish(pubsub.UpdatedEvent, Event{
		Type:   EventStateChanged,
		Name:   name,
		State:  state,
		Error:  err,
		Counts: counts,
	})
}
