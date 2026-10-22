package config

import "sync"

// HoldReloadsForTest holds off every reload of s, as a reload in progress
// does (a folder's config watcher), until release runs: a write's own
// reload is then skipped (autoReload) and ReloadFromDisk waits.
func (s *ConfigStore) HoldReloadsForTest() (release func()) {
	s.writeMu.Lock()
	var once sync.Once
	return func() { once.Do(s.writeMu.Unlock) }
}
