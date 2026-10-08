package sessionhost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/home"
	"github.com/stretchr/testify/require"
)

// folders makes a folder with the given folders and files in it.
func folders(t *testing.T, dirs []string, files ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range dirs {
		require.NoError(t, os.MkdirAll(filepath.Join(root, d), 0o755))
	}
	for _, f := range files {
		require.NoError(t, os.WriteFile(filepath.Join(root, f), nil, 0o600))
	}
	return root
}

func key(h *Host, code rune) {
	h.Update(tea.KeyPressMsg(tea.Key{Code: code}))
}

func typeText(h *Host, text string) {
	for _, r := range text {
		h.Update(tea.KeyPressMsg(tea.Key{Code: r, Text: string(r)}))
	}
}

func clearInput(h *Host) {
	h.Update(tea.KeyPressMsg(tea.Key{Code: 'u', Mod: tea.ModCtrl}))
}

func names(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = filepath.Base(p)
	}
	return out
}

// pickerHost returns a host showing one session in dir, whose new sessions
// are recorded in started instead of running Crush.
func pickerHost(t *testing.T, dir string, recent ...string) (*Host, *[]string) {
	t.Helper()
	h := newTestHost(t, 120, 30, Status{Title: "First", Dir: dir})
	var started []string
	h.opts.Exe = "/bin/sh"
	h.opts.NewArgs = func(dir string) []string {
		started = append(started, dir)
		return []string{"-c", "exit 0"}
	}
	h.opts.Recent = func() []string { return recent }
	h.send = func(tea.Msg) {}
	return h, &started
}

func TestPickerOpensAtShownFolderListingItsFolders(t *testing.T) {
	root := folders(t, []string{"alpha", "beta", ".hidden"}, "notes.txt")
	h, _ := pickerHost(t, root)
	press(h, "alt+n")
	require.NotNil(t, h.picker)
	require.Equal(t, withSlash(home.Short(root)), string(h.picker.input))
	// Files and hidden folders don't show.
	require.Equal(t, []string{"alpha", "beta"}, names(h.picker.matches))
	require.Contains(t, screenText(h), "New session in…")
}

func TestPickerMatchesIgnoringCaseAndHidesDotFolders(t *testing.T) {
	root := folders(t, []string{"Alpha", "alpine", "beta", ".config"}, "alps.txt")
	h, _ := pickerHost(t, root)
	press(h, "alt+n")
	typeText(h, "al")
	require.Equal(t, []string{"Alpha", "alpine"}, names(h.picker.matches))
	key(h, tea.KeyBackspace)
	key(h, tea.KeyBackspace)
	typeText(h, ".")
	require.Equal(t, []string{".config"}, names(h.picker.matches))
}

func TestPickerShowsAtMostFiveMatches(t *testing.T) {
	root := folders(t, []string{"a1", "a2", "a3", "a4", "a5", "a6", "a7"})
	h, _ := pickerHost(t, root)
	press(h, "alt+n")
	require.Len(t, h.picker.matches, pickerRows)
}

func TestPickerExpandsHomeAndRelativePaths(t *testing.T) {
	p := &picker{base: "/work/project"}
	require.Equal(t, home.Dir(), p.expand("~"))
	require.Equal(t, filepath.Join(home.Dir(), "dev"), filepath.Clean(p.expand("~/dev")))
	require.Equal(t, "/work/project/sub", p.expand("sub"))
	require.Equal(t, "/etc", p.expand("/etc"))
}

func TestPickerTabGoesIntoPickedFolder(t *testing.T) {
	root := folders(t, []string{"alpha/inner", "beta"})
	h, _ := pickerHost(t, root)
	press(h, "alt+n")
	key(h, tea.KeyDown)
	require.Equal(t, 0, h.picker.sel)
	key(h, tea.KeyTab)
	require.Equal(t, withSlash(home.Short(filepath.Join(root, "alpha"))), string(h.picker.input))
	require.Equal(t, []string{"inner"}, names(h.picker.matches))
	require.Equal(t, -1, h.picker.sel)

	// With nothing picked, Tab takes the first match.
	typeText(h, "in")
	key(h, tea.KeyTab)
	require.Equal(t, withSlash(home.Short(filepath.Join(root, "alpha", "inner"))), string(h.picker.input))
}

func TestPickerArrowsWrapThroughTypedPath(t *testing.T) {
	root := folders(t, []string{"a", "b"})
	h, _ := pickerHost(t, root)
	press(h, "alt+n")
	key(h, tea.KeyUp)
	require.Equal(t, 1, h.picker.sel)
	key(h, tea.KeyDown)
	require.Equal(t, -1, h.picker.sel)
	key(h, tea.KeyDown)
	require.Equal(t, 0, h.picker.sel)
}

func TestPickerEnterStartsInPickedOrTypedFolder(t *testing.T) {
	root := folders(t, []string{"alpha", "beta"})
	h, started := pickerHost(t, root)

	press(h, "alt+n")
	key(h, tea.KeyDown)
	key(h, tea.KeyDown)
	press(h, "enter")
	require.Nil(t, h.picker)
	require.Equal(t, []string{filepath.Join(root, "beta")}, *started)
	require.Len(t, h.sessions, 2)

	// A typed path, relative to the shown session's folder.
	h.show(0)
	press(h, "alt+n")
	clearInput(h)
	typeText(h, "alpha")
	press(h, "enter")
	require.Equal(t, filepath.Join(root, "alpha"), (*started)[1])
}

func TestPickerSaysWhenThereIsNoSuchFolder(t *testing.T) {
	root := folders(t, nil, "file.txt")
	h, started := pickerHost(t, root)
	press(h, "alt+n")
	typeText(h, "missing")
	press(h, "enter")
	require.NotNil(t, h.picker, "the box stays open")
	require.Empty(t, *started)
	require.Contains(t, h.picker.err, "No folder at "+home.Short(filepath.Join(root, "missing")))
	// Long paths keep their end in view.
	require.Contains(t, screenText(h), filepath.Base(root)+"/missing")

	// A file isn't a folder either; typing clears the message.
	clearInput(h)
	typeText(h, "file.txt")
	require.Empty(t, h.picker.err)
	press(h, "enter")
	require.Contains(t, h.picker.err, "No folder at")
}

func TestPickerEscClosesWithoutStarting(t *testing.T) {
	h, started := pickerHost(t, t.TempDir())
	press(h, "alt+n")
	press(h, "esc")
	require.Nil(t, h.picker)
	require.Empty(t, *started)
}

func TestPickerKeysDontReachTheSession(t *testing.T) {
	h, _ := pickerHost(t, t.TempDir())
	press(h, "alt+n")
	typeText(h, "xyz")
	press(h, "alt+s")
	require.False(t, h.prefs.ListClosed, "host keys wait while the box is open")
	require.True(t, strings.HasSuffix(string(h.picker.input), "xyz"))
}

func TestPickerRecentSkipsMissingAndRepeatedFolders(t *testing.T) {
	root := folders(t, []string{"r1", "r2", "r3", "r4", "r5", "r6"})
	r := func(n string) string { return filepath.Join(root, n) }
	p := newPicker(root, []string{r("r1"), r("gone"), r("r1"), r("r2"), r("r3"), r("r4"), r("r5"), r("r6")})
	require.Equal(t, []string{"r1", "r2", "r3", "r4", "r5"}, names(p.recent))
}

func TestPickerShowsRecentBelowMatches(t *testing.T) {
	root := folders(t, []string{"here", "elsewhere/project"})
	recent := filepath.Join(root, "elsewhere", "project")
	h, started := pickerHost(t, filepath.Join(root, "here"), recent)
	press(h, "alt+n")
	text := screenText(h)
	require.Contains(t, text, "Recent")
	require.Contains(t, text, "/elsewhere/project")
	// No matches inside "here", so the first row is the recent folder.
	key(h, tea.KeyDown)
	press(h, "enter")
	require.Equal(t, []string{recent}, *started)
}

func TestNewSessionClickOpensPickerAndRowClicksPickThenStart(t *testing.T) {
	root := folders(t, []string{"alpha", "beta"})
	h, started := pickerHost(t, root)
	h.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: newSessionColumn, Y: newSessionRow})
	require.NotNil(t, h.picker)

	_, inner := pickerBox(h.sessionArea())
	row := tea.MouseClickMsg{Button: tea.MouseLeft, X: inner.Min.X + 2, Y: inner.Min.Y + pickerMatchLine + 1}
	h.Update(row)
	require.Equal(t, 1, h.picker.sel)
	require.Empty(t, *started)
	h.Update(row)
	require.Equal(t, []string{filepath.Join(root, "beta")}, *started)
}

func TestPickerClickOutsideCloses(t *testing.T) {
	h, started := pickerHost(t, t.TempDir())
	press(h, "alt+n")
	h.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: h.width - 1, Y: h.height - 1})
	require.Nil(t, h.picker)
	require.Empty(t, *started)
}

func TestPickerPasteAndWordDelete(t *testing.T) {
	h, _ := pickerHost(t, t.TempDir())
	press(h, "alt+n")
	clearInput(h)
	h.Update(tea.PasteMsg{Content: "/tmp/one/two\n"})
	require.Equal(t, "/tmp/one/two", string(h.picker.input))
	h.Update(tea.KeyPressMsg(tea.Key{Code: 'w', Mod: tea.ModCtrl}))
	require.Equal(t, "/tmp/one/", string(h.picker.input))
}
