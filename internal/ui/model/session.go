package model

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/diff"
	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/charmbracelet/x/ansi"
)

// loadSessionMsg is a message indicating that a session and its files have
// been loaded. seq identifies the load that produced it (zero applies
// unconditionally); prepared holds the chat items built off the UI thread,
// and is rebuilt on the spot when nil.
type loadSessionMsg struct {
	seq       uint64
	session   *session.Session
	files     []SessionFile
	readFiles []string
	messages  []message.Message
	prepared  *preparedTranscript
}

// lspFilePaths returns deduplicated file paths from both modified and read
// files for starting LSP servers.
func (msg loadSessionMsg) lspFilePaths() []string {
	seen := make(map[string]struct{}, len(msg.files)+len(msg.readFiles))
	paths := make([]string, 0, len(msg.files)+len(msg.readFiles))
	for _, f := range msg.files {
		p := f.LatestVersion.Path
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		paths = append(paths, p)
	}
	for _, p := range msg.readFiles {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		paths = append(paths, p)
	}
	return paths
}

// SessionFile tracks the first and latest versions of a file in a session,
// along with the total additions and deletions.
type SessionFile struct {
	FirstVersion  history.File
	LatestVersion history.File
	Additions     int
	Deletions     int
}

// loadSession loads the session along with its associated files and computes
// the diff statistics (additions and deletions) for each file in the session.
// The session, its files and its transcript are fetched concurrently off the
// UI thread, and the chat items (including nested agent tools, which need a
// fetch each) are built there too, so Update only swaps them in. It must run
// on the UI thread: it starts tracking the load (see sessionLoadState).
//
// The returned batch also reports the new current-session selection to
// the workspace so the server can update its per-client presence map.
// That report is fire-and-forget: errors are logged at debug and the
// UI never blocks on the call.
func (m *UI) loadSession(sessionID string) tea.Cmd {
	ctx, seq, switching := m.beginSessionLoad(sessionID)
	ws := m.com.Workspace
	sty := m.com.Styles
	canceled := m.sessionLoad.canceled
	load := func() tea.Msg {
		var (
			wg           sync.WaitGroup
			sess         session.Session
			sessErr      error
			sessionFiles []SessionFile
			filesErr     error
			readFiles    []string
			messages     []message.Message
			messagesErr  error
		)
		wg.Go(func() { sess, sessErr = ws.GetSession(ctx, sessionID) })
		wg.Go(func() { sessionFiles, filesErr = loadSessionFiles(ctx, ws, sessionID) })
		wg.Go(func() {
			var err error
			readFiles, err = ws.FileTrackerListReadFiles(ctx, sessionID)
			if err != nil {
				slog.Error("Failed to load read files for session", "error", err)
			}
		})
		// Read the transcript here, not in Update: a long session is tens
		// of megabytes and would freeze the event loop while it loads.
		wg.Go(func() { messages, messagesErr = ws.ListMessages(ctx, sessionID) })
		wg.Wait()
		if err := cmp.Or(sessErr, filesErr, messagesErr); err != nil {
			return sessionLoadFailedMsg{seq: seq, err: err}
		}

		return loadSessionMsg{
			seq:       seq,
			session:   &sess,
			files:     sessionFiles,
			readFiles: readFiles,
			messages:  messages,
			prepared:  prepareTranscript(ctx, ws, sty, ws.Config(), ws.WorkingDir(), messages, canceled),
		}
	}
	cmds := []tea.Cmd{load, m.reportCurrentSession(sessionID)}
	if switching {
		cmds = append(cmds, sessionLoadTick(seq))
	}
	return tea.Batch(cmds...)
}

// reportCurrentSession returns a fire-and-forget tea.Cmd that
// informs the workspace which session this client is currently
// viewing. Errors are logged at debug only; the call is a hint
// for server-side presence tracking, not correctness-critical
// state.
func (m *UI) reportCurrentSession(sessionID string) tea.Cmd {
	return func() tea.Msg {
		if err := m.com.Workspace.SetCurrentSession(context.Background(), sessionID); err != nil {
			slog.Debug("Failed to report current session", "session_id", sessionID, "error", err)
		}
		return nil
	}
}

func (m *UI) loadSessionFiles(sessionID string) ([]SessionFile, error) {
	return loadSessionFiles(context.Background(), m.com.Workspace, sessionID)
}

func loadSessionFiles(ctx context.Context, ws workspace.Workspace, sessionID string) ([]SessionFile, error) {
	files, err := ws.ListSessionHistory(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	filesByPath := make(map[string][]history.File)
	for _, f := range files {
		filesByPath[f.Path] = append(filesByPath[f.Path], f)
	}
	sessionFiles := make([]SessionFile, 0, len(filesByPath))
	for _, versions := range filesByPath {
		if len(versions) == 0 {
			continue
		}

		first := versions[0]
		last := versions[0]
		for _, v := range versions {
			if v.Version < first.Version {
				first = v
			}
			if v.Version > last.Version {
				last = v
			}
		}

		_, additions, deletions := diff.GenerateDiff(first.Content, last.Content, first.Path)

		sessionFiles = append(sessionFiles, SessionFile{
			FirstVersion:  first,
			LatestVersion: last,
			Additions:     additions,
			Deletions:     deletions,
		})
	}

	slices.SortFunc(sessionFiles, func(a, b SessionFile) int {
		if a.LatestVersion.UpdatedAt > b.LatestVersion.UpdatedAt {
			return -1
		}
		if a.LatestVersion.UpdatedAt < b.LatestVersion.UpdatedAt {
			return 1
		}
		return 0
	})
	return sessionFiles, nil
}

// handleFileEvent processes file change events and updates the session file
// list with new or updated file information.
func (m *UI) handleFileEvent(file history.File) tea.Cmd {
	if m.session == nil || file.SessionID != m.session.ID {
		return nil
	}

	return func() tea.Msg {
		sessionFiles, err := m.loadSessionFiles(m.session.ID)
		// could not load session files
		if err != nil {
			return util.NewErrorMsg(err)
		}

		return sessionFilesUpdatesMsg{
			sessionFiles: sessionFiles,
		}
	}
}

// filesInfo renders the modified files section for the sidebar, showing files
// with their addition/deletion counts.
func (m *UI) filesInfo(cwd string, width, maxItems int, isSection bool) string {
	t := m.com.Styles

	title := t.Files.SectionTitle.Render("Modified Files")
	if isSection {
		title = common.Section(t, "Modified Files", width)
	}
	list := t.Files.EmptyMessage.Render("None")
	var filesWithChanges []SessionFile
	for _, f := range m.sessionFiles {
		if f.Additions == 0 && f.Deletions == 0 {
			continue
		}
		filesWithChanges = append(filesWithChanges, f)
	}
	if len(filesWithChanges) > 0 {
		list = fileList(t, cwd, filesWithChanges, width, maxItems)
	}

	return lipgloss.NewStyle().Width(width).Render(fmt.Sprintf("%s\n\n%s", title, list))
}

// fileList renders a list of files with their diff statistics, truncating to
// maxItems and showing a "...and N more" message if needed.
func fileList(t *styles.Styles, cwd string, filesWithChanges []SessionFile, width, maxItems int) string {
	if maxItems <= 0 {
		return ""
	}
	var renderedFiles []string
	filesShown := 0

	for _, f := range filesWithChanges {
		// Skip files with no changes
		if filesShown >= maxItems {
			break
		}

		// Build stats string with colors
		var statusParts []string
		if f.Additions > 0 {
			statusParts = append(statusParts, t.Files.Additions.Render(fmt.Sprintf("+%d", f.Additions)))
		}
		if f.Deletions > 0 {
			statusParts = append(statusParts, t.Files.Deletions.Render(fmt.Sprintf("-%d", f.Deletions)))
		}
		extraContent := strings.Join(statusParts, " ")

		// Format file path
		filePath := f.FirstVersion.Path
		if rel, err := filepath.Rel(cwd, filePath); err == nil {
			filePath = rel
		}
		filePath = fsext.DirTrim(filePath, 2)
		suffix := ""
		if extraContent != "" {
			suffix = " " + extraContent
		}
		maxPathWidth := max(width-lipgloss.Width(suffix), 0)
		filePath = ansi.Truncate(filePath, maxPathWidth, "…")

		line := t.Files.Path.Render(filePath)
		if extraContent != "" {
			line = fmt.Sprintf("%s %s", line, extraContent)
		}

		renderedFiles = append(renderedFiles, line)
		filesShown++
	}

	if len(filesWithChanges) > maxItems {
		remaining := len(filesWithChanges) - maxItems
		renderedFiles = append(renderedFiles, t.Files.TruncationHint.Render(fmt.Sprintf("…and %d more", remaining)))
	}

	return lipgloss.JoinVertical(lipgloss.Left, renderedFiles...)
}

// startLSPs starts LSP servers for the given file paths.
func (m *UI) startLSPs(paths []string) tea.Cmd {
	if len(paths) == 0 {
		return nil
	}

	return func() tea.Msg {
		ctx := context.Background()
		for _, path := range paths {
			m.com.Workspace.LSPStart(ctx, path)
		}
		return nil
	}
}
