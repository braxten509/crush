package chat

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func copyAssistant(t *testing.T, source string, width int) (*AssistantMessageItem, string) {
	t.Helper()
	sty := styles.CharmtonePantera()
	m := &message.Message{ID: "copy", Role: message.Assistant, Parts: []message.ContentPart{
		message.TextContent{Text: source}, message.Finish{Reason: message.FinishReasonEndTurn},
	}}
	item := NewAssistantMessageItem(&sty, m).(*AssistantMessageItem)
	return item, item.RawRender(width)
}

func TestCopyAssistantCodePreservesSource(t *testing.T) {
	cases := []string{
		"bash /home/example/Documents/forecaster-ui-public/scripts/deployment/install-worker-key.sh /home/example/.ssh/forecaster-scraper-deploy.ABCDEFGH/id_ed25519",
		"bash /home/example/Documents/forecaster-ui-public/scripts/deployment/install-worker-key.sh \\\n  /home/example/.ssh/forecaster-scraper-deploy.ABCDEFGH/id_ed25519",
		"printf '%s\\n' 'a   b'\n    printf '%s\\n' 'second line'\n\n\tprintf '%s\\n' 'tabbed line'",
		"printf '%s' '路径/é/👩‍💻/" + strings.Repeat("long", 30) + "'",
		strings.Repeat("x", 50) + "\n" + strings.Repeat("y", 50),
		"s h",
		"    s  h",
		"    \n",
	}
	for _, width := range []int{32, 60, 100, 160} {
		for i, code := range cases {
			t.Run(fmt.Sprintf("width%d/case%d", width, i), func(t *testing.T) {
				source := "```sh\n" + code + "\n```"
				item, rendered := copyAssistant(t, source, width)
				got, ok := list.HighlightSource(item.CopySource(), rendered,
					uv.Rect(0, 0, width, lipgloss.Height(rendered)), 0, 0, -1, -1)
				require.True(t, ok, "rendered: %s", ansi.Strip(rendered))
				require.Equal(t, code+"\n", got)
			})
		}
	}
}

func TestCopyAssistantPartialWrappedPath(t *testing.T) {
	path := "/home/example/" + strings.Repeat("segment/", 15) + "file.txt"
	item, rendered := copyAssistant(t, "Run this:\n\n```sh\ncat "+path+"\n```\n\nThen continue.", 48)
	lines := strings.Split(ansi.Strip(rendered), "\n")
	startLine, startCol, endLine, endCol := -1, -1, -1, -1
	for y, line := range lines {
		if x := strings.Index(line, "/home/"); x >= 0 {
			startLine, startCol = y, ansi.StringWidth(line[:x])
		}
		if startLine >= 0 && y > startLine && strings.TrimSpace(line) == "" {
			endLine, endCol = y-1, ansi.StringWidth(strings.TrimRight(lines[y-1], " "))
			break
		}
	}
	require.Greater(t, endLine, startLine)
	// Exercise the highlighted render used by Chat.HighlightContent as well.
	item.SetHighlight(startLine, startCol+MessageLeftPaddingTotal, endLine, endCol+MessageLeftPaddingTotal)
	rendered = item.RawRender(48)
	got, ok := list.HighlightSource(item.CopySource(), rendered,
		uv.Rect(0, 0, 48, lipgloss.Height(rendered)), startLine, startCol, endLine, endCol)
	require.True(t, ok)
	require.Equal(t, path, got)
}

func TestCopyAssistantRepeatedCommandsKeepTheirWhitespace(t *testing.T) {
	source := "```sh\necho one two\necho one   two\n```"
	item, rendered := copyAssistant(t, source, 80)
	for y, row := range strings.Split(ansi.Strip(rendered), "\n") {
		if strings.Contains(row, "one   two") {
			got, ok := list.HighlightSource(item.CopySource(), rendered,
				uv.Rect(0, 0, 80, lipgloss.Height(rendered)), y, 0, y, 80)
			require.True(t, ok)
			require.Equal(t, "echo one   two", got)
			return
		}
	}
	t.Fatal("second command not rendered")
}

func TestCopySourceExcludesHiddenMarkup(t *testing.T) {
	source := "alpha<!-- secret -->omega\n\n" + common.PlanStartMarker + "\n\n```sh\ns h\n```\n\n" + common.PlanReadyMarker
	item, _ := copyAssistant(t, source, 60)
	blocks := item.CopySource()
	require.Equal(t, []string{"alphaomega", "s h\n"}, blocks)
	got, ok := list.HighlightSource(blocks, "alphaomega", uv.Rect(0, 0, 60, 1), 0, 0, 0, 10)
	require.True(t, ok)
	require.Equal(t, "alphaomega", got)
}

func TestCopyCodeInHeavilyFormattedReply(t *testing.T) {
	var source strings.Builder
	var command string
	for i := 0; i < 160; i++ {
		code := fmt.Sprintf("command-%03d /home/example/", i) + strings.Repeat("long-path/", 12) + "end"
		fmt.Fprintf(&source, "## **Section %03d**\n\n```sh\n%s\n```\n\n", i, code)
		if i == 80 {
			command = code
		}
	}
	item, rendered := copyAssistant(t, source.String(), 64)
	lines := strings.Split(ansi.Strip(rendered), "\n")
	start, end := -1, -1
	for y, line := range lines {
		if strings.Contains(line, "command-080") {
			start = y
		}
		if start >= 0 && y > start && strings.TrimSpace(line) == "" {
			end = y
			break
		}
	}
	require.Greater(t, end, start)
	got, ok := list.HighlightSource(item.CopySource(), rendered, uv.Rect(0, 0, 64, len(lines)), start, 0, end, 0)
	require.True(t, ok)
	require.Equal(t, command+"\n", got)
}

// Inline code renders without backticks or padding; a copy of the reply must
// still restore them from the message source.
func TestCopyAssistantInlineCodeKeepsBackticks(t *testing.T) {
	source := "Started a wait (PID `1438360`) with `sleep 10` and `nohup`."
	for _, width := range []int{20, 40, 100} {
		item, rendered := copyAssistant(t, source, width)
		require.NotContains(t, ansi.Strip(rendered), "`")
		area := uv.Rect(0, 0, width, lipgloss.Height(rendered))
		got, ok := list.HighlightSource(item.CopySource(), rendered, area, 0, 0, -1, -1)
		require.True(t, ok, "rendered: %s", ansi.Strip(rendered))
		require.Equal(t, source, got, "width %d", width)
	}

	// Selecting just the code text takes its backticks; selecting inside it
	// does not invent any.
	item, rendered := copyAssistant(t, source, 100)
	area := uv.Rect(0, 0, 100, lipgloss.Height(rendered))
	for y, line := range strings.Split(ansi.Strip(rendered), "\n") {
		x := strings.Index(line, "1438360")
		if x < 0 {
			continue
		}
		col := ansi.StringWidth(line[:x])
		got, ok := list.HighlightSource(item.CopySource(), rendered, area, y, col, y, col+7)
		require.True(t, ok)
		require.Equal(t, "`1438360`", got)
		got, ok = list.HighlightSource(item.CopySource(), rendered, area, y, col+1, y, col+6)
		require.True(t, ok)
		require.Equal(t, "43836", got)
		return
	}
	t.Fatal("code text not rendered")
}
