package sessionhost

import (
	"io"
	"log/slog"
	"slices"
	"strconv"

	"github.com/pkg/browser"
)

// hostServer is a port a chat's background work listens on, shown at the
// bottom of the open list.
type hostServer struct {
	port    int
	session int // the id of the chat that reported it
}

func (s hostServer) url() string { return "http://localhost:" + strconv.Itoa(s.port) }

// Server rows at the bottom of the open list: one per server, then the key
// hint, which shows only while one is picked.
const serverHint = " ↵ open · x stop · esc back"

// openLink opens url in the browser; a variable so tests don't.
var openLink = func(url string) {
	go func() {
		if err := browser.OpenURL(url); err != nil {
			slog.Warn("Could not open the server's link", "url", url, "error", err)
		}
	}()
}

// servers lists the chats' servers in list order, each port once.
func (h *Host) servers() []hostServer {
	var out []hostServer
	for _, s := range h.sessions {
		if s.kind != chatSession {
			continue
		}
		for _, port := range s.snapshot().Servers {
			if !slices.ContainsFunc(out, func(o hostServer) bool { return o.port == port }) {
				out = append(out, hostServer{port: port, session: s.id})
			}
		}
	}
	return out
}

// shownServers returns the servers that fit at the bottom of the open list,
// leaving room for at least one chat, and the row the first is on.
func (h *Host) shownServers() (shown []hostServer, top int) {
	if !h.listOpen() {
		return nil, h.height
	}
	all := h.servers()
	room := h.height - listFirstRow - listBlockHeight - listGapRows - 1
	shown = all[:max(min(len(all), room), 0)]
	return shown, h.height - 1 - len(shown)
}

// footerRows is how many rows below the chats the side keeps: a gap and the
// notice row, then in the open list its servers and their hint.
func (h *Host) footerRows() int {
	if !h.listOpen() {
		return sideFooterRows
	}
	if shown, _ := h.shownServers(); len(shown) > 0 {
		return listGapRows + len(shown) + 1
	}
	return listGapRows
}

// picked returns the picked server, letting go of one that has stopped.
func (h *Host) picked() (hostServer, bool) {
	if h.pickedServer == 0 {
		return hostServer{}, false
	}
	shown, _ := h.shownServers()
	i := slices.IndexFunc(shown, func(s hostServer) bool { return s.port == h.pickedServer })
	if i < 0 {
		h.pickedServer = 0
		return hostServer{}, false
	}
	return shown[i], true
}

// stopServer asks the chat that reported the server to stop it.
func (h *Host) stopServer(server hostServer) {
	h.pickedServer = 0
	s := h.byID(server.session)
	if s == nil {
		return
	}
	go func() { _, _ = io.WriteString(s.emu.InputPipe(), stopServerSequence(server.port)) }()
}

// serverKey handles a key while a server is picked, and reports whether it
// used it. Any other key lets go of the server and goes on to the chat.
func (h *Host) serverKey(key string) bool {
	server, ok := h.picked()
	if !ok {
		return false
	}
	switch key {
	case "enter":
		openLink(server.url())
	case "x", "X":
		h.stopServer(server)
	case "up", "down":
		shown, _ := h.shownServers()
		i := slices.Index(shown, server)
		if key == "up" {
			i = max(i-1, 0)
		} else {
			i = min(i+1, len(shown)-1)
		}
		h.pickedServer = shown[i].port
	case "esc":
		h.pickedServer = 0
	default:
		h.pickedServer = 0
		return false
	}
	return true
}
