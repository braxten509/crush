package dialog

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/secureentry"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestSecureEntryMaskedAndSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.env")
	require.NoError(t, os.WriteFile(path, []byte("KEY=%s\n"), 0o644))
	requests := make(chan *secureentry.Request, 1)
	detach := secureentry.Attach(func(r *secureentry.Request) { requests <- r })
	defer detach()
	status, err := secureentry.Open("session", secureentry.Spec{File: path, Label: "Example API key", Occurrence: 1})
	require.NoError(t, err)
	req := <-requests
	sty := styles.CharmtonePantera()
	d := NewSecureEntry(&common.Common{Styles: &sty}, req)
	const value = "dummy-private-12345"
	d.Handle(tea.PasteMsg{Content: value})
	for _, size := range [][2]int{{120, 30}, {70, 20}, {35, 15}, {15, 6}} {
		scr := uv.NewScreenBuffer(size[0], size[1])
		d.Draw(scr, scr.Bounds())
		out := ansi.Strip(scr.Render())
		require.NotContains(t, out, value)
		if size[0] >= 70 {
			require.Contains(t, out, "Secure entry")
			require.Contains(t, out, "••••")
		}
		if size[0] == 120 {
			t.Log("\n" + out)
		}
	}
	require.NotContains(t, fmt.Sprintf("%+v", d), value)
	done, cmd := d.Handle(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.False(t, done)
	require.Empty(t, d.value)
	require.NotNil(t, cmd)
	result := cmd().(SecureEntrySaved)
	require.NoError(t, result.Err)
	require.True(t, d.Saved(result))
	req.Finish(true)
	require.NotContains(t, fmt.Sprintf("%+v", result), value)
	require.Equal(t, "saved", <-status)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "KEY="+value+"\n", string(data))
}

func TestSecureEntryCancellationAndEditing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	require.NoError(t, os.WriteFile(path, []byte("%s"), 0o600))
	requests := make(chan *secureentry.Request, 1)
	detach := secureentry.Attach(func(r *secureentry.Request) { requests <- r })
	defer detach()
	status, err := secureentry.Open("session", secureentry.Spec{File: path, Occurrence: 1})
	require.NoError(t, err)
	sty := styles.CharmtonePantera()
	d := NewSecureEntry(&common.Common{Styles: &sty}, <-requests)
	d.Handle(tea.KeyPressMsg{Text: "abc", Code: 'a'})
	d.Handle(tea.KeyPressMsg{Code: tea.KeyLeft})
	d.Handle(tea.KeyPressMsg{Code: tea.KeyBackspace})
	require.Equal(t, "ac", string(d.value))
	d.Handle(tea.PasteMsg{Content: "\x1b[31msecret\n"})
	require.Equal(t, "ac", string(d.value))
	require.NotEmpty(t, d.errorText)
	d.Handle(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	require.Empty(t, d.value)
	d.Handle(tea.PasteMsg{Content: "discarded"})
	done, cmd := d.Handle(tea.KeyPressMsg{Code: tea.KeyEscape})
	require.True(t, done)
	require.Empty(t, d.value)
	cmd()
	require.Equal(t, "cancelled", <-status)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "%s", string(data))
}

func TestSecureEntrySaveFailureClearsInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	require.NoError(t, os.WriteFile(path, []byte("%s"), 0o600))
	requests := make(chan *secureentry.Request, 1)
	detach := secureentry.Attach(func(r *secureentry.Request) { requests <- r })
	defer detach()
	status, err := secureentry.Open("session", secureentry.Spec{File: path, Occurrence: 1})
	require.NoError(t, err)
	req := <-requests
	sty := styles.CharmtonePantera()
	d := NewSecureEntry(&common.Common{Styles: &sty}, req)
	d.Handle(tea.PasteMsg{Content: "dummy-failed-key"})
	require.NoError(t, os.WriteFile(path, []byte("changed"), 0o600))
	_, cmd := d.Handle(tea.KeyPressMsg{Code: tea.KeyEnter})
	result := cmd().(SecureEntrySaved)
	require.Error(t, result.Err)
	require.False(t, d.Saved(result))
	require.Empty(t, d.value)
	require.False(t, d.saving)
	require.NotContains(t, d.errorText, "dummy-failed-key")
	done, cmd := d.Handle(tea.KeyPressMsg{Code: tea.KeyEscape})
	require.True(t, done)
	cmd()
	require.Equal(t, "cancelled", <-status)
}
