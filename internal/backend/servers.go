package backend

import (
	"context"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/proto"
)

// ListServers lists the ports the background work of every chat of the
// project listens on, one row per port, as the terminal's session list
// finds them (internal/agent/servers.go; G-18). Jobs of chats the project
// does not hold are left out.
func (b *Backend) ListServers(ctx context.Context, workspaceID string) ([]proto.ServerPort, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	var mine []agent.Process
	for _, p := range agent.BackgroundProcesses() {
		ok, seen := known[p.Session]
		if !seen {
			_, err := ws.Sessions.Get(ctx, p.Session)
			ok = err == nil
			known[p.Session] = ok
		}
		if ok {
			mine = append(mine, p)
		}
	}
	servers, _ := agent.FindServers(mine)
	out := make([]proto.ServerPort, 0, len(servers))
	for _, s := range servers {
		out = append(out, proto.ServerPort{
			Port:      s.Port,
			Program:   s.Owner,
			Name:      s.Title(),
			Command:   s.Process.Command,
			// DELIBERATE BREAK (G-18): no job ID
			PID:       s.Process.PID,
			SessionID: s.Process.Session,
		})
	}
	return out, nil
}
