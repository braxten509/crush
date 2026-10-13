package model

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/charmbracelet/crush/internal/ui/attachments"
	"github.com/charmbracelet/crush/internal/ui/completions"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

// newThemeUI returns a UI whose theme can be applied, with the user theme
// directory in a temporary global config directory. Tests using it must
// not call t.Parallel, as it sets the environment.
func newThemeUI(t *testing.T) (*UI, *cmdStateWorkspace, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CRUSH_GLOBAL_CONFIG", dir)
	u, ws := newCmdStateUI(t)
	u.header = newHeader(u.com)
	u.mention.popup = completions.New(u.com.Styles.Completions.Normal, u.com.Styles.Completions.Focused, u.com.Styles.Completions.Match)
	u.attachments = attachments.New(attachments.NewRenderer(u.com.Styles.Attachments), attachments.Keymap{})
	return u, ws, filepath.Join(dir, "themes")
}

// TestThemePreviewLoadsOffLoop pins that previews load in a command, that
// only the latest one is shown, and that one landing after the preview
// ended is dropped.
func TestThemePreviewLoadsOffLoop(t *testing.T) {
	u, _, _ := newThemeUI(t)
	original := *u.com.Styles
	names := otherThemes(t, original)
	second, err := styles.LoadTheme(names[1])
	require.NoError(t, err)

	older := collectMsgs(u.handleDialogAction(dialog.ActionPreviewTheme{Theme: names[0]}))
	newer := collectMsgs(u.handleDialogAction(dialog.ActionPreviewTheme{Theme: names[1]}))
	require.Nil(t, u.theme.preview, "the preview waits for its theme to load")

	done, ok := findMsg[workDoneMsg](newer)
	require.True(t, ok)
	u.Update(done)
	require.Equal(t, second, *u.com.Styles)
	require.NotNil(t, u.theme.preview)

	// The older load landing late does not replace the newer preview.
	done, ok = findMsg[workDoneMsg](older)
	require.True(t, ok)
	u.Update(done)
	require.Equal(t, second, *u.com.Styles)

	// A load still in flight when the preview is reverted is dropped.
	late := collectMsgs(u.handleDialogAction(dialog.ActionPreviewTheme{Theme: names[0]}))
	u.handleDialogAction(dialog.ActionRevertThemePreview{})
	require.Equal(t, original, *u.com.Styles)
	done, ok = findMsg[workDoneMsg](late)
	require.True(t, ok)
	u.Update(done)
	require.Equal(t, original, *u.com.Styles)
	require.Nil(t, u.theme.preview)
}

// otherThemes returns two built-in themes that differ from current and
// from each other.
func otherThemes(t *testing.T, current styles.Styles) []string {
	t.Helper()
	var names []string
	var seen []styles.Styles
	for _, name := range styles.BuiltinThemeNames() {
		s, err := styles.LoadTheme(name)
		require.NoError(t, err)
		if reflect.DeepEqual(s, current) || slices.ContainsFunc(seen, func(o styles.Styles) bool { return reflect.DeepEqual(s, o) }) {
			continue
		}
		names, seen = append(names, name), append(seen, s)
		if len(names) == 2 {
			return names
		}
	}
	t.Fatal("the test needs two themes that differ from the current one")
	return nil
}

// TestThemeFileWritesRunOffLoop pins that creating, saving, renaming and
// deleting a user theme touch the theme files only when the command runs,
// and that the dialogs follow once Update applied the result.
func TestThemeFileWritesRunOffLoop(t *testing.T) {
	u, ws, dir := newThemeUI(t)
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name+".json"))
		return err == nil
	}

	u.dialog.OpenDialog(idDialog{id: dialog.ThemeNewID})
	cmd := u.handleDialogAction(dialog.ActionCreateTheme{Name: "mine"})
	require.False(t, exists("mine"), "the file must not be written on the Update goroutine")
	_, written := ws.set("options.tui.active_theme")
	require.False(t, written)
	runWorkDone(u, cmd)
	require.True(t, exists("mine"))
	active, _ := ws.set("options.tui.active_theme")
	require.Equal(t, "mine", active)
	require.False(t, u.dialog.ContainsDialog(dialog.ThemeNewID))
	require.True(t, u.dialog.ContainsDialog(dialog.ThemeEditorID))

	require.NoError(t, os.Remove(filepath.Join(dir, "mine.json")))
	cmd = u.handleDialogAction(dialog.ActionSaveThemePalette{Name: "mine", Base: defaultThemeName})
	require.False(t, exists("mine"))
	require.True(t, u.dialog.ContainsDialog(dialog.ThemeEditorID))
	runWorkDone(u, cmd)
	require.True(t, exists("mine"))
	require.False(t, u.dialog.ContainsDialog(dialog.ThemeEditorID))

	// The stub config still names the default theme, so neither the
	// rename nor the delete rewrites the config.
	cmd = u.handleDialogAction(dialog.ActionRenameTheme{OldName: "mine", NewName: "yours"})
	require.True(t, exists("mine"))
	runWorkDone(u, cmd)
	require.False(t, exists("mine"))
	require.True(t, exists("yours"))
	require.True(t, u.dialog.ContainsDialog(dialog.ThemeID))

	cmd = u.handleDialogAction(dialog.ActionDeleteTheme{Name: "yours"})
	require.True(t, exists("yours"))
	runWorkDone(u, cmd)
	require.False(t, exists("yours"))
}

// TestSwitchThemeLoadsOffLoop pins that switching themes loads the theme
// and writes the config when the command runs, and applies it on Update.
func TestSwitchThemeLoadsOffLoop(t *testing.T) {
	u, ws, _ := newThemeUI(t)
	before := *u.com.Styles
	names := otherThemes(t, before)
	want, err := styles.LoadTheme(names[1])
	require.NoError(t, err)
	u.dialog.OpenDialog(idDialog{id: dialog.ThemeID})

	cmd := u.handleDialogAction(dialog.ActionSwitchTheme{Theme: names[1]})
	require.Equal(t, before, *u.com.Styles)
	runWorkDone(u, cmd)
	require.Equal(t, want, *u.com.Styles)
	active, _ := ws.set("options.tui.active_theme")
	require.Equal(t, names[1], active)
	require.False(t, u.dialog.ContainsDialog(dialog.ThemeID))
	require.True(t, u.theme.userSelected)
}
