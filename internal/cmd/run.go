package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"charm.land/log/v2"
	"github.com/charmbracelet/crush/internal/client"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/event"
	"github.com/charmbracelet/crush/internal/format"
	"github.com/charmbracelet/crush/internal/herdr"
	"github.com/charmbracelet/crush/internal/noninteractive"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/ui/anim"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Aliases: []string{"r"},
	Use:     "run [prompt...]",
	Short:   "Run a single non-interactive prompt",
	Long: `Run a single prompt in non-interactive mode and exit.
The prompt can be provided as arguments or piped from stdin.`,
	Example: `
# Run a simple prompt
crush run "Guess my 5 favorite Pokémon"

# Pipe input from stdin
curl https://charm.land | crush run "Summarize this website"

# Read from a file
crush run "What is this code doing?" <<< prrr.go

# Redirect output to a file
crush run "Generate a hot README for this project" > MY_HOT_README.md

# Run in quiet mode (hide the spinner)
crush run --quiet "Generate a README for this project"

# Run in verbose mode (show logs)
crush run --verbose "Generate a README for this project"

# Use a specific reasoning effort
# Levels depend on the model, unsupported values are rejected
# with the accepted values listed
crush run --reasoning-effort high "What is the meaning of life?"

# Run in fast mode (Claude Code, Codex and Abacus OpenAI priority models)
crush run --model codex-cli/gpt-6.1-sol --fast "Fix the failing test"

# Continue a previous session
crush run --session {session-id} "Follow up on your last response"

# Continue the most recent session
crush run --continue "Follow up on your last response"

  `,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, crashDone := recordCrashes()
		defer crashDone()
		var (
			quiet, _           = cmd.Flags().GetBool("quiet")
			verbose, _         = cmd.Flags().GetBool("verbose")
			largeModel, _      = cmd.Flags().GetString("model")
			smallModel, _      = cmd.Flags().GetString("small-model")
			reasoningEffort, _ = cmd.Flags().GetString("reasoning-effort")
			fast, _            = cmd.Flags().GetBool("fast")
			sessionID, _       = cmd.Flags().GetString("session")
			useLast, _         = cmd.Flags().GetBool("continue")
		)

		// Cancel on SIGINT or SIGTERM.
		ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		prompt := strings.Join(args, " ")

		prompt, err := MaybePrependStdin(prompt)
		if err != nil {
			slog.Error("Failed to read from stdin", "error", err)
			return err
		}

		if prompt == "" {
			return fmt.Errorf("no prompt provided")
		}

		event.SetNonInteractive(true)

		switch {
		case sessionID != "":
			event.SetContinueBySessionID(true)
		case useLast:
			event.SetContinueLastSession(true)
		}

		opts := noninteractive.Options{
			Prompt:            prompt,
			LargeModel:        largeModel,
			SmallModel:        smallModel,
			ReasoningEffort:   reasoningEffort,
			Fast:              fast,
			ContinueSessionID: sessionID,
			UseLast:           useLast,
		}

		hideSpinner := quiet || verbose
		if useClientServer(cmd) {
			return runOnServer(ctx, cmd, opts, verbose, hideSpinner)
		}
		return runInProcess(ctx, cmd, opts, verbose, hideSpinner)
	},
}

func init() {
	runCmd.Flags().BoolP("quiet", "q", false, "Hide spinner")
	runCmd.Flags().BoolP("verbose", "v", false, "Show logs")
	runCmd.Flags().StringP("model", "m", "", "Model to use. Accepts 'model' or 'provider/model' to disambiguate models with the same name across providers")
	runCmd.Flags().String("small-model", "", "Small model to use. If not provided, uses the default small model for the provider")
	runCmd.Flags().String("reasoning-effort", "", "Reasoning effort for the model (e.g. low, medium, high). Levels depend on the model; unsupported values are rejected with the accepted values listed")
	runCmd.Flags().Bool("fast", false, "Run the model in fast mode (Claude Code, Codex and Abacus OpenAI priority models)")
	runCmd.Flags().StringP("session", "s", "", "Continue a previous session by ID")
	runCmd.Flags().BoolP("continue", "C", false, "Continue the most recent session")
	runCmd.MarkFlagsMutuallyExclusive("session", "continue")
}

// runOnServer runs a `crush run` turn on the Crush server, starting it if
// needed.
func runOnServer(ctx context.Context, cmd *cobra.Command, opts noninteractive.Options, verbose, hideSpinner bool) error {
	c, ws, cleanup, err := connectToServer(cmd)
	if err != nil {
		return err
	}
	defer cleanup()

	event.AppInitialized()

	if !ws.Config.IsConfigured() {
		return fmt.Errorf("no providers configured - please run 'crush' to set up a provider interactively")
	}

	clientWs := workspace.NewClientWorkspace(c, *ws)
	if err := clientWs.InitCoderAgentNonInteractive(ctx); err != nil {
		return fmt.Errorf("failed to initialize agent: %w", err)
	}

	if opts.ContinueSessionID != "" {
		sess, err := resolveSessionByID(ctx, c, ws.ID, opts.ContinueSessionID)
		if err != nil {
			return err
		}
		opts.ContinueSessionID = sess.ID
	}

	if verbose {
		slog.SetDefault(slog.New(log.New(os.Stderr)))
	}

	return noninteractive.Run(ctx, &clientRun{c: c, ws: ws}, opts, runTerminal(ws.Config, hideSpinner))
}

// runInProcess runs a `crush run` turn in this process.
func runInProcess(ctx context.Context, cmd *cobra.Command, opts noninteractive.Options, verbose, hideSpinner bool) error {
	appInstance, cleanup, err := setupLocalApp(cmd)
	if err != nil {
		return err
	}
	defer cleanup()

	event.AppInitialized()

	if !appInstance.Config().IsConfigured() {
		return fmt.Errorf("no providers configured - please run 'crush' to set up a provider interactively")
	}

	if verbose {
		slog.SetDefault(slog.New(log.New(os.Stderr)))
	}

	if opts.ContinueSessionID != "" {
		sess, err := resolveSessionID(ctx, appInstance.Sessions, opts.ContinueSessionID)
		if err != nil {
			return err
		}
		opts.ContinueSessionID = sess.ID
	}

	// Re-initialize the coder agent without interactive-only tools.
	if err := appInstance.InitCoderAgentNonInteractive(ctx); err != nil {
		return fmt.Errorf("failed to reinitialize agent for non-interactive mode: %w", err)
	}

	return noninteractive.Run(ctx, appInstance.NonInteractive(), opts, runTerminal(appInstance.Config(), hideSpinner))
}

// runTerminal describes how `crush run` presents itself on this terminal:
// the reply on stdout, and, unless hidden, a spinner themed from cfg.
func runTerminal(cfg *config.Config, hideSpinner bool) noninteractive.Terminal {
	return noninteractive.Terminal{
		Stdout:      os.Stdout,
		HideSpinner: hideSpinner,
		Progress:    cfg.Options.Progress == nil || *cfg.Options.Progress,
		NewSpinner: func(ctx context.Context, cancel context.CancelFunc) noninteractive.Spinner {
			t := common.ThemeStylesFromConfig(cfg)
			return format.NewSpinner(ctx, cancel, anim.Settings{
				Size:        10,
				Label:       "Generating",
				GradColorA:  t.WorkingGradFromColor,
				GradColorB:  t.WorkingGradToColor,
				CycleColors: true,
			})
		},
	}
}

// clientRun is the [noninteractive.Backend] for client/server mode: it
// runs the turn on the server and streams the reply back over SSE. Model
// changes are written to the workspace's preferred models on the server.
type clientRun struct {
	c  *client.Client
	ws *proto.Workspace
}

func (r *clientRun) Config(ctx context.Context) (*config.Config, error) {
	cfg, err := r.c.GetConfig(ctx, r.ws.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get config: %w", err)
	}
	return cfg, nil
}

func (r *clientRun) SetModel(ctx context.Context, typ config.SelectedModelType, model config.SelectedModel) error {
	return r.c.UpdatePreferredModel(ctx, r.ws.ID, config.ScopeWorkspace, typ, model)
}

func (r *clientRun) DefaultSmallModel(ctx context.Context, providerID string) (*config.SelectedModel, error) {
	return r.c.GetDefaultSmallModel(ctx, r.ws.ID, providerID)
}

func (r *clientRun) LastAssistantModel(ctx context.Context, sessionID string) (string, string, error) {
	msgs, err := r.c.ListMessages(ctx, r.ws.ID, sessionID)
	if err != nil {
		return "", "", fmt.Errorf("failed to list messages: %w", err)
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == proto.Assistant && !msgs[i].IsSummaryMessage {
			return msgs[i].Provider, msgs[i].Model, nil
		}
	}
	return "", "", nil
}

func (r *clientRun) UpdateAgent(ctx context.Context) error {
	return r.c.UpdateAgent(ctx, r.ws.ID)
}

func (r *clientRun) Prepare(ctx context.Context) error {
	// Wait for the agent to become ready (MCP init, etc).
	if err := waitForAgent(ctx, r.c, r.ws.ID); err != nil {
		return fmt.Errorf("agent not ready: %w", err)
	}

	// Force-update agent models so MCP tools are loaded.
	if err := r.c.UpdateAgent(ctx, r.ws.ID); err != nil {
		slog.Warn("Failed to update agent", "error", err)
	}
	return nil
}

func (r *clientRun) ResolveSession(ctx context.Context, continueSessionID string, useLast bool) (string, error) {
	sess, err := resolveSession(ctx, r.c, r.ws.ID, continueSessionID, useLast)
	if err != nil {
		return "", fmt.Errorf("failed to resolve session: %w", err)
	}
	return sess.ID, nil
}

// Stream sends the prompt to the server and prints the reply from the
// workspace's event stream until this run's RunComplete arrives.
func (r *clientRun) Stream(ctx context.Context, out *noninteractive.Output, sessionID, prompt string) error {
	events, err := r.c.SubscribeEvents(ctx, r.ws.ID)
	if err != nil {
		return fmt.Errorf("failed to subscribe to events: %w", err)
	}

	// Mint a per-call RunID so we can correlate the terminal
	// RunComplete with *this* SendMessage even if the session was
	// busy and another turn finished first. Without it the stream
	// loop would exit on whichever RunComplete arrived first for
	// the same session and drop the queued prompt's output.
	runID := uuid.New().String()
	if err := r.c.SendMessage(ctx, r.ws.ID, sessionID, runID, "", prompt); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	stream := &runStream{
		sessionID: sessionID,
		runID:     runID,
		out:       out.Stdout,
		read:      make(map[string]int),
	}

	// Start herdr integration when running inside a herdr pane.
	hc := herdr.Init()
	hc.SetSessionID(sessionID)
	defer hc.Close()

	defer out.Finish()

	for {
		out.Heartbeat()

		select {
		case ev, ok := <-events:
			if !ok {
				out.StopSpinner()
				return nil
			}

			// Forward events to herdr if running inside a herdr pane.
			if hev := herdr.Translate(ev); hev != nil {
				hc.HandleEvent(hev)
			}

			done, err := stream.handle(ev, out.StopSpinner)
			if err != nil {
				return err
			}
			if done {
				return nil
			}

		case <-ctx.Done():
			out.StopSpinner()
			cancelCtx, cancelRequest := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelRequest()
			_ = r.c.CancelAgentSession(cancelCtx, r.ws.ID, sessionID)
			return ctx.Err()
		}
	}
}

// runStream tracks the per-message stdout cursor and the
// reconciliation state used by [clientRun.Stream] to translate
// streaming SSE events into a final, complete stdout for `crush run`.
// It is split out so the state machine can be exercised in unit tests
// without spinning up the full server/client harness.
//
// runID, when non-empty, is the authoritative correlator for the
// terminal RunComplete event: the stream suppresses live message
// events and only exits on a RunComplete whose RunID matches, so a
// turn that finishes first on the same session (e.g. when our prompt
// was queued behind a busy session) cannot contaminate stdout or
// terminate us prematurely. When empty (older servers, tests that
// don't supply one) the stream falls back to SessionID-only matching
// and live message streaming, which is still correct for the
// single-turn case.
type runStream struct {
	sessionID string
	runID     string
	out       io.Writer
	read      map[string]int
	printed   bool
}

// handle processes one SSE event. Returns done=true when the run
// loop should exit (RunComplete observed); returns an error only
// when the agent run failed (not on context cancel — that path is
// handled by the caller's select). stopSpinner is called on the
// first observable assistant output and on completion; passing nil
// is safe for tests.
func (s *runStream) handle(ev any, stopSpinner func()) (done bool, err error) {
	stop := func() {
		if stopSpinner != nil {
			stopSpinner()
		}
	}
	switch e := ev.(type) {
	case pubsub.Event[proto.Message]:
		msg := e.Payload
		if msg.SessionID != s.sessionID || msg.Role != proto.Assistant || len(msg.Parts) == 0 {
			return false, nil
		}
		if s.runID != "" {
			return false, nil
		}
		stop()

		content := msg.Content().String()
		readBytes := s.read[msg.ID]
		if len(content) < readBytes {
			slog.Error("Non-interactive: message content shorter than read bytes",
				"message_length", len(content), "read_bytes", readBytes)
			return false, fmt.Errorf("message content is shorter than read bytes: %d < %d", len(content), readBytes)
		}

		part := content[readBytes:]
		if readBytes == 0 {
			part = strings.TrimLeft(part, " \t")
		}
		if s.printed || strings.TrimSpace(part) != "" {
			s.printed = true
			fmt.Fprint(s.out, part)
		}
		s.read[msg.ID] = len(content)
		return false, nil

	case pubsub.Event[proto.RunComplete]:
		// RunComplete is the authoritative end-of-run signal. We
		// exit on it instead of guessing from message finish parts,
		// which fire on every tool-call step too and were the
		// source of the regression where `crush run` exited
		// mid-turn on finish.reason == tool_use.
		//
		// Correlation:
		//   - if we minted a RunID for this SendMessage, only the
		//     event whose RunID matches is ours; any other turn
		//     finishing first on the same session (busy-session
		//     queue path) must be ignored.
		//   - if we have no RunID (older server, tests), fall back
		//     to SessionID matching.
		if s.runID != "" {
			if e.Payload.RunID != s.runID {
				return false, nil
			}
		} else if e.Payload.SessionID != s.sessionID {
			return false, nil
		}
		stop()
		if e.Payload.Error != "" && !e.Payload.Cancelled {
			return true, fmt.Errorf("agent run failed: %s", e.Payload.Error)
		}
		// Reconcile stdout against the authoritative final
		// assistant text carried in the event. The pubsub fan-in
		// does not serialize publishes across upstream brokers, so
		// the final message event may not have reached this loop
		// yet; the embedded Text field is the backstop that
		// guarantees the full final text always appears on stdout.
		if e.Payload.MessageID != "" {
			full := e.Payload.Text
			readBytes := s.read[e.Payload.MessageID]
			if readBytes < len(full) {
				tail := full[readBytes:]
				if readBytes == 0 {
					tail = strings.TrimLeft(tail, " \t")
				}
				if s.printed || strings.TrimSpace(tail) != "" {
					s.printed = true
					fmt.Fprint(s.out, tail)
				}
			}
		}
		return true, nil

	case pubsub.Event[proto.AgentEvent]:
		if e.Payload.Error == nil {
			return false, nil
		}
		// Attribute the error to our run before treating it as
		// fatal. Async errors from an unrelated workspace run share
		// this channel, so a foreign failure must not abort us:
		//   - if the event carries a RunID, it is the authoritative
		//     correlator: it must match our run exactly, otherwise it
		//     belongs to a different request and we ignore it.
		//   - if the event carries no RunID (older server), fall back
		//     to SessionID: it must be present and match our session,
		//     otherwise we ignore it.
		if e.Payload.RunID != "" {
			if e.Payload.RunID != s.runID {
				return false, nil
			}
		} else if e.Payload.SessionID == "" || e.Payload.SessionID != s.sessionID {
			return false, nil
		}
		stop()
		return true, fmt.Errorf("agent error: %w", e.Payload.Error)
	}
	return false, nil
}

// waitForAgent polls GetAgentInfo until the agent is ready, with a
// timeout.
func waitForAgent(ctx context.Context, c *client.Client, wsID string) error {
	timeout := time.After(30 * time.Second)
	for {
		info, err := c.GetAgentInfo(ctx, wsID)
		if err == nil && info.IsReady {
			return nil
		}
		select {
		case <-timeout:
			if err != nil {
				return fmt.Errorf("timeout waiting for agent: %w", err)
			}
			return fmt.Errorf("timeout waiting for agent readiness")
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// resolveSession returns the session to use for a non-interactive run.
// If continueSessionID is set it fetches that session; if useLast is set it
// returns the most recently updated top-level session; otherwise it creates a
// new one.
func resolveSession(ctx context.Context, c *client.Client, wsID, continueSessionID string, useLast bool) (*proto.Session, error) {
	switch {
	case continueSessionID != "":
		sess, err := c.GetSession(ctx, wsID, continueSessionID)
		if err != nil {
			return nil, fmt.Errorf("session not found: %s", continueSessionID)
		}
		if sess.ParentSessionID != "" {
			return nil, fmt.Errorf("cannot continue a child session: %s", continueSessionID)
		}
		return sess, nil

	case useLast:
		sessions, err := c.ListSessions(ctx, wsID)
		if err != nil || len(sessions) == 0 {
			return nil, fmt.Errorf("no sessions found to continue")
		}
		last := sessions[0]
		for _, s := range sessions[1:] {
			if s.UpdatedAt > last.UpdatedAt && s.ParentSessionID == "" {
				last = s
			}
		}
		return &last, nil

	default:
		return c.CreateSession(ctx, wsID, "non-interactive")
	}
}

// resolveSessionByID resolves a session ID that may be a full UUID or a hash
// prefix returned by crush session list.
func resolveSessionByID(ctx context.Context, c *client.Client, wsID, id string) (*proto.Session, error) {
	if sess, err := c.GetSession(ctx, wsID, id); err == nil {
		return sess, nil
	}

	sessions, err := c.ListSessions(ctx, wsID)
	if err != nil {
		return nil, err
	}

	matches := sessionsMatchingHash(sessions, id, func(s proto.Session) string { return s.ID })

	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("session %q not found", id)
	case 1:
		return &matches[0], nil
	default:
		return nil, fmt.Errorf("session ID %q is ambiguous (%d matches)", id, len(matches))
	}
}
