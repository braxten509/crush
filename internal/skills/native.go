package skills

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// NativeLocation gives external CLIs a readable file for embedded skills.
// Only metadata goes into their prompt; the full file is read on demand.
func NativeLocation(skill *Skill) (*Skill, error) {
	if !skill.Builtin {
		return skill, nil
	}
	rel, ok := strings.CutPrefix(skill.SkillFilePath, BuiltinPrefix)
	if !ok {
		return nil, fmt.Errorf("invalid builtin skill path %q", skill.SkillFilePath)
	}
	content, err := builtinFS.ReadFile("builtin/" + rel)
	if err != nil {
		return nil, err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(content)
	path := filepath.Join(cache, "crush", "builtin-skills", fmt.Sprintf("%x", digest[:]), filepath.FromSlash(rel))
	if previous, err := os.ReadFile(path); err != nil || !bytes.Equal(previous, content) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		file, err := os.CreateTemp(filepath.Dir(path), ".skill-*")
		if err != nil {
			return nil, err
		}
		defer os.Remove(file.Name())
		_, writeErr := file.Write(content)
		closeErr := file.Close()
		if writeErr != nil {
			return nil, writeErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if err := os.Rename(file.Name(), path); err != nil {
			return nil, err
		}
	}
	copy := *skill
	copy.Path, copy.SkillFilePath = filepath.Dir(path), path
	return &copy, nil
}
