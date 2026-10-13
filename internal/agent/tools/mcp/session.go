package mcp

import (
	"context"
	"log/slog"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	mcpoauth "github.com/charmbracelet/crush/internal/oauth/mcp"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ClientSession wraps an mcp.ClientSession with a context cancel function so
// that the context created during session establishment is properly cleaned up
// on close.
type ClientSession struct {
	*mcp.ClientSession
	cancel       context.CancelFunc
	oauthHandler *mcpoauth.Handler
	// channel reports whether this server is an active channel (it declared
	// the claude/channel capability and was opted in via --channels or
	// channel_enabled in config).
	channel bool
}

// Close cancels the session context and then closes the underlying session.
func (s *ClientSession) Close() error {
	s.cancel()
	if s.oauthHandler != nil {
		s.oauthHandler.Close()
	}
	return s.ClientSession.Close()
}

func createSession(ctx context.Context, cfg *config.ConfigStore, name string, m config.MCPConfig, resolver config.VariableResolver, channelOptIn bool) (*ClientSession, error) {
	timeout := mcpTimeout(m)
	mcpCtx, cancel := context.WithCancel(ctx)
	cancelTimer := time.AfterFunc(timeout, cancel)

	transport, oauthHandler, err := createTransport(mcpCtx, cfg, name, m, resolver)
	if err != nil {
		updateState(name, StateError, err, nil, Counts{})
		slog.Error("Error creating MCP client", "error", err, "name", name)
		cancel()
		cancelTimer.Stop()
		return nil, err
	}

	// If the caller requested a browser-suppressed flow (server-driven
	// remote auth), suppress the handler's local browser open; the caller
	// surfaces MCPAuthURL(name) to the user on their own machine.
	if oauthHandler != nil {
		if suppress, _ := ctx.Value(suppressBrowserKey{}).(bool); suppress {
			oauthHandler.SetBrowserSuppress(true)
		}
	}

	// Wrap the transport so channel notifications can be intercepted. The
	// gate starts undecided: notifications that arrive during capability
	// negotiation are buffered. After Connect resolves, the gate is opened
	// (and the buffer drained) only when the server declares the channel
	// capability AND was opted in (--channels or channel_enabled);
	// otherwise it is closed (buffer discarded). This prevents early
	// notifications from being lost.
	channelGate := newChannelGate()
	transport = &channelTransport{inner: transport, name: name, gate: channelGate}

	// When the server is marked Sessionless, the tools/prompts/resources
	// list-changed handlers are omitted. The go-sdk opens a SEP-2575
	// "subscriptions/listen" stream whenever any of those handlers is set
	// (client.go), and sessionless streamable-HTTP servers such as GitHub MCP
	// answer that POST with 404 ("session not found"), which the SDK treats
	// as fatal and tears the connection down. Setting the flag lets those
	// servers connect at the cost of live list-changed notifications.
	opts := &mcp.ClientOptions{
		LoggingMessageHandler: func(ctx context.Context, req *mcp.LoggingMessageRequest) {
			level := parseLevel(string(req.Params.Level))
			slog.Log(ctx, level, "MCP log", "name", name, "logger", req.Params.Logger, "data", req.Params.Data)
		},
	}
	if !m.IsSessionless(resolver) {
		opts.ToolListChangedHandler = func(context.Context, *mcp.ToolListChangedRequest) {
			broker.Publish(pubsub.UpdatedEvent, Event{
				Type: EventToolsListChanged,
				Name: name,
			})
		}
		opts.PromptListChangedHandler = func(context.Context, *mcp.PromptListChangedRequest) {
			broker.Publish(pubsub.UpdatedEvent, Event{
				Type: EventPromptsListChanged,
				Name: name,
			})
		}
		opts.ResourceListChangedHandler = func(context.Context, *mcp.ResourceListChangedRequest) {
			broker.Publish(pubsub.UpdatedEvent, Event{
				Type: EventResourcesListChanged,
				Name: name,
			})
		}
	}
	client := mcp.NewClient(
		&mcp.Implementation{
			Name:    "crush",
			Version: version.Version,
			Title:   "Crush",
		},
		opts,
	)

	session, err := client.Connect(mcpCtx, transport, nil)
	if err != nil {
		err = maybeStdioErr(err, transport)
		updateState(name, StateError, maybeTimeoutErr(err, timeout), nil, Counts{})
		slog.Error("MCP client failed to initialize", "error", err, "name", name)
		cancel()
		cancelTimer.Stop()
		return nil, err
	}

	cancelTimer.Stop()
	slog.Debug("MCP client initialized", "name", name)

	// Resolve the channel gate: open only for a server that both declares
	// the claude/channel capability and was opted in — via --channels or
	// channel_enabled in config. Merely listing a server under mcp is not
	// enough. Otherwise close the gate (fail closed). Resolving drains buffered messages
	// that arrived during negotiation so a fast server does not lose early
	// events.
	isChannel := channelOptIn && hasChannelCapability(session.InitializeResult())
	if isChannel {
		buffered := channelGate.resolve(true)
		for _, raw := range buffered {
			publishChannelMessage(mcpCtx, name, raw)
		}
		slog.Info("MCP channel enabled", "name", name, "buffered", len(buffered))
	} else {
		channelGate.resolve(false)
	}

	return &ClientSession{
		ClientSession: session,
		cancel:        cancel,
		oauthHandler:  oauthHandler,
		channel:       isChannel,
	}, nil
}
