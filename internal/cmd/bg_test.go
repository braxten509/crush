package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/stretchr/testify/require"
)

func TestBackgroundCommandTransportsQuotedArguments(t *testing.T) {
	for _, test := range []struct {
		name    string
		args    []string
		command string
		output  string
		service bool
	}{
		{name: "script", args: []string{"--", "sleep 2; echo done"}, command: "sleep 2; echo done"},
		{name: "arguments", args: []string{"--", "printf", "%s", "$(not-a-command); literal"}, command: "printf %s '$(not-a-command); literal'"},
		{name: "service", args: []string{"--service", "--", "npm run dev"}, command: "npm run dev", service: true},
		{name: "output", args: []string{"--output", "001"}, output: "001"},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			t.Setenv(agent.TasksDirEnv, directory)
			t.Setenv(agent.TasksSessionEnv, "owner")
			requests := make(chan agent.TaskRequest, 1)
			done := make(chan struct{})
			defer close(done)
			go func() {
				tick := time.NewTicker(5 * time.Millisecond)
				defer tick.Stop()
				for {
					select {
					case <-done:
						return
					case <-tick.C:
					}
					paths, _ := filepath.Glob(filepath.Join(directory, "*.req"))
					if len(paths) == 0 {
						continue
					}
					data, err := os.ReadFile(paths[0])
					if err != nil {
						continue
					}
					var request agent.TaskRequest
					if json.Unmarshal(data, &request) != nil {
						continue
					}
					requests <- request
					response := fantasy.NewTextResponse("job accepted")
					reply, _ := json.Marshal(agent.TaskReply{Background: &response})
					_ = os.WriteFile(strings.TrimSuffix(paths[0], ".req")+".ack", reply, 0600)
					return
				}
			}()
			cmd := newBackgroundCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetArgs(test.args)
			require.NoError(t, cmd.Execute())
			request := <-requests
			require.Equal(t, "owner", request.Session)
			require.NotNil(t, request.Background)
			require.Equal(t, test.command, request.Background.Command)
			require.Equal(t, test.output, request.Background.OutputID)
			require.Equal(t, test.service, request.Background.Service)
			require.True(t, filepath.IsAbs(request.Background.WorkingDir))
			require.Contains(t, output.String(), "job accepted")
		})
	}
}

func TestBackgroundCommandRejectsAmbiguousOperation(t *testing.T) {
	t.Setenv(agent.TasksDirEnv, t.TempDir())
	t.Setenv(agent.TasksSessionEnv, "owner")
	cmd := newBackgroundCommand()
	cmd.SetArgs([]string{"--output", "001", "--", "echo hi"})
	require.ErrorContains(t, cmd.Execute(), "exactly one")
}

func TestBackgroundServiceFlagRequiresCommand(t *testing.T) {
	t.Setenv(agent.TasksDirEnv, t.TempDir())
	t.Setenv(agent.TasksSessionEnv, "owner")
	cmd := newBackgroundCommand()
	cmd.SetArgs([]string{"--service", "--output", "001"})
	require.ErrorContains(t, cmd.Execute(), "requires a command")
}
