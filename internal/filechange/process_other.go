//go:build !linux || !amd64

package filechange

import "os/exec"

func startObservedProcess(cmd *exec.Cmd, review *ProcessReview) error { return cmd.Start() }
