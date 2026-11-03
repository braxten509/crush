//go:build !linux

package localmodel

import "os/exec"

func prepareProcess(cmd *exec.Cmd) {}
