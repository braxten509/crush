package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/noninteractive"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/session"
)

// NonInteractive returns the in-process [noninteractive.Backend] for a
// `crush run` turn. The chat's model is saved with the chat; a helper model
// override is in memory only.
func (app *App) NonInteractive() noninteractive.Backend {
	return nonInteractive{app: app}
}

// nonInteractive runs a non-interactive turn directly against the app's
// services.
type nonInteractive struct {
	app *App
}

func (n nonInteractive) Config(context.Context) (*config.Config, error) {
	return n.app.config.Config(), nil
}

func (n nonInteractive) ChatModel(ctx context.Context, sessionID string) (config.SelectedModel, bool, error) {
	sess, err := n.app.Sessions.Get(ctx, sessionID)
	if err != nil {
		return config.SelectedModel{}, false, fmt.Errorf("failed to read the chat's model: %w", err)
	}
	var m config.SelectedModel
	if len(sess.Model) == 0 || json.Unmarshal(sess.Model, &m) != nil || m.Provider == "" || m.Model == "" {
		return config.SelectedModel{}, false, nil
	}
	return m, true, nil
}

// SetChatModel saves model on the chat. It is the owner's pick, so it is
// also the last pick a new chat starts on.
func (n nonInteractive) SetChatModel(ctx context.Context, sessionID string, model config.SelectedModel) error {
	data, err := json.Marshal(model)
	if err != nil {
		return err
	}
	if _, err := n.app.Sessions.SetModel(ctx, sessionID, data); err != nil {
		return err
	}
	if err := config.WriteLastPick(model); err != nil {
		slog.Warn("Could not save the last picked model", "error", err)
	}
	return nil
}

// SetHelperModel sets the helper model in memory, for this run only.
func (n nonInteractive) SetHelperModel(_ context.Context, model config.SelectedModel) error {
	n.app.config.OverridePreferredModel(config.SelectedModelTypeSmall, model)
	return nil
}

func (n nonInteractive) UpdateAgent(ctx context.Context) error {
	return n.app.AgentCoordinator.UpdateModels(ctx)
}

func (n nonInteractive) Prepare(ctx context.Context) error {
	// Non-interactive runs get a single shot at the tool palette, so wait for
	// MCP initialization to settle before reading MCP tools. The coordinator
	// waits again for the same reason (it is the gate the client/server path
	// goes through); doing it here too surfaces the failure before we create a
	// session, and lets the UpdateModels below see every MCP tool. The wait is
	// bounded so a server wedged mid-handshake cannot stall the one-shot run
	// for its full connect timeout; past the budget the run proceeds without
	// the unfinished servers' tools.
	if err := mcp.WaitForInitBudget(ctx, mcp.InitWaitBudget); err != nil {
		return fmt.Errorf("failed to wait for MCP initialization: %w", err)
	}

	// force update of agent models before running so mcp tools are loaded
	n.app.AgentCoordinator.UpdateModels(ctx)
	return nil
}

func (n nonInteractive) ResolveSession(ctx context.Context, continueSessionID string, useLast bool) (string, error) {
	sess, err := n.app.resolveSession(ctx, continueSessionID, useLast)
	if err != nil {
		return "", fmt.Errorf("failed to create session for non-interactive mode: %w", err)
	}
	return sess.ID, nil
}

// resolveSession resolves which session to use for a non-interactive run
// If continueSessionID is set, it looks up that session by ID
// If useLast is set, it returns the most recently updated top-level session
// Otherwise, it creates a new session
func (app *App) resolveSession(ctx context.Context, continueSessionID string, useLast bool) (session.Session, error) {
	switch {
	case continueSessionID != "":
		if app.Sessions.IsAgentToolSession(continueSessionID) {
			return session.Session{}, fmt.Errorf("cannot continue an agent tool session: %s", continueSessionID)
		}
		sess, err := app.Sessions.Get(ctx, continueSessionID)
		if err != nil {
			return session.Session{}, fmt.Errorf("session not found: %s", continueSessionID)
		}
		if sess.ParentSessionID != "" {
			return session.Session{}, fmt.Errorf("cannot continue a child session: %s", continueSessionID)
		}
		return sess, nil

	case useLast:
		sess, err := app.Sessions.GetLast(ctx)
		if err != nil {
			return session.Session{}, fmt.Errorf("no sessions found to continue")
		}
		return sess, nil

	default:
		return app.Sessions.Create(ctx, agent.DefaultSessionName)
	}
}

// Stream runs the agent on the session in a goroutine and prints the
// assistant's reply as it streams in, ending on the run's result.
func (n nonInteractive) Stream(ctx context.Context, out *noninteractive.Output, sessionID, prompt string) error {
	app := n.app

	// Automatically approve all permission requests for this non-interactive
	// session.
	app.Permissions.AutoApproveSession(sessionID)

	// Report session identity to herdr.
	app.ReportCurrentSession(sessionID)

	type response struct {
		result *fantasy.AgentResult
		err    error
	}
	done := make(chan response, 1)

	messageEvents := app.Messages.Subscribe(ctx)
	completionEvents := app.runCompletions.Subscribe(ctx)
	var lastCompletion notify.RunComplete
	readCompletion := func(event pubsub.Event[notify.RunComplete]) {
		if event.Payload.SessionID == sessionID {
			lastCompletion = event.Payload
		}
	}
	// Waiting on sub-agents is reported on stderr, one line each time it
	// starts or ends, so callers such as benchmarks can show it.
	statusEvents := make(chan bool, 16)
	runCtx := agent.WithRunStatus(ctx, func(waiting bool) {
		select {
		case statusEvents <- waiting:
		case <-ctx.Done():
		}
	})
	go func(ctx context.Context, sessionID, prompt string) {
		result, err := app.AgentCoordinator.Run(ctx, sessionID, prompt)
		if err != nil {
			done <- response{
				err: fmt.Errorf("failed to start agent processing stream: %w", err),
			}
			return
		}
		done <- response{
			result: result,
		}
	}(runCtx, sessionID, prompt)

	printer := replyPrinter{out: out, read: make(map[string]int)}

	defer out.Finish()

	for {
		// HACK: Reinitialize the terminal progress bar on every iteration
		// so it doesn't get hidden by the terminal due to inactivity.
		out.Heartbeat()

		select {
		case result := <-done:
			out.StopSpinner()
			if result.err != nil {
				if errors.Is(result.err, context.Canceled) || errors.Is(result.err, agent.ErrRequestCancelled) {
					slog.Debug("Non-interactive: agent processing cancelled", "session_id", sessionID)
					return nil
				}
				return fmt.Errorf("agent processing failed: %w", result.err)
			}
			// Completion can beat the last message event through the pubsub
			// fan-in. The coordinator publishes before returning, so drain
			// its broker and use the exact final message ID. Timestamp order
			// is ambiguous when several replies finish in the same second.
			drainCompletions(completionEvents, readCompletion)
			return n.printFinalReply(ctx, &printer, sessionID, lastCompletion.MessageID)

		case event, ok := <-completionEvents:
			if !ok {
				completionEvents = nil
			} else {
				readCompletion(event)
			}

		case waiting := <-statusEvents:
			out.SubAgentStatus(waiting, printer.printed)

		case event := <-messageEvents:
			msg := event.Payload
			if msg.SessionID == sessionID && msg.Role == message.Assistant && len(msg.Parts) > 0 {
				out.StopSpinner()

				if err := printer.print(msg); err != nil {
					return err
				}
			}

		case <-ctx.Done():
			out.StopSpinner()
			return ctx.Err()
		}
	}
}

// drainCompletions passes every completion event already queued on events
// to read, without waiting for more.
func drainCompletions(events <-chan pubsub.Event[notify.RunComplete], read func(pubsub.Event[notify.RunComplete])) {
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}
			read(event)
		default:
			return
		}
	}
}

// printFinalReply prints whatever of the run's final assistant message has
// not been printed yet: the message the run completed with when known,
// otherwise the session's last assistant message.
func (n nonInteractive) printFinalReply(ctx context.Context, printer *replyPrinter, sessionID, messageID string) error {
	var msg message.Message
	var err error
	if messageID != "" {
		msg, err = n.app.Messages.Get(ctx, messageID)
	} else {
		msg, err = n.app.Messages.GetLastAssistantMessage(ctx, sessionID)
	}
	if err == nil {
		return printer.print(msg)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return nil
}

// replyPrinter writes the unseen tail of each assistant message to stdout,
// separating consecutive messages with a blank line and skipping leading
// whitespace-only output.
type replyPrinter struct {
	out           *noninteractive.Output
	read          map[string]int
	printed       bool
	lastMessageID string
}

func (p *replyPrinter) print(msg message.Message) error {
	content := msg.Content().String()
	readBytes := p.read[msg.ID]
	if len(content) < readBytes {
		return fmt.Errorf("message content is shorter than read bytes: %d < %d", len(content), readBytes)
	}
	part := content[readBytes:]
	if readBytes == 0 {
		part = strings.TrimLeft(part, " \t")
	}
	if (p.printed || strings.TrimSpace(part) != "") && part != "" {
		if p.printed && p.lastMessageID != msg.ID {
			fmt.Fprint(p.out.Stdout, "\n\n")
		}
		p.printed = true
		p.lastMessageID = msg.ID
		fmt.Fprint(p.out.Stdout, part)
	}
	p.read[msg.ID] = len(content)
	return nil
}
