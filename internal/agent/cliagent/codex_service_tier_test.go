package cliagent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/secretguard"
	"github.com/stretchr/testify/require"
)

func TestCodexServiceTier(t *testing.T) {
	for _, tier := range []string{"", "fast", "default"} {
		for _, resume := range []string{"", "existing-thread"} {
			t.Run(tier+"/"+resume, func(t *testing.T) {
				dir := t.TempDir()
				// Capture the real driver's requests, answering each only after
				// reading it so the test also covers the protocol handshake.
				script := `#!/bin/sh
printf '%s\n' "$@" > args.txt
read -r line
printf '%s\n' "$line" > requests.jsonl
echo '{"id":"1","result":{}}'
read -r line
printf '%s\n' "$line" >> requests.jsonl
read -r line
printf '%s\n' "$line" >> requests.jsonl
echo '{"id":"hooks","result":{"data":[]}}'
read -r line
printf '%s\n' "$line" >> requests.jsonl
echo '{"id":"2","result":{"thread":{"id":"th"}}}'
read -r line
printf '%s\n' "$line" >> requests.jsonl
echo '{"id":"3","result":{"turn":{"id":"tu"}}}'
echo '{"method":"turn/completed","params":{"turn":{"id":"tu","status":"completed"}}}'
cat >/dev/null
`
				require.NoError(t, os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o755))
				t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
				provider := NewProvider(config.TypeCodexCLI, dir, dir, nil, nil, tier, false, "", false)
				model, err := provider.LanguageModel(t.Context(), "gpt-6-astra")
				require.NoError(t, err)
				require.NoError(t, model.(*Model).Run(t.Context(), Turn{
					Prompt: "hi", Resume: resume, Effort: "max", Emit: func(Event) error { return nil },
				}))
				assertRequests := func(ephemeral bool) {
					t.Helper()
					args, err := os.ReadFile(filepath.Join(dir, "args.txt"))
					require.NoError(t, err)
					require.Contains(t, string(args), codexGuardHook())
					data, err := os.ReadFile(filepath.Join(dir, "requests.jsonl"))
					require.NoError(t, err)
					sent := map[string]map[string]any{}
					for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
						var request struct {
							Method string         `json:"method"`
							Params map[string]any `json:"params"`
						}
						require.NoError(t, json.Unmarshal([]byte(line), &request))
						sent[request.Method] = request.Params
					}
					require.Contains(t, sent, "hooks/list")
					threadMethod := "thread/start"
					if resume != "" && !ephemeral {
						threadMethod = "thread/resume"
					}
					for _, method := range []string{threadMethod, "turn/start"} {
						require.Contains(t, sent, method)
						params := sent[method]
						if tier == "" {
							require.NotContains(t, params, "serviceTier", "unset must inherit Codex's setting")
						} else {
							require.Equal(t, tier, params["serviceTier"])
						}
						if method == threadMethod {
							if threadMethod == "thread/resume" {
								require.Equal(t, resume, params["threadId"])
							}
							require.Equal(t, "gpt-6-astra", params["model"])
							if ephemeral {
								require.Equal(t, true, params["ephemeral"])
								require.Equal(t, "read-only", params["sandbox"])
							} else {
								require.Equal(t, "workspace-write", params["sandbox"])
								require.Equal(t, "on-request", params["approvalPolicy"])
							}
						} else if !ephemeral {
							require.Equal(t, "max", params["effort"])
						}
					}
				}
				assertRequests(false)
				// Titles and other text-only requests must carry the selected
				// small model's tier through the same provider as regular turns.
				_, err = model.Generate(t.Context(), fantasy.Call{})
				require.NoError(t, err)
				assertRequests(true)
			})
		}
	}
}

// Codex skips new hooks until they're trusted; Crush trusts only its own
// secrets guard, by the hash Codex reports.
func TestCodexGuardTrust(t *testing.T) {
	t.Parallel()
	list := func(source, command, status string) json.RawMessage {
		data, err := json.Marshal(map[string]any{"data": []any{map[string]any{"hooks": []any{map[string]any{
			"key": "/<session-flags>/config.toml:pre_tool_use:0:0", "command": command, "source": source,
			"currentHash": "sha256:abc", "trustStatus": status,
		}}}}})
		require.NoError(t, err)
		return data
	}
	trust := codexGuardTrust(list("sessionFlags", secretguard.HookCommand(), "untrusted"))
	require.NotNil(t, trust)
	require.Equal(t, "config/value/write", trust["method"])
	params := trust["params"].(map[string]any)
	require.Equal(t, `hooks.state."/<session-flags>/config.toml:pre_tool_use:0:0".trusted_hash`, params["keyPath"])
	require.Equal(t, "sha256:abc", params["value"])

	require.Nil(t, codexGuardTrust(list("sessionFlags", secretguard.HookCommand(), "trusted")))
	require.Nil(t, codexGuardTrust(list("sessionFlags", "some other hook", "untrusted")))
	require.Nil(t, codexGuardTrust(list("user", secretguard.HookCommand(), "untrusted")))
	require.Nil(t, codexGuardTrust(nil))
}
