//go:build !linux

package skills

import (
	"os"
	"path/filepath"
)

func publishSkillDir(stage, destination string) error {
	// Reserve the destination exclusively. Publish SKILL.md last so discovery
	// never loads a partially copied skill on platforms without no-replace rename.
	if err := os.Mkdir(destination, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(stage)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == SkillFileName {
			continue
		}
		if err := os.Rename(filepath.Join(stage, entry.Name()), filepath.Join(destination, entry.Name())); err != nil {
			return err
		}
	}
	return os.Rename(filepath.Join(stage, SkillFileName), filepath.Join(destination, SkillFileName))
}
