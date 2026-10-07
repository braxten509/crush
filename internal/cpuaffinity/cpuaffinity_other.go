//go:build !linux

package cpuaffinity

import "errors"

var errUnsupported = errors.New("keeping AI work on some CPUs only works on Linux")

// Supported reports whether Crush can move processes between CPUs here.
func Supported() bool { return false }

// Apply is a no-op outside Linux.
func Apply(string) (string, error) { return "", errUnsupported }

// Reset is a no-op outside Linux.
func Reset() error { return nil }
