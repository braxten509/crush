package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// A skill notice is one line lined up with replies, without the user band.
func TestSystemNoticeIsOneLine(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	for text, mark := range map[string]string{
		"teach turned on by the agent":  "◐",
		"teach turned off by the agent": "○",
	} {
		msg := &message.Message{ID: text, Role: message.System, Parts: []message.ContentPart{message.TextContent{Text: text}}}
		items := ExtractMessageItems(&sty, msg, nil, "")
		require.Len(t, items, 1)
		out := ansi.Strip(items[0].Render(60))
		require.Equal(t, 1, strings.Count(out, "\n")+1, "one line: %q", out)
		require.Contains(t, out, mark+" "+text)
		require.NotContains(t, out, "›", "not shown as something the user typed")
	}
}
