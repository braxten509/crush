//go:build !linux

package cliagent

import (
	"fmt"
	"os/exec"
)

func instructionCommand(dir, home, name string, args ...string) (*exec.Cmd, func(), error) {
	return nil, nil, fmt.Errorf("Crush's shared-only CLI instructions require Linux and bubblewrap")
}

func CloseInstructionViews() {}
