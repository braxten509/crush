package common

import (
	"testing"

	"charm.land/glamour/v2"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// TestRenderedInlineCodeIsPlainColoredText guards the inline code look:
// codespans render as colored text only, with no backticks, no padding and
// no background chip (copies get their backticks back from the message
// source, see list.HighlightSource).
func TestRenderedInlineCodeIsPlainColoredText(t *testing.T) {
	t.Parallel()

	sty := styles.CharmtonePantera()

	render := func(t *testing.T, r *glamour.TermRenderer, src string) string {
		t.Helper()
		mu := LockMarkdownRenderer(r)
		mu.Lock()
		defer mu.Unlock()
		rendered, err := r.Render(src)
		require.NoError(t, err)
		return rendered
	}

	for name, r := range map[string]*glamour.TermRenderer{
		"markdown": MarkdownRenderer(&sty, 80),
		"quiet":    QuietMarkdownRenderer(&sty, 80),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rendered := render(t, r, `message "this is `+"`code`"+`" ok`)
			actual := ansi.Strip(rendered)

			require.NotContains(t, actual, "`", "codespan backticks must not render on screen")
			require.Contains(t, actual, `this is code" ok`, "codespan must render without padding")
			require.Nil(t, sty.Markdown.Code.BackgroundColor, "inline code must not have a background")
			require.Nil(t, sty.QuietMarkdown.Code.BackgroundColor, "quiet inline code must not have a background")
		})
	}
}
