package config

import "fmt"

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
