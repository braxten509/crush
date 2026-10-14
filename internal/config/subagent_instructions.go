package config

import (
	"os"
	"path/filepath"
	"strings"
)

// SubAgentInstructionsFile is where the user's instructions for starting
// sub-agents are saved, beside the global crush.json so they hold for every
// project and can be edited by hand too.
func SubAgentInstructionsFile() string {
	return filepath.Join(filepath.Dir(GlobalConfig()), "subagent-instructions.md")
}

// LoadSubAgentInstructions returns the saved instructions; "" when there are
// none.
func LoadSubAgentInstructions() string {
	data, _ := os.ReadFile(SubAgentInstructionsFile())
	return strings.TrimSpace(string(data))
}

// SaveSubAgentInstructions saves text as the instructions, replacing the file
// whole so a reader never sees half of it.
func SaveSubAgentInstructions(text string) error {
	path := SubAgentInstructionsFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text != "" {
		text += "\n"
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
