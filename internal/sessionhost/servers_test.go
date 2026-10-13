package sessionhost

import (
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestServersShowOncePerPort(t *testing.T) {
	h := newTestHost(t, 120, 30,
		Status{Title: "One", Servers: ports(5173, 8080)},
		Status{Title: "Two", Servers: ports(8080, 3000)},
	)
	require.Equal(t, []hostServer{{port: 5173, session: 1}, {port: 8080, session: 1}, {port: 3000, session: 2}}, h.servers())
	lines := strings.Split(screenText(h), "\n")
	require.Contains(t, lines[h.height-4], "↗ localhost:5173")
	require.Contains(t, lines[h.height-3], "↗ localhost:8080")
	require.Contains(t, lines[h.height-2], "↗ localhost:3000")
	require.NotContains(t, lines[h.height-1], "x stop", "the hint shows only while a server is picked")
	require.Equal(t, 1, strings.Count(screenText(h), "localhost:8080"))

	press(h, "alt+s")
	require.NotContains(t, screenText(h), "localhost", "the strip has no room for links")
}

func TestServerPickOpenAndStop(t *testing.T) {
	var opened []string
	open := openLink
	openLink = func(url string) { opened = append(opened, url) }
	t.Cleanup(func() { openLink = open })
	h := newTestHost(t, 120, 30, Status{Title: "One"}, Status{Title: "Two", Servers: ports(5173, 8080)})
	_, top := h.shownServers()
	click := func(x, y int) { h.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y}) }

	click(5, top)
	require.Equal(t, 5173, h.pickedServer, "the first click picks")
	require.Empty(t, opened)
	require.Contains(t, screenText(h), "x stop")
	click(5, top)
	require.Equal(t, []string{"http://localhost:5173"}, opened, "a second click opens")

	h.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	require.Equal(t, 8080, h.pickedServer)
	press(h, "enter")
	require.Equal(t, "http://localhost:8080", opened[1])
	press(h, "esc")
	require.Zero(t, h.pickedServer)
	require.Equal(t, 0, h.active, "esc doesn't reach the chat list")

	click(5, top)
	click(5, listFirstRow)
	require.Zero(t, h.pickedServer, "a click elsewhere lets go")

	click(5, top+1)
	press(h, "x")
	require.Zero(t, h.pickedServer)
	got := make(chan string, 1)
	go func() {
		b := make([]byte, 64)
		n, _ := io.ReadAtLeast(h.sessions[1].emu, b, len(stopServerSequence(8080)))
		got <- string(b[:n])
	}()
	select {
	case seq := <-got:
		port, ok := ParseStopServer(seq)
		require.True(t, ok)
		require.Equal(t, 8080, port, "x asks the chat that runs it to stop it")
	case <-time.After(5 * time.Second):
		t.Fatal("the chat was not asked to stop the server")
	}
}

func TestStopServerSequence(t *testing.T) {
	t.Parallel()
	port, ok := ParseStopServer(stopServerSequence(5173))
	require.True(t, ok)
	require.Equal(t, 5173, port)
	for _, bad := range []string{"\x1b]7373;5173\x07", "\x1b]7374;x\x07", "\x1b]7374;0\x07"} {
		_, ok := ParseStopServer(bad)
		require.False(t, ok, bad)
	}
	require.False(t, Status{Servers: ports(1)}.Equal(Status{Servers: ports(2)}))
	require.True(t, Status{Title: "a", Servers: ports(1)}.Equal(Status{Title: "a", Servers: ports(1)}))
}

func ports(numbers ...int) []Server {
	out := make([]Server, len(numbers))
	for i, n := range numbers {
		out[i] = Server{Port: n}
	}
	return out
}

func TestServersShowTheirNames(t *testing.T) {
	h := newTestHost(t, 120, 30, Status{Title: "One", Servers: []Server{
		{Port: 5037, Name: "Forecaster app"},
		{Port: 8080, Name: "A name far too long to fit the side list"},
	}})
	col := func(line, text string) int {
		i := strings.Index(line, text)
		require.GreaterOrEqual(t, i, 0, text)
		return ansi.StringWidth(line[:i])
	}
	lines := strings.Split(screenText(h), "\n")
	app, long := lines[h.height-3], lines[h.height-2]
	require.Contains(t, app, "↗ Forecaster app")
	require.Contains(t, long, "↗ A name far")
	require.Contains(t, long, "…")
	require.Equal(t, col(app, ":5037"), col(long, ":8080"), "the ports line up")

	_, top := h.shownServers()
	h.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: top})
	picked := strings.Split(screenText(h), "\n")[top]
	require.Less(t, col(picked, ":5037"), col(picked, "×"))
	require.Less(t, col(picked, "×"), listWidth)
	require.Equal(t, col(app, ":5037"), col(picked, ":5037"), "picking doesn't move the port")
}

func TestServerReadsBarePorts(t *testing.T) {
	t.Parallel()
	st, ok := parseStatus([]byte(`7373;{"servers":[5173,{"port":8080,"name":"API"}]}`))
	require.True(t, ok)
	require.Equal(t, []Server{{Port: 5173}, {Port: 8080, Name: "API"}}, st.Servers)
}
