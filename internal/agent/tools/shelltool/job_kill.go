package shelltool

import (
	"context"
	_ "embed"
	"fmt"

	"charm.land/fantasy"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/shell"
)

//go:embed job_kill.md
var jobKillDescription string

func NewJobKillTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		tools.JobKillToolName,
		jobKillDescription,
		func(ctx context.Context, params tools.JobKillParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.ShellID == "" {
				return fantasy.NewTextErrorResponse("missing shell_id"), nil
			}

			bgManager := shell.GetBackgroundShellManager()

			bgShell, ok := bgManager.Get(params.ShellID)
			if !ok {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("background shell not found: %s", params.ShellID)), nil
			}

			metadata := tools.JobKillResponseMetadata{
				ShellID:     params.ShellID,
				Command:     bgShell.Command,
				Description: bgShell.Description,
			}

			if !bgShell.IsDone() {
				tools.MarkStoppedByAgent(params.ShellID)
			}
			err := bgManager.Kill(params.ShellID)
			if err != nil {
				tools.UnmarkStoppedByAgent(params.ShellID)
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}

			result := fmt.Sprintf("Background shell %s terminated successfully", params.ShellID)
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(result), metadata), nil
		},
	)
}
