package config

import (
	"encoding/json"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"slices"
)

// SetSkillEnabled persists a name toggle and publishes the new config only
// after the write succeeds. Never mutate a config snapshot held by a reader.
func (s *ConfigStore) SetSkillEnabled(name string, enabled bool) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	cfg := s.Config().cloneForWrite()
	if cfg.Options == nil {
		cfg.Options = &Options{}
	}
	var disabled []string
	if err := s.atomicWrite(ScopeGlobal, func(data []byte) ([]byte, error) {
		disabled = slices.Clone(cfg.Options.DisabledSkills)
		if current := gjson.GetBytes(data, "options.disabled_skills"); current.Exists() {
			if err := json.Unmarshal([]byte(current.Raw), &disabled); err != nil {
				return nil, err
			}
		}
		disabled = slices.DeleteFunc(disabled, func(n string) bool { return n == name })
		if !enabled {
			disabled = append(disabled, name)
		}
		if disabled == nil {
			disabled = []string{}
		}
		slices.Sort(disabled)
		return sjson.SetBytes(data, "options.disabled_skills", disabled)
	}); err != nil {
		return err
	}
	cfg.Options.DisabledSkills = disabled
	s.setConfig(cfg)
	if path, err := s.configPath(ScopeGlobal); err == nil {
		s.captureStalenessSnapshot(append(slices.Clone(s.loadedPaths), path))
	}
	return nil
}
