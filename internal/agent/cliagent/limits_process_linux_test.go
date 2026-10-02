//go:build linux

package cliagent

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUsageProbesReapTheirProcesses(t *testing.T) {
	for _, probe := range []struct {
		name  string
		fetch func(context.Context) ([]Limit, error)
		data  string
	}{
		{"codex", fetchCodexLimits, `{"rateLimits":{"primary":{"usedPercent":12,"windowDurationMins":300}}}`},
		{"grok", fetchGrokLimits, `{"config":{"currentPeriod":{"type":"USAGE_PERIOD_TYPE_WEEKLY"},"creditUsagePercent":12}}`},
	} {
		for _, failed := range []bool{false, true} {
			t.Run(probe.name+"/error="+strconv.FormatBool(failed), func(t *testing.T) {
				dir := t.TempDir()
				pidPath := filepath.Join(dir, "pid")
				t.Setenv("CRUSH_TEST_PROBE_PID", pidPath)
				response := `{"id":"2","result":` + probe.data + `}`
				if failed {
					response = `{"id":"2","error":{"code":-1,"message":"probe failed"}}`
				}
				script := "#!/bin/sh\nprintf '%s' \"$$\" > \"$CRUSH_TEST_PROBE_PID\"\nread -r _\n" +
					"echo '{\"id\":\"1\",\"result\":{}}'\necho '" + response + "'\ncat >/dev/null\n"
				require.NoError(t, os.WriteFile(filepath.Join(dir, probe.name), []byte(script), 0o755))
				t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
				limits, err := probe.fetch(t.Context())
				if failed {
					require.ErrorContains(t, err, "probe failed")
				} else {
					require.NoError(t, err)
					require.Len(t, limits, 1)
				}
				raw, err := os.ReadFile(pidPath)
				require.NoError(t, err)
				pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
				require.NoError(t, err)
				t.Cleanup(func() { _, _ = syscall.Wait4(pid, nil, syscall.WNOHANG, nil) })
				require.ErrorIs(t, syscall.Kill(pid, 0), syscall.ESRCH, "a finished usage check must not leave a zombie")
			})
		}
	}
}
