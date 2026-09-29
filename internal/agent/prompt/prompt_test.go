package prompt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

func TestDisabledInstructionFilesStayOutOfPrompt(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "AGENTS.md")
	personal := filepath.Join(dir, "CLAUDE.md")
	require.NoError(t, os.WriteFile(project, []byte("project-guidance-marker"), 0o600))
	require.NoError(t, os.WriteFile(personal, []byte("personal-guidance-marker"), 0o600))
	store := config.NewTestStore(&config.Config{Options: &config.Options{
		DisableInstructionFiles: true, ContextPaths: []string{project}, GlobalContextPaths: []string{personal},
	}})
	prompt, err := NewPrompt("test", `{{range .ContextFiles}}{{.Content}}{{end}}{{range .GlobalContextFiles}}{{.Content}}{{end}}`, WithWorkingDir(dir))
	require.NoError(t, err)
	text, err := prompt.Build(t.Context(), "openai-compat", "route-llm", store)
	require.NoError(t, err)
	require.Empty(t, text)
	store.Config().Options.DisableInstructionFiles = false
	text, err = prompt.Build(t.Context(), "openai-compat", "route-llm", store)
	require.NoError(t, err)
	require.Contains(t, text, "project-guidance-marker")
	require.Contains(t, text, "personal-guidance-marker")
}
