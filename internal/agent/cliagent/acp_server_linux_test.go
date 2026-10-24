//go:build linux

package cliagent

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestOpenCodeSteerServerFindsTheLocalServer: the ACP parent starts an
// `opencode serve --stdio` child with the password in its environment and
// the port in its socket table.
func TestOpenCodeSteerServerFindsTheLocalServer(t *testing.T) {
	script := `python3 -c 'import http.server, socketserver
s = socketserver.TCPServer(("127.0.0.1", 0), http.server.SimpleHTTPRequestHandler)
s.serve_forever()' serve --stdio & wait`
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append(os.Environ(), "OPENCODE_PASSWORD=secret")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
	})

	var url, password string
	require.Eventually(t, func() bool {
		url, password = openCodeSteerServer(&proc{cmd: cmd})
		return url != ""
	}, 3*time.Second, 20*time.Millisecond)
	require.Equal(t, "secret", password)
	require.Contains(t, url, "http://127.0.0.1:")
}
