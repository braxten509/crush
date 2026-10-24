//go:build !linux || !amd64

package filechange

import (
	"errors"
	"os/exec"
)

func startObservedProcess(cmd *exec.Cmd, review *ProcessReview) error { return cmd.Start() }

// WatchUnavailable explains why commands run here without a review.
func WatchUnavailable() error { return errors.ErrUnsupported }
