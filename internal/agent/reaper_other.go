//go:build !linux

package agent

import "os"

// startReaper does nothing where the process table can't be read.
func startReaper(string) (*os.File, error) { return nil, nil }
