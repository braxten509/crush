//go:build !linux

package memwatch

import "errors"

func readUsage() (Usage, error) { return Usage{}, errors.ErrUnsupported }

func topPrograms(int) []Program { return nil }

// ClaimNotify never claims outside Linux, where memory isn't checked.
func ClaimNotify(string) bool { return false }
