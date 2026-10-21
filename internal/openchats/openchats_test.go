//go:build !windows

package openchats

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// otherApp signs a running child process in as another app, the way the
// desktop or a second terminal would, and returns a function that ends it.
func otherApp(t *testing.T, l List) (end func()) {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	app := App{PID: cmd.Process.Pid, Start: processStart(cmd.Process.Pid), Kind: "desktop"}
	require.NoError(t, l.change(func(f *file) { f.Apps = append(f.Apps, app) }))
	return func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }
}

func ids(chats []Chat) []string {
	out := []string{}
	for _, c := range chats {
		out = append(out, c.ID)
	}
	return out
}

// The chats stay while either app runs and close once both are closed
// (owner, 2026-10-20: "IFF both apps are closed, those chats close").
func TestChatsCloseOnlyWhenEveryAppIsClosed(t *testing.T) {
	l := At(filepath.Join(t.TempDir(), "open-chats.json"))
	endDesktop := otherApp(t, l)
	require.NoError(t, l.Add(Chat{ID: "a", Dir: "/p"}))

	leave, err := l.Join("terminal")
	require.NoError(t, err)
	require.Equal(t, []string{"a"}, ids(l.Chats()), "joining while the desktop runs keeps its chats")
	require.NoError(t, l.Add(Chat{ID: "b", Dir: "/q"}))

	leave()
	require.Equal(t, []string{"a", "b"}, ids(l.Chats()), "the terminal quitting keeps them: the desktop still runs")

	endDesktop()
	require.Empty(t, l.Chats(), "with no app running (the desktop crashed) the list is empty")
	leave, err = l.Join("terminal")
	require.NoError(t, err)
	require.Empty(t, l.Chats(), "and the next app starts with none")
	require.NoError(t, l.Add(Chat{ID: "c", Dir: "/p"}))
	leave()
	b, err := os.ReadFile(l.path)
	require.NoError(t, err)
	var f file
	require.NoError(t, json.Unmarshal(b, &f))
	require.Empty(t, f.Chats, "the last app leaving empties the file")
	require.Empty(t, f.Apps)
}

func TestAddRemoveAndSwapKeepTheOrder(t *testing.T) {
	l := At(filepath.Join(t.TempDir(), "open-chats.json"))
	leave, err := l.Join("terminal")
	require.NoError(t, err)
	defer leave()
	for _, id := range []string{"a", "b", "c"} {
		require.NoError(t, l.Add(Chat{ID: id, Dir: "/p", Title: "New Session"}))
	}
	require.NoError(t, l.Add(Chat{ID: "b", Title: "Renamed"}))
	require.Equal(t, []string{"a", "b", "c"}, ids(l.Chats()), "adding a listed chat keeps its place")
	require.Equal(t, "Renamed", l.Chats()[1].Title)
	require.Equal(t, "/p", l.Chats()[1].Dir, "an empty folder leaves the old one")

	require.NoError(t, l.Swap("b", Chat{ID: "d", Dir: "/q"}))
	require.Equal(t, []string{"a", "d", "c"}, ids(l.Chats()), "a row's new chat takes the old one's place")
	require.NoError(t, l.Swap("a", Chat{ID: "c", Dir: "/p"}))
	require.Equal(t, []string{"c", "d"}, ids(l.Chats()), "swapping to a listed chat leaves it listed once")

	require.NoError(t, l.Remove("d"))
	require.Equal(t, []string{"c"}, ids(l.Chats()))
}

// The terminal stays quiet only for folders a running desktop hears
// (owner, 2026-10-20: "only ring ONCE - from the desktop one").
func TestDesktopHearsOnlyItsFoldersWhileItRuns(t *testing.T) {
	l := At(filepath.Join(t.TempDir(), "open-chats.json"))
	heard, other := t.TempDir(), t.TempDir()
	cmd := exec.Command("sleep", "60")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	desktop := App{PID: cmd.Process.Pid, Start: processStart(cmd.Process.Pid), Kind: "desktop", Dirs: []string{heard + "/"}}
	require.NoError(t, l.change(func(f *file) { f.Apps = append(f.Apps, desktop) }))
	require.True(t, l.DesktopHears(heard))
	require.False(t, l.DesktopHears(other))
	require.True(t, l.DesktopOpen())
	// A terminal listing the same folder is not a desktop.
	require.NoError(t, l.change(func(f *file) { f.Apps[0].Kind = "terminal" }))
	require.False(t, l.DesktopHears(heard))
	require.False(t, l.DesktopOpen())
	require.NoError(t, l.change(func(f *file) { f.Apps[0].Kind = "desktop" }))
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	require.False(t, l.DesktopHears(heard), "a closed desktop rings for nothing")
	require.False(t, l.DesktopOpen())
}
