package config

// GlobalDataPath returns the global data config file this store writes
// global-scope changes to (the file `crush login` saves keys in).
func (s *ConfigStore) GlobalDataPath() string {
	return s.globalDataPath
}
