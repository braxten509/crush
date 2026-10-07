package config

import (
	"charm.land/catwalk/pkg/catwalk"
	"fmt"
)

// NativeCompaction identifies agents that own their conversation's context.
// An unavailable budget report must not enable a competing Crush compactor.
func NativeCompaction(kind catwalk.Type) bool {
	switch kind {
	case TypeClaudeCode, TypeCodexCLI, TypeOpenCodeCLI, TypeGrokCLI, TypeAGYCLI:
		return true
	default:
		return false
	}
}

// FallbackCompactionLimit is shared by the fallback trigger and its display.
func FallbackCompactionLimit(window, configured int64) int64 {
	if window <= 0 {
		return configured
	}
	buffer := window / 5
	if window > 200_000 {
		buffer = 20_000
	}
	return min(configured, window-buffer)
}

// DefaultAutoCompactTokenLimit is used until the user saves a threshold.
const DefaultAutoCompactTokenLimit int64 = 400_000

// GetAutoCompactTokenLimit returns Crush's global compaction threshold.
func (m *Options) GetAutoCompactTokenLimit() int64 {
	if m != nil && m.AutoCompactTokenLimit > 0 {
		return m.AutoCompactTokenLimit
	}
	return DefaultAutoCompactTokenLimit
}

// ValidateAutoCompactTokenLimit checks a user-entered threshold.
func ValidateAutoCompactTokenLimit(tokens int64) error {
	if tokens < 1000 {
		return fmt.Errorf("Enter at least 1,000 tokens")
	}
	return nil
}
