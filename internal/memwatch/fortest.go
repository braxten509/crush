package memwatch

// SetReadingsForTest makes Default, and TotalRAM, report memory percent
// full of totalRAM bytes, with top as the biggest programs, instead of
// reading this computer, until restore runs. The crushtest seam `memory`
// uses it (desktop design 5-6, G-21).
func SetReadingsForTest(percent int, top []Program, totalRAM uint64) (restore func()) {
	if totalRAM == 0 {
		totalRAM = 100
	}
	// Rounding used memory up keeps Percent at exactly percent.
	used := (totalRAM*uint64(max(0, min(percent, 100))) + 99) / 100
	usage := Usage{MemTotal: totalRAM, MemAvailable: totalRAM - min(used, totalRAM)}
	top = append([]Program(nil), top...)

	w := Default
	w.mu.Lock()
	savedUsage, savedTop := w.usage, w.top
	w.usage = func() (Usage, error) { return usage, nil }
	w.top = func(n int) []Program { return append([]Program(nil), top[:min(n, len(top))]...) }
	w.at = w.now().AddDate(-1, 0, 0) // the next Recent reads again
	w.mu.Unlock()
	return func() {
		w.mu.Lock()
		w.usage, w.top = savedUsage, savedTop
		w.at = w.now().AddDate(-1, 0, 0)
		w.mu.Unlock()
	}
}
