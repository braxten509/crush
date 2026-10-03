package agent

import (
	"context"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/secretguard"
)

// guardedTool refuses tool calls that would read a secrets folder. It sits
// inside any hooks, so it checks the input after hooks rewrite it, and it
// applies to sub-agents too.
type guardedTool struct {
	fantasy.AgentTool
}

func guardTools(tools []fantasy.AgentTool) []fantasy.AgentTool {
	out := make([]fantasy.AgentTool, len(tools))
	for i, tool := range tools {
		out[i] = guardedTool{tool}
	}
	return out
}

func (g guardedTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	if secretguard.Blocks(call.Name, []byte(call.Input)) {
		return fantasy.NewTextErrorResponse(secretguard.Reason), nil
	}
	return g.AgentTool.Run(ctx, call)
}
