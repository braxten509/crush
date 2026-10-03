package dialog

import (
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/secureentry"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// The secure entry dialog shows only its title, the field and the keys: the
// label is the title (never repeated below it), and no file path,
// placeholder or privacy notes.
func TestSecureEntryShowsOnlyFieldAndKeys(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	for label, title := range map[string]string{"": "Secure entry", "Service API key": "Service API key"} {
		s := NewSecureEntry(&common.Common{Styles: &sty}, &secureentry.Request{Spec: secureentry.Spec{
			File: "/dummy/state/secret.txt", Label: label, Placeholder: "%s", Occurrence: 1,
		}})
		scr := uv.NewScreenBuffer(100, 20)
		s.Draw(scr, scr.Bounds())
		view := ansi.Strip(scr.Render())

		require.Equal(t, 1, strings.Count(view, title), "title once:\n%s", view)
		require.Contains(t, view, "Enter secret…")
		require.Contains(t, view, "enter save")
		for _, gone := range []string{"/dummy/state", "Replace:", "occurrence", "owner access", "cancelled status"} {
			require.NotContains(t, view, gone)
		}
	}
}
