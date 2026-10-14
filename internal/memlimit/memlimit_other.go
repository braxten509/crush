//go:build !linux

package memlimit

import "errors"

// Join is Linux-only: the box is a systemd slice.
func Join(string) error { return errors.ErrUnsupported }

// Apply is Linux-only: the box is a systemd slice.
func Apply(string, uint64) error { return errors.ErrUnsupported }

// Usage is Linux-only: the box is a systemd slice.
func Usage(string) (uint64, bool) { return 0, false }
