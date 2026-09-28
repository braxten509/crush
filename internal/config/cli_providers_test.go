package config

import (
	"os"
	"path/filepath"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/stretchr/testify/require"
)

func TestAddCLIProviders(t *testing.T) {
	detectCLIs = true
	t.Cleanup(func() { detectCLIs = false })

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\n"), 0o755))

	cfg := &Config{Providers: csync.NewMapFrom(map[string]ProviderConfig{
		string(TypeCodexCLI): {Disable: true},
	})}
	cfg.addCLIProviders(dir)

	claude, ok := cfg.Providers.Get(string(TypeClaudeCode))
	require.True(t, ok, "claude on PATH is registered")
	require.NotEmpty(t, claude.Models)

	codex, _ := cfg.Providers.Get(string(TypeCodexCLI))
	require.True(t, codex.Disable, "user settings are kept")
	require.NotEmpty(t, codex.Models, "missing fields get defaults")

	large, small, ok := cfg.defaultCLIModels()
	require.True(t, ok)
	require.Equal(t, "opus", large.Model)
	require.Equal(t, "haiku", small.Model)
}

func TestListModels(t *testing.T) {
	dir := t.TempDir()
	write := func(bin, out string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, bin), []byte("#!/bin/sh\ncat <<'EOF'\n"+out+"EOF\n"), 0o755))
	}
	write("grok", "Default model: grok-4.7\n\nAvailable models:\n  * grok-4.7 (default)\n  - grok-4.6\n")
	write("agy", "Fetching available models...\ngemini-x-high\tGemini X (High)\n")
	write("opencode", "opencode-go/glm-5.3\nopencode-go/kimi-k3\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", t.TempDir())

	ids := func(typ catwalk.Type) (out []string) {
		models, err := cliDiscovery[typ](t.Context())
		require.NoError(t, err)
		for _, m := range models {
			out = append(out, m.ID+"="+m.Name)
		}
		return out
	}
	require.Equal(t, []string{"grok-4.7=grok-4.7", "grok-4.6=grok-4.6"}, ids(TypeGrokCLI))
	require.Equal(t, []string{"gemini-x-high=Gemini X (High)"}, ids(TypeAGYCLI))
	require.Equal(t, []string{"opencode-go/glm-5.3=glm-5.3", "opencode-go/kimi-k3=kimi-k3"}, ids(TypeOpenCodeCLI))
}

func TestCodexImageCapabilities(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	script := `#!/bin/sh
read -r line
read -r line
read -r line
echo '{"id":2,"result":{"data":[{"id":"gpt-6-astra","inputModalities":["text"]},{"id":"new-vision","inputModalities":["text","image"]},{"id":"legacy"},{"id":"hidden","hidden":true}]}}'
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	models, err := discoverCodex(t.Context())
	require.NoError(t, err)
	require.Len(t, models, 3)
	require.False(t, models[0].SupportsImages)
	require.True(t, models[1].SupportsImages)
	require.True(t, models[2].SupportsImages)
	// Refresh must not overwrite the catalog's explicit text-only capability
	// with the built-in fallback, and new capabilities must survive caching.
	refreshCLIModels(dir)
	models = cachedModels(string(TypeCodexCLI))
	require.Len(t, models, 3)
	require.False(t, models[0].SupportsImages)
	require.True(t, models[1].SupportsImages)
	require.True(t, models[2].SupportsImages)
	for _, provider := range cliProviders {
		if provider.cfg.Type == TypeCodexCLI {
			for _, model := range provider.cfg.Models {
				require.True(t, model.SupportsImages, model.ID)
			}
		}
	}
}
