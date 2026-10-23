package config

import (
	"os"
	"path/filepath"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/home"
	"github.com/stretchr/testify/require"
)

func TestSharedInstructionsUseOnlySharedSkills(t *testing.T) {
	cfg := &Config{Options: &Options{DisableInstructionFiles: true, SkillsPaths: []string{"project-skills"}}}
	cfg.setDefaults(t.TempDir(), t.TempDir())
	require.Equal(t, []string{filepath.Join(home.Dir(), ".agents", "skills")}, cfg.Options.SkillsPaths)
}

func TestAddCLIProviders(t *testing.T) {
	detectCLIs = true
	t.Cleanup(func() { detectCLIs = false })

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "abacusai"), []byte("#!/bin/sh\n"), 0o755))

	cfg := &Config{Providers: csync.NewMapFrom(map[string]ProviderConfig{
		string(TypeCodexCLI): {Disable: true},
	})}
	cfg.addCLIProviders(dir)
	_, abacusRegistered := cfg.Providers.Get("abacus-cli")
	require.False(t, abacusRegistered)

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

func TestOpenCodeZenKeepsOnlyFreeModels(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
if [ "$2" = "opencode" ]; then
printf 'opencode/paid-model\n{"id":"paid-model","name":"Paid Model","cost":{"input":1,"output":2}}\n'
printf 'opencode/free-model\n{"id":"free-model","name":"Free Model","cost":{"input":0,"output":0},"variants":{"low":{"reasoningEffort":"low"},"high":{"reasoningEffort":"high"},"max":{"reasoningEffort":"max"}}}\n'
fi
if [ "$2" = "opencode-go" ]; then
printf 'opencode-go/go-only\n{"id":"go-only","name":"go-only","cost":{"input":1,"output":2}}\n'
fi
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "opencode"), []byte(script), 0o755))
	ids := func(typ catwalk.Type) (out []string) {
		models, err := cliDiscovery[typ](t.Context(), filepath.Join(dir, "opencode"))
		require.NoError(t, err)
		for _, m := range models {
			out = append(out, m.ID+"="+m.Name)
		}
		return out
	}
	require.Equal(t, []string{"opencode/free-model=Free Model"}, ids(TypeOpenCodeZenCLI))
	require.Equal(t, []string{"opencode-go/go-only=go-only"}, ids(TypeOpenCodeCLI))

	models, err := cliDiscovery[TypeOpenCodeZenCLI](t.Context(), filepath.Join(dir, "opencode"))
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, []string{"low", "high", "max"}, models[0].ReasoningLevels)
	require.Equal(t, "low", models[0].DefaultReasoningEffort)
	require.True(t, models[0].CanReason)
}

func TestOpenCodeZenRegistersAsFreeProvider(t *testing.T) {
	detectCLIs = true
	t.Cleanup(func() { detectCLIs = false })
	isolateModelCache(t)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "opencode"), []byte("#!/bin/sh\n"), 0o755))

	cfg := &Config{Providers: csync.NewMapFrom(map[string]ProviderConfig{})}
	cfg.addCLIProviders(dir)

	zen, ok := cfg.Providers.Get("opencode-zen-cli")
	require.True(t, ok, "opencode on PATH registers Zen beside Go")
	require.Equal(t, "OpenCode Zen", zen.Name)
	require.Equal(t, TypeOpenCodeZenCLI, zen.Type)
	require.NotEmpty(t, zen.Models)
	require.Equal(t, "opencode/deepseek-v4.1-flash-free", zen.Models[0].ID)
	var ids []string
	for _, model := range zen.Models {
		ids = append(ids, model.ID)
	}
	require.Equal(t, []string{
		"opencode/deepseek-v4.1-flash-free",
		"opencode/beam-free",
		"opencode/big-pickle",
		"opencode/flint-40b-free",
		"opencode/glm-5.3-flash-free",
		"opencode/mimo-v2.6-flash-free",
		"opencode/nemotron-3-ultra-free",
		"opencode/nemotron-3.5-lightning-free",
		"opencode/nemotron-3.5-super-vl-free",
	}, ids, "Zen lists free models only")

	goProvider, ok := cfg.Providers.Get(string(TypeOpenCodeCLI))
	require.True(t, ok)
	require.Equal(t, "OpenCode Go", goProvider.Name)
}

func TestListModels(t *testing.T) {
	dir := t.TempDir()
	write := func(bin, out string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, bin), []byte("#!/bin/sh\ncat <<'EOF'\n"+out+"EOF\n"), 0o755))
	}
	write("agy", "Fetching available models...\ngemini-x-high\tGemini X (High)\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "opencode"), []byte(`#!/bin/sh
printf 'opencode-go/glm-5.3\n{"id":"glm-5.3","name":"glm-5.3","status":"active","cost":{"input":1.4,"output":4.4},"limit":{"context":200000,"output":32000}}\n'
printf 'opencode-go/kimi-k3\n{"id":"kimi-k3","name":"kimi-k3","status":"active","cost":{"input":3,"output":15},"limit":{"context":1000000,"output":128000}}\n'
`), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", t.TempDir())

	ids := func(typ catwalk.Type, bin string) (out []string) {
		models, err := cliDiscovery[typ](t.Context(), filepath.Join(dir, bin))
		require.NoError(t, err)
		for _, m := range models {
			out = append(out, m.ID+"="+m.Name)
		}
		return out
	}
	require.Equal(t, []string{"gemini-x-high=Gemini X (High)"}, ids(TypeAGYCLI, "agy"))
	require.Equal(t, []string{"opencode-go/glm-5.3=glm-5.3", "opencode-go/kimi-k3=kimi-k3"}, ids(TypeOpenCodeCLI, "opencode"))

	goModels, err := cliDiscovery[TypeOpenCodeCLI](t.Context(), filepath.Join(dir, "opencode"))
	require.NoError(t, err)
	require.Equal(t, int64(200_000), goModels[0].ContextWindow)
	require.Equal(t, int64(1_000_000), goModels[1].ContextWindow)
}

func TestCodexImageCapabilities(t *testing.T) {
	dir := t.TempDir()
	isolateModelCache(t)
	script := `#!/bin/sh
read -r line
read -r line
read -r line
echo '{"id":2,"result":{"data":[{"id":"gpt-6-astra","inputModalities":["text"]},{"id":"new-vision","inputModalities":["text","image"]},{"id":"legacy"},{"id":"hidden","hidden":true}]}}'
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	models, err := discoverCodex(t.Context(), filepath.Join(dir, "codex"))
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
	require.NotEmpty(t, models[0].ReasoningLevels, "older CLI metadata without effort fields keeps the known fallback")
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
