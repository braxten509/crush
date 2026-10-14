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
	name    string
	owner   string // the program holding the port
	group   string // the background program it is under; "" from older chats
}

func (s hostServer) url() string { return "http://localhost:" + strconv.Itoa(s.port) }

// fold names the row s folds into with the other ports of its program.
func (s hostServer) fold() string { return strconv.Itoa(s.session) + "/" + s.group }

// serverRow is a row of the server list: one server, or a background
// program folded over its several servers.
type serverRow struct {
	server hostServer // a fold's first server
	ports  int        // how many servers a fold holds; 0 on a server's row
	child  bool       // a server listed under its opened program
}

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
		for _, server := range s.snapshot().Servers {
			if !slices.ContainsFunc(out, func(o hostServer) bool { return o.port == server.Port }) {
				out = append(out, hostServer{port: server.Port, session: s.id, name: server.Name, owner: server.Owner, group: server.Group})
			}
		}
	}
	return out
}

// serverRows lays the servers out as rows: a program with several folds
// into one row, followed by its servers while it is open.
func (h *Host) serverRows() []serverRow {
	all := h.servers()
	count := map[string]int{}
	for _, s := range all {
		if s.group != "" {
			count[s.fold()]++
		}
	}
	var rows []serverRow
	for _, s := range all {
		fold := s.fold()
		switch {
		case s.group == "" || count[fold] < 2:
			rows = append(rows, serverRow{server: s})
		case !slices.ContainsFunc(rows, func(r serverRow) bool { return r.ports > 0 && r.server.fold() == fold }):
			rows = append(rows, serverRow{server: s, ports: count[fold]})
			if h.openFolds[fold] {
				for _, c := range all {
					if c.group != "" && c.fold() == fold {
						rows = append(rows, serverRow{server: c, child: true})
					}
				}
			}
		}
	}
	return rows
}

// shownServers returns the server rows that fit at the bottom of the open
// list, leaving room for at least one chat, and the row the first is on.
func (h *Host) shownServers() (shown []serverRow, top int) {
	if !h.listOpen() {
		return nil, h.height
	}
	all := h.serverRows()
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

// isPicked reports whether r is the picked row.
func (h *Host) isPicked(r serverRow) bool {
	if r.ports > 0 {
		return h.pickedFold == r.server.fold()
	}
	return h.pickedFold == "" && h.pickedServer == r.server.port
}

// pick makes r the picked row.
func (h *Host) pick(r serverRow) {
	h.pickedServer, h.pickedFold = 0, ""
	if r.ports > 0 {
		h.pickedFold = r.server.fold()
	} else {
		h.pickedServer = r.server.port
	}
}

// unpick lets go of the picked row.
func (h *Host) unpick() { h.pickedServer, h.pickedFold = 0, "" }

// picked returns the picked row, letting go of one that has gone.
func (h *Host) picked() (serverRow, bool) {
	if h.pickedServer == 0 && h.pickedFold == "" {
		return serverRow{}, false
	}
	shown, _ := h.shownServers()
	i := slices.IndexFunc(shown, h.isPicked)
	if i < 0 {
		h.unpick()
		return serverRow{}, false
	}
	return shown[i], true
}

// useRow does what enter or a click on the picked row does: a fold opens
// or closes, and a server opens in the browser.
func (h *Host) useRow(r serverRow) {
	if r.ports == 0 {
		openLink(r.server.url())
		return
	}
	fold := r.server.fold()
	if h.openFolds[fold] {
		delete(h.openFolds, fold)
		return
	}
	if h.openFolds == nil {
		h.openFolds = map[string]bool{}
	}
	h.openFolds[fold] = true
}

// stopServer asks the chat that reported the server to stop it.
func (h *Host) stopServer(server hostServer) {
	h.unpick()
	s := h.byID(server.session)
	if s == nil {
		return
	}
	go func() { _, _ = io.WriteString(s.emu.InputPipe(), stopServerSequence(server.port)) }()
}

// serverKey handles a key while a server row is picked, and reports
// whether it used it. Any other key lets go of the row and goes on to the
// chat.
func (h *Host) serverKey(key string) bool {
	row, ok := h.picked()
	if !ok {
		return false
	}
	switch key {
	case "enter":
		h.useRow(row)
	case "x", "X":
		h.stopServer(row.server)
	case "up", "down":
		shown, _ := h.shownServers()
		i := slices.IndexFunc(shown, h.isPicked)
		if key == "up" {
			i = max(i-1, 0)
		} else {
			i = min(i+1, len(shown)-1)
		}
		h.pick(shown[i])
	case "esc":
		h.unpick()
	default:
		h.unpick()
		return false
	}
	return true
}
