package config

import (
	"fmt"
	"math"
)

// MinMemoryLimitGB is the smallest limit accepted; below it Crush and one
// agent CLI with its add-ons would barely fit.
const MinMemoryLimitGB = 2

// MemoryLimitBytes is the most memory all Crush windows together may use,
// or 0 for no limit. Until the user saves a limit it is half of totalRAM.
func (m *Options) MemoryLimitBytes(totalRAM uint64) uint64 {
	if m == nil || m.MemoryLimitGB == nil {
		return totalRAM / 2
	}
	return GBToBytes(*m.MemoryLimitGB)
}

// GBToBytes turns a limit in GB into bytes; 0 or less is no limit.
func GBToBytes(gb float64) uint64 {
	if gb <= 0 || math.IsNaN(gb) || math.IsInf(gb, 0) {
		return 0
	}
	return uint64(gb * (1 << 30))
}

// ValidateMemoryLimitGB checks a user-entered limit; 0 means no limit.
func ValidateMemoryLimitGB(gb float64) error {
	if math.IsNaN(gb) || math.IsInf(gb, 0) || gb < 0 {
		return fmt.Errorf("Enter a number of GB, or leave it empty for no limit")
	}
	if gb != 0 && gb < MinMemoryLimitGB {
		return fmt.Errorf("Enter at least %d GB, or leave it empty for no limit", MinMemoryLimitGB)
	}
	return nil
}
