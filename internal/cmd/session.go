package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/event"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/charmtone"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

var sessionCmd = &cobra.Command{
	Use:     "session",
	Aliases: []string{"sessions", "s"},
	Short:   "Manage sessions",
	Long:    "Manage Crush sessions. Agents can use --json for machine-readable output.",
}

var (
	sessionListJSON   bool
	sessionShowJSON   bool
	sessionLastJSON   bool
	sessionDeleteJSON bool
	sessionRenameJSON bool
)

var sessionListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all sessions",
	Long:    "List all sessions. Use --json for machine-readable output.",
	RunE:    runSessionList,
}

var sessionShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show session details",
	Long:  "Show session details. Use --json for machine-readable output. ID can be a UUID, full hash, or hash prefix.",
	Args:  cobra.ExactArgs(1),
	RunE:  runSessionShow,
}

var sessionLastCmd = &cobra.Command{
	Use:   "last",
	Short: "Show most recent session",
	Long:  "Show the last updated session. Use --json for machine-readable output.",
	RunE:  runSessionLast,
}

var sessionDeleteCmd = &cobra.Command{
	Use:     "delete <id>",
	Aliases: []string{"rm"},
	Short:   "Delete a session",
	Long:    "Delete a session by ID. Use --json for machine-readable output. ID can be a UUID, full hash, or hash prefix.",
	Args:    cobra.ExactArgs(1),
	RunE:    runSessionDelete,
}

var sessionRenameCmd = &cobra.Command{
	Use:   "rename <id> <title>",
	Short: "Rename a session",
	Long:  "Rename a session by ID. Use --json for machine-readable output. ID can be a UUID, full hash, or hash prefix.",
	Args:  cobra.MinimumNArgs(2),
	RunE:  runSessionRename,
}

func init() {
	sessionListCmd.Flags().BoolVar(&sessionListJSON, "json", false, "output in JSON format")
	sessionShowCmd.Flags().BoolVar(&sessionShowJSON, "json", false, "output in JSON format")
	sessionLastCmd.Flags().BoolVar(&sessionLastJSON, "json", false, "output in JSON format")
	sessionDeleteCmd.Flags().BoolVar(&sessionDeleteJSON, "json", false, "output in JSON format")
	sessionRenameCmd.Flags().BoolVar(&sessionRenameJSON, "json", false, "output in JSON format")
	sessionCmd.AddCommand(sessionListCmd)
	sessionCmd.AddCommand(sessionShowCmd)
	sessionCmd.AddCommand(sessionLastCmd)
	sessionCmd.AddCommand(sessionDeleteCmd)
	sessionCmd.AddCommand(sessionRenameCmd)
}

type sessionServices struct {
	sessions session.Service
	messages message.Service
	cfg      *config.ConfigStore
}

func sessionSetup(cmd *cobra.Command) (context.Context, *sessionServices, func(), error) {
	dataDir, _ := cmd.Flags().GetString("data-dir")
	ctx := cmd.Context()

	cwd, err := ResolveCwd(cmd)
	if err != nil {
		return nil, nil, nil, err
	}

	cfg, err := config.Init(cwd, dataDir, false)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to initialize config: %w", err)
	}
	if dataDir == "" {
		dataDir = cfg.Config().Options.DataDirectory
	}
	if shouldEnableMetrics(cfg.Config()) {
		event.Init()
	}

	conn, err := db.Connect(ctx, dataDir)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	queries := db.New(conn)
	svc := &sessionServices{
		sessions: session.NewService(queries, conn),
		messages: message.NewService(queries),
		cfg:      cfg,
	}
	return ctx, svc, func() { conn.Close() }, nil
}

func runSessionList(cmd *cobra.Command, _ []string) error {
	event.SetNonInteractive(true)

	ctx, svc, cleanup, err := sessionSetup(cmd)
	if err != nil {
		return err
	}
	defer cleanup()

	event.SessionListed(sessionListJSON)

	list, err := svc.sessions.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	if sessionListJSON {
		out := cmd.OutOrStdout()
		output := make([]sessionJSON, len(list))
		for i, s := range list {
			output[i] = sessionJSON{
				ID:       session.HashID(s.ID),
				UUID:     s.ID,
				Title:    s.Title,
				Created:  time.Unix(s.CreatedAt, 0).Format(time.RFC3339),
				Modified: time.Unix(s.UpdatedAt, 0).Format(time.RFC3339),
			}
		}
		enc := json.NewEncoder(out)
		enc.SetEscapeHTML(false)
		return enc.Encode(output)
	}

	w, cleanup, usingPager := sessionWriter(ctx, len(list))
	defer cleanup()

	hashStyle := lipgloss.NewStyle().Foreground(charmtone.Malibu)
	dateStyle := lipgloss.NewStyle().Foreground(charmtone.Damson)

	width := sessionOutputWidth
	if tw, _, err := term.GetSize(os.Stdout.Fd()); err == nil && tw > 0 {
		width = tw
	}
	// 7 (hash) + 1 (space) + 25 (RFC3339 date) + 1 (space) = 34 chars prefix.
	titleWidth := max(width-34, 10)

	var writeErr error
	for _, s := range list {
		hash := session.HashID(s.ID)[:7]
		date := time.Unix(s.CreatedAt, 0).Format(time.RFC3339)
		title := strings.ReplaceAll(s.Title, "\n", " ")
		title = ansi.Truncate(title, titleWidth, "…")
		_, writeErr = fmt.Fprintln(w, hashStyle.Render(hash), dateStyle.Render(date), title)
		if writeErr != nil {
			break
		}
	}
	if writeErr != nil && usingPager && isBrokenPipe(writeErr) {
		return nil
	}
	return writeErr
}

type sessionJSON struct {
	ID       string `json:"id"`
	UUID     string `json:"uuid"`
	Title    string `json:"title"`
	Created  string `json:"created"`
	Modified string `json:"modified"`
}

type sessionMutationResult struct {
	ID      string `json:"id"`
	UUID    string `json:"uuid"`
	Title   string `json:"title"`
	Deleted bool   `json:"deleted,omitempty"`
	Renamed bool   `json:"renamed,omitempty"`
}

// sessionsMatchingHash returns the sessions whose hash ID (see
// [session.HashID]) equals or starts with id. idOf returns a session's ID.
func sessionsMatchingHash[S any](sessions []S, id string, idOf func(S) string) []S {
	var matches []S
	for _, s := range sessions {
		if strings.HasPrefix(session.HashID(idOf(s)), id) {
			matches = append(matches, s)
		}
	}
	return matches
}

// resolveSessionID resolves a session ID that can be a UUID, full hash, or hash prefix.
// Returns an error if the prefix is ambiguous (matches multiple sessions).
func resolveSessionID(ctx context.Context, svc session.Service, id string) (session.Session, error) {
	// Try direct UUID lookup first
	if s, err := svc.Get(ctx, id); err == nil {
		return s, nil
	}

	// List all sessions and check for hash matches
	sessions, err := svc.List(ctx)
	if err != nil {
		return session.Session{}, err
	}

	matches := sessionsMatchingHash(sessions, id, func(s session.Session) string { return s.ID })

	if len(matches) == 0 {
		return session.Session{}, fmt.Errorf("session not found: %s", id)
	}

	if len(matches) == 1 {
		return matches[0], nil
	}

	// Ambiguous - show matches like Git does
	var sb strings.Builder
	fmt.Fprintf(&sb, "session ID '%s' is ambiguous. Matches:\n\n", id)
	for _, m := range matches {
		hash := session.HashID(m.ID)
		created := time.Unix(m.CreatedAt, 0).Format("2006-01-02")
		// Keep title on one line by replacing newlines with spaces, and truncate.
		title := strings.ReplaceAll(m.Title, "\n", " ")
		title = ansi.Truncate(title, 50, "…")
		fmt.Fprintf(&sb, "  %s... %q (created %s)\n", hash[:12], title, created)
	}
	sb.WriteString("\nUse more characters or the full hash")
	return session.Session{}, errors.New(sb.String())
}

func runSessionShow(cmd *cobra.Command, args []string) error {
	event.SetNonInteractive(true)

	ctx, svc, cleanup, err := sessionSetup(cmd)
	if err != nil {
		return err
	}
	defer cleanup()

	event.SessionShown(sessionShowJSON)

	sess, err := resolveSessionID(ctx, svc.sessions, args[0])
	if err != nil {
		return err
	}

	msgs, err := svc.messages.List(ctx, sess.ID)
	if err != nil {
		return fmt.Errorf("failed to list messages: %w", err)
	}

	msgPtrs := messagePtrs(msgs)
	if sessionShowJSON {
		return outputSessionJSON(cmd.OutOrStdout(), sess, msgPtrs)
	}
	return outputSessionHuman(ctx, svc.cfg, sess, msgPtrs)
}

func runSessionDelete(cmd *cobra.Command, args []string) error {
	event.SetNonInteractive(true)

	ctx, svc, cleanup, err := sessionSetup(cmd)
	if err != nil {
		return err
	}
	defer cleanup()

	event.SessionDeletedCommand(sessionDeleteJSON)

	sess, err := resolveSessionID(ctx, svc.sessions, args[0])
	if err != nil {
		return err
	}

	if err := svc.sessions.Delete(ctx, sess.ID); err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	out := cmd.OutOrStdout()
	if sessionDeleteJSON {
		enc := json.NewEncoder(out)
		enc.SetEscapeHTML(false)
		return enc.Encode(sessionMutationResult{
			ID:      session.HashID(sess.ID),
			UUID:    sess.ID,
			Title:   sess.Title,
			Deleted: true,
		})
	}

	fmt.Fprintf(out, "Deleted session %s\n", session.HashID(sess.ID)[:12])
	return nil
}

func runSessionRename(cmd *cobra.Command, args []string) error {
	event.SetNonInteractive(true)

	ctx, svc, cleanup, err := sessionSetup(cmd)
	if err != nil {
		return err
	}
	defer cleanup()

	event.SessionRenamed(sessionRenameJSON)

	sess, err := resolveSessionID(ctx, svc.sessions, args[0])
	if err != nil {
		return err
	}

	newTitle := strings.Join(args[1:], " ")
	if err := svc.sessions.Rename(ctx, sess.ID, newTitle); err != nil {
		return fmt.Errorf("failed to rename session: %w", err)
	}

	out := cmd.OutOrStdout()
	if sessionRenameJSON {
		enc := json.NewEncoder(out)
		enc.SetEscapeHTML(false)
		return enc.Encode(sessionMutationResult{
			ID:      session.HashID(sess.ID),
			UUID:    sess.ID,
			Title:   newTitle,
			Renamed: true,
		})
	}

	fmt.Fprintf(out, "Renamed session %s to %q\n", session.HashID(sess.ID)[:12], newTitle)
	return nil
}

func runSessionLast(cmd *cobra.Command, _ []string) error {
	event.SetNonInteractive(true)

	ctx, svc, cleanup, err := sessionSetup(cmd)
	if err != nil {
		return err
	}
	defer cleanup()

	event.SessionLastShown(sessionLastJSON)

	list, err := svc.sessions.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	if len(list) == 0 {
		return fmt.Errorf("no sessions found")
	}

	sess := list[0]

	msgs, err := svc.messages.List(ctx, sess.ID)
	if err != nil {
		return fmt.Errorf("failed to list messages: %w", err)
	}

	msgPtrs := messagePtrs(msgs)
	if sessionLastJSON {
		return outputSessionJSON(cmd.OutOrStdout(), sess, msgPtrs)
	}
	return outputSessionHuman(ctx, svc.cfg, sess, msgPtrs)
}

const (
	sessionOutputWidth     = 80
	sessionMaxContentWidth = 120
)

func isBrokenPipe(err error) bool {
	if err == nil {
		return false
	}
	// Check for syscall.EPIPE (broken pipe)
	if errors.Is(err, syscall.EPIPE) {
		return true
	}
	// Also check for "broken pipe" in the error message
	return strings.Contains(err.Error(), "broken pipe")
}

// sessionWriter returns a writer, cleanup function, and a bool indicating if a pager is used.
// When the content fits within the terminal (or stdout is not a TTY), it returns
// a colorprofile.Writer wrapping stdout. When content exceeds terminal height,
// it starts a pager process (respecting $PAGER, defaulting to "less -R").
func sessionWriter(ctx context.Context, contentHeight int) (io.Writer, func(), bool) {
	// Use NewWriter which automatically detects TTY and strips ANSI when redirected
	if runtime.GOOS == "windows" || !term.IsTerminal(os.Stdout.Fd()) {
		return colorprofile.NewWriter(os.Stdout, os.Environ()), func() {}, false
	}

	_, termHeight, err := term.GetSize(os.Stdout.Fd())
	if err != nil || contentHeight <= termHeight {
		return colorprofile.NewWriter(os.Stdout, os.Environ()), func() {}, false
	}

	// Detect color profile from stderr since stdout is piped to the pager.
	profile := colorprofile.Detect(os.Stderr, os.Environ())

	pager := os.Getenv("PAGER")
	if pager == "" {
		pager = "less -R"
	}

	parts := strings.Fields(pager)
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...) //nolint:gosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	pipe, err := cmd.StdinPipe()
	if err != nil {
		return colorprofile.NewWriter(os.Stdout, os.Environ()), func() {}, false
	}

	if err := cmd.Start(); err != nil {
		return colorprofile.NewWriter(os.Stdout, os.Environ()), func() {}, false
	}

	return &colorprofile.Writer{
		Forward: pipe,
		Profile: profile,
	}, func() {
		pipe.Close()
		_ = cmd.Wait()
	}, true
}
