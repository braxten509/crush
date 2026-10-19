package config

// GlobalDataPath returns the global data config file this store writes
// global-scope changes to (the file `crush login` saves keys in).
func (s *ConfigStore) GlobalDataPath() string {
	return s.globalDataPath
}

// ChangedOnDisk reports whether a config file the store loaded or looked
// for changed on disk since its last load ([ConfigStore.ConfigStaleness]).
// It reports false while a write or reload holds the store: that reload
// takes the change in anyway.
func (s *ConfigStore) ChangedOnDisk() bool {
	if !s.writeMu.TryLock() {
		return false
	}
	defer s.writeMu.Unlock()
	return s.ConfigStaleness().Dirty
}
