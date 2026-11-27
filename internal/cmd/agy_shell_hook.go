package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/spf13/cobra"
	"mvdan.cc/sh/v3/syntax"
)

// Agy executes the rewritten command after its ordinary pre-tool checks.
func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use: "__agy-shell-hook", Hidden: true, Args: cobra.NoArgs,
		RunE: agyShellHook,
	})
}

func agyShellHook(cmd *cobra.Command, _ []string) error {
	reply := map[string]any{"decision": "allow"}
	writeReply := func() error { return json.NewEncoder(cmd.OutOrStdout()).Encode(reply) }
	if os.Getenv(agent.AGYCommandTurnEnv) == "" {
		return writeReply()
	}
	var event struct {
		ToolCall struct {
			Name string
			Args struct {
				CommandLine       string
				WaitMsBeforeAsync int
				IsDaemon          bool
			}
		}
	}
	if err := json.NewDecoder(cmd.InOrStdin()).Decode(&event); err != nil {
		return err
	}
	args := event.ToolCall.Args
	if event.ToolCall.Name != "run_command" || args.CommandLine == "" {
		return writeReply()
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if agyCrushCommand(args.CommandLine, exe) {
		return writeReply()
	}
	executable, err := syntax.Quote(exe, syntax.LangBash)
	if err != nil {
		return err
	}
	command, err := syntax.Quote(args.CommandLine, syntax.LangBash)
	if err != nil {
		return err
	}
	mode := "--service"
	if !args.IsDaemon {
		wait := args.WaitMsBeforeAsync
		if wait <= 0 {
			wait = 10000
		}
		mode = fmt.Sprintf("--agy-foreground=%dms", wait)
	}
	// Give the wrapper Agy's full native wait to return the job ID,
	// even when the original command asked to yield sooner.
	reply["overwrite"] = map[string]any{
		"CommandLine":       executable + " bg " + mode + " -- " + command,
		"WaitMsBeforeAsync": 10000,
	}
	return writeReply()
}

// A direct Crush control command already runs in the right place. Compound
// commands still go through the shell so the entire operation is tracked.
func agyCrushCommand(command, exe string) bool {
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil || len(file.Stmts) != 1 {
		return false
	}
	call, ok := file.Stmts[0].Cmd.(*syntax.CallExpr)
	if !ok || len(call.Args) < 2 {
		return false
	}
	if name := call.Args[0].Lit(); name != "crush" && name != exe {
		return false
	}
	switch call.Args[1].Lit() {
	case "bg", "spawn", "ask", "secure-entry", "skill":
		return true
	}
	return false
}
