package app

import (
	"context"
	"log/slog"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/log"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/secureentry"
)

func (app *App) setupEvents() {
	ctx, cancel := context.WithCancel(app.globalCtx)
	app.eventsCtx = ctx
	app.subscribe(ctx, "sessions", app.Sessions.Subscribe)
	app.subscribe(ctx, "messages", app.Messages.Subscribe)
	app.subscribeMustDeliver(ctx, "permissions", app.Permissions.Subscribe)
	app.subscribeMustDeliver(ctx, "permissions-notifications", app.Permissions.SubscribeNotifications)
	app.subscribeMustDeliver(ctx, "question-batches", app.Questions.Subscribe)
	app.subscribeMustDeliver(ctx, "question-notifications", app.Questions.SubscribeNotifications)
	app.subscribe(ctx, "history", app.History.Subscribe)
	app.subscribe(ctx, "agent-notifications", app.agentNotifications.Subscribe)
	app.subscribeMustDeliver(ctx, "run-completions", app.runCompletions.Subscribe)
	app.subscribe(ctx, "tasks", app.tasks.Subscribe)
	app.subscribe(ctx, "mcp", mcp.SubscribeEvents)
	app.subscribe(ctx, "mcp-channels", app.subscribeScopedChannelEvents)
	app.subscribe(ctx, "lsp", SubscribeLSPEvents)
	if app.Skills != nil {
		app.subscribe(ctx, "skills", app.Skills.SubscribeEvents)
	}
	cleanupFunc := func(context.Context) error {
		cancel()
		app.serviceEventsWG.Wait()
		app.events.Shutdown()
		return nil
	}
	app.cleanupFuncs = append(app.cleanupFuncs, cleanupFunc)
}

// subscribeScopedChannelEvents forwards channel message events for servers
// this workspace both declares in its MCP config and opted in (via
// --channels or channel_enabled; see mcp.ChannelOptIn).
// The MCP broker is process-global and channel events carry no workspace
// identity, so this per-app scoping is what keeps another workspace's channel
// messages out of this app's event stream (see mcp.SubscribeChannelEvents).
// The scoped events feed the TUI's in-process injection and, in server mode,
// the SSE stream to attached clients.
func (app *App) subscribeScopedChannelEvents(ctx context.Context) <-chan pubsub.Event[mcp.Event] {
	raw := mcp.SubscribeChannelEvents(ctx)
	scoped := make(chan pubsub.Event[mcp.Event], 64)
	go func() {
		defer close(scoped)
		for ev := range raw {
			mcpCfg, declared := app.config.Config().MCP[ev.Payload.Name]
			if !declared {
				continue
			}
			if !mcp.ChannelOptIn(mcpCfg, app.config.Overrides().EnabledChannels, ev.Payload.Name) {
				continue
			}
			select {
			case scoped <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return scoped
}

// subscribe fans a service's event stream into the shared app.events
// broker on app.serviceEventsWG, re-publishing each upstream event as a
// tea.Msg. The goroutine exits when ctx is cancelled or the upstream
// channel closes. It is a generic method (Go 1.27) so it can live in the
// App namespace while still inferring the upstream event type T.
func (app *App) subscribe[T any](
	ctx context.Context,
	name string,
	subscriber func(context.Context) <-chan pubsub.Event[T],
) {
	// Subscribe before returning, so no event published right after setup
	// is lost while the goroutine is still waiting to run.
	subCh := subscriber(ctx)
	app.serviceEventsWG.Go(func() {
		for {
			select {
			case event, ok := <-subCh:
				if !ok {
					slog.Debug("Subscription channel closed", "name", name)
					return
				}
				app.events.Publish(pubsub.UpdatedEvent, tea.Msg(event))
			case <-ctx.Done():
				slog.Debug("Subscription cancelled", "name", name)
				return
			}
		}
	})
}

// subscribeMustDeliver is the bounded-blocking fan-in variant of
// [App.subscribe]: it re-publishes upstream events onto the shared
// app.events broker using PublishMustDeliver instead of Publish. Use
// this for terminal events that subscribers cannot tolerate losing —
// notably RunComplete, which is the authoritative end-of-run signal
// for `crush run`. A lossy fan-in here can drop the only terminal
// event and hang non-interactive clients waiting on it.
func (app *App) subscribeMustDeliver[T any](
	ctx context.Context,
	name string,
	subscriber func(context.Context) <-chan pubsub.Event[T],
) {
	// Subscribe before returning, so no event published right after setup
	// is lost while the goroutine is still waiting to run.
	subCh := subscriber(ctx)
	app.serviceEventsWG.Go(func() {
		for {
			select {
			case event, ok := <-subCh:
				if !ok {
					slog.Debug("Subscription channel closed", "name", name)
					return
				}
				app.events.PublishMustDeliver(ctx, pubsub.UpdatedEvent, tea.Msg(event))
			case <-ctx.Done():
				slog.Debug("Subscription cancelled", "name", name)
				return
			}
		}
	})
}

// Subscribe sends events to the TUI as tea.Msgs.
func (app *App) Subscribe(program *tea.Program) {
	defer log.RecoverPanic("app.Subscribe", func() {
		slog.Info("TUI subscription panic: attempting graceful shutdown")
		program.Quit()
	})

	app.tuiWG.Add(1)
	tuiCtx, tuiCancel := context.WithCancel(app.globalCtx)
	app.cleanupFuncs = append(app.cleanupFuncs, func(context.Context) error {
		slog.Debug("Cancelling TUI message handler")
		tuiCancel()
		app.tuiWG.Wait()
		return nil
	})
	defer app.tuiWG.Done()

	detachSecureForms := secureentry.AttachForms(func(form *secureentry.Form) { program.Send(form) })
	defer detachSecureForms()
	events := app.events.Subscribe(tuiCtx)
	// Now that the TUI hears task events, resume the sub-agents an earlier
	// Crush left unfinished.
	agent.ResumeInterruptedTasks()
	for {
		select {
		case <-tuiCtx.Done():
			slog.Debug("TUI message handler shutting down")
			return
		case ev, ok := <-events:
			if !ok {
				slog.Debug("TUI message channel closed")
				return
			}
			program.Send(ev.Payload)
		}
	}
}
