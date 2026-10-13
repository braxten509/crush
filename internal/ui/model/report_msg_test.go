package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/ui/attachments"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/stretchr/testify/require"
)

// A command must deliver a message: a tea.Cmd returned in its place is
// dropped by the runtime, so the warning or error it would report never
// reaches the status bar. These tests pin the reports that used to be lost.

func requireInfoMsg(t *testing.T, msg tea.Msg, typ util.InfoType) util.InfoMsg {
	t.Helper()
	info, ok := msg.(util.InfoMsg)
	require.True(t, ok, "got %T, want util.InfoMsg", msg)
	require.Equal(t, typ, info.Type)
	return info
}

func TestEditorFileMsgReportsReadError(t *testing.T) {
	t.Parallel()

	msg := editorFileMsg(filepath.Join(t.TempDir(), "missing.md"))
	requireInfoMsg(t, msg, util.InfoTypeError)
}

func TestHandleFilePathPasteReportsProblems(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	dir := t.TempDir()
	info := requireInfoMsg(t, u.handleFilePathPaste(dir)(), util.InfoTypeWarn)
	require.Contains(t, info.Msg, "directory")

	info = requireInfoMsg(t, u.handleFilePathPaste(filepath.Join(dir, "missing.png"))(), util.InfoTypeError)
	require.NotEmpty(t, info.Msg)

	big := filepath.Join(dir, "big.png")
	f, err := os.Create(big)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(6*1024*1024))
	require.NoError(t, f.Close())
	info = requireInfoMsg(t, u.handleFilePathPaste(big)(), util.InfoTypeWarn)
	require.Contains(t, info.Msg, "too big")
}

func TestHugePasteReportsSize(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	u.attachments = attachments.New(nil, attachments.Keymap{})
	u.dialog = dialog.NewOverlay()
	cmd := u.handlePasteMsg(tea.PasteMsg{Content: strings.Repeat("x\n", 3*1024*1024)})
	require.NotNil(t, cmd)
	info := requireInfoMsg(t, cmd(), util.InfoTypeWarn)
	require.Contains(t, info.Msg, "too big")
}
