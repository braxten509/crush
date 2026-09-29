package model

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/secureentry"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/stretchr/testify/require"
)

func TestSecureInputNeverReachesComposer(t *testing.T) {
	t.Parallel()
	m := newTestUI()
	m.textarea.SetValue("existing draft")
	m.Update(&secureentry.Request{Spec: secureentry.Spec{File: "/dummy/keys.env", Label: "Test key"}})
	m.Update(tea.PasteMsg{Content: "dummy-pasted-key"})
	m.Update(tea.KeyPressMsg{Code: 'a', Text: "dummy-typed-key"})
	m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	require.Equal(t, "existing draft", m.textarea.Value())
	require.Empty(t, m.attachments)
	require.NotNil(t, m.secureDialog)
	// A clipboard result that arrives after closure is wiped, never pasted.
	m.secureDialog = nil
	value := []byte("dummy-late-key")
	m.Update(dialog.SecureEntryPaste{Value: value})
	require.Equal(t, make([]byte, len(value)), value)
	require.Equal(t, "existing draft", m.textarea.Value())
}
