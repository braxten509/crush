package model

import (
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/util"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

func TestLinkTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "A file #1%.txt")
	require.NoError(t, os.WriteFile(path, []byte("test"), 0600))
	fileURL := (&url.URL{Scheme: "file", Path: path}).String()
	for _, destination := range []string{fileURL, fileURL + ":12:3", fileURL + "#L12", "A%20file%20%231%25.txt"} {
		got, err := linkTarget(destination, dir)
		require.NoError(t, err, destination)
		require.Equal(t, fileURL, got)
	}
	for _, destination := range []string{dir, "./", (&url.URL{Scheme: "file", Path: dir}).String()} {
		got, err := linkTarget(destination, dir)
		require.NoError(t, err)
		require.Equal(t, (&url.URL{Scheme: "file", Path: dir}).String(), got)
	}
	for _, destination := range []string{"https://example.com/a?q=b#c", "mailto:a@example.com"} {
		got, err := linkTarget(destination, dir)
		require.NoError(t, err)
		require.Equal(t, destination, got)
	}
	for _, destination := range []string{"javascript:alert(1)", "file://another-computer/etc/passwd", "#heading", "./missing.txt"} {
		_, err := linkTarget(destination, dir)
		require.Error(t, err, destination)
	}
	// Existing colon-containing names win over editor line suffixes.
	colon := filepath.Join(dir, "report:12")
	require.NoError(t, os.WriteFile(colon, nil, 0600))
	got, err := linkTarget(colon, dir)
	require.NoError(t, err)
	require.Equal(t, (&url.URL{Scheme: "file", Path: colon}).String(), got)
}

func linkTestChat(t *testing.T) (*Chat, int, int) {
	t.Helper()
	u := newTestUI()
	u.chat.SetMessages(chat.NewAssistantMessageItem(u.com.Styles, &message.Message{
		ID: "link-test", Role: message.Assistant,
		Parts: []message.ContentPart{message.TextContent{Text: "Before 👩‍💻 [Open the folder](/tmp) after."}, message.Finish{Reason: message.FinishReasonEndTurn}},
	}))
	u.updateLayoutAndSize()
	c := u.chat
	buf := uv.NewScreenBuffer(c.list.Width(), c.list.Height())
	c.Draw(buf, buf.Bounds())
	for y := 0; y < buf.Height(); y++ {
		for x := 0; x < buf.Width(); x++ {
			if cell := buf.CellAt(x, y); cell != nil && cell.Link.URL == "/tmp" {
				return c, x, y
			}
		}
	}
	t.Fatal("rendered Markdown link missing")
	return nil, 0, 0
}

func TestChatLinkClick(t *testing.T) {
	for _, action := range []string{"click", "hold", "drag", "drag back", "release elsewhere", "double click", "stale"} {
		t.Run(action, func(t *testing.T) {
			c, x, y := linkTestChat(t)
			handled, cmd := c.HandleMouseDown(x, y)
			require.True(t, handled)
			require.NotNil(t, cmd)
			idx, itemY := c.list.ItemIndexAtPosition(x, y)
			click := DelayedClickMsg{ClickID: c.pendingClickID, ItemIdx: idx, X: x, Y: itemY}
			switch action {
			case "hold":
				c.HandleDelayedClick(click)
				require.Empty(t, c.openLink, "holding must not open a link before release")
			case "drag":
				c.HandleMouseDrag(x+4, y)
			case "drag back":
				c.HandleMouseDrag(x+4, y)
				c.HandleMouseDrag(x, y)
			case "release elsewhere":
				c.HandleMouseUp(x+4, y)
			case "double click":
				c.HandleMouseDown(x, y)
			case "stale":
				c.pendingClickID++
			}
			c.HandleMouseUp(x, y)
			c.HandleDelayedClick(click)
			if action == "click" || action == "hold" {
				require.Equal(t, "/tmp", c.openLink)
			} else {
				require.Empty(t, c.openLink)
			}
		})
	}
}

func TestChatWrappedLinkHitArea(t *testing.T) {
	c, _, _ := linkTestChat(t)
	c.SetMessages(chat.NewAssistantMessageItem(c.com.Styles, &message.Message{
		ID: "wrapped", Role: message.Assistant, Parts: []message.ContentPart{
			message.TextContent{Text: "Prefix 👩‍💻 [" + strings.Repeat("folder name ", 30) + "](/tmp)"},
			message.Finish{Reason: message.FinishReasonEndTurn},
		},
	}))
	item := c.list.ItemAt(0)
	rendered := item.Render(c.list.Width())
	buf := uv.NewScreenBuffer(c.list.Width(), strings.Count(rendered, "\n")+1)
	uv.NewStyledString(rendered).Draw(buf, buf.Bounds())
	rows := map[int]bool{}
	for y := 0; y < buf.Height(); y++ {
		for x := 0; x < buf.Width(); x++ {
			cell := buf.CellAt(x, y)
			if cell != nil && cell.Link.URL != "" {
				rows[y] = true
				require.Equal(t, cell.Link.URL, c.itemLinkAt(item, x, y))
			}
		}
	}
	require.Greater(t, len(rows), 1)
}

// Exercise the real desktop opener without creating a visible window.
func TestOpenChatLinkDesktopDispatch(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux desktop dispatcher")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "folder with spaces")
	require.NoError(t, os.Mkdir(path, 0700))
	output := filepath.Join(dir, "opened")
	dispatcher := filepath.Join(dir, "xdg-open")
	require.NoError(t, os.WriteFile(dispatcher, []byte("#!/bin/sh\n[ /dev/null -ef /proc/self/fd/0 ] || exit 40\n[ /dev/null -ef /proc/self/fd/1 ] || exit 41\n[ /dev/null -ef /proc/self/fd/2 ] || exit 42\nprintf 'noisy app output\\n'\nprintf 'Qt accessibility warning\\n' >&2\nprintf '%s' \"$1\" > \"$LINK_TEST_OUTPUT\"\n"), 0700))
	t.Setenv("PATH", dir)
	t.Setenv("LINK_TEST_OUTPUT", output)
	require.Nil(t, openChatLink(path, dir)())
	got, err := os.ReadFile(output)
	require.NoError(t, err)
	require.Equal(t, (&url.URL{Scheme: "file", Path: path}).String(), string(got))
}

func TestOpenChatLinkDesktopFailure(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux desktop dispatcher")
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "xdg-open"), []byte("#!/bin/sh\necho 'app failure' >&2\nexit 7\n"), 0700))
	t.Setenv("PATH", dir)
	msg, ok := openChatLink(dir, dir)().(util.InfoMsg)
	require.True(t, ok)
	require.Equal(t, util.InfoTypeError, msg.Type)
	require.Contains(t, msg.Msg, "exit status 7")
}
