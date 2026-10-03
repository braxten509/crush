package cliagent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/config"
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
echo '{"id":"2","result":{"thread":{"id":"th"}}}'
read -r line
printf '%s\n' "$line" >> requests.jsonl
echo '{"id":"3","result":{"turn":{"id":"tu"}}}'
echo '{"method":"turn/completed","params":{"turn":{"id":"tu","status":"completed"}}}'
cat >/dev/null
`
				require.NoError(t, os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o755))
				t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
				limit := int64(400000)
				if resume != "" {
					limit = 240000
				}
				provider := NewProvider(config.TypeCodexCLI, dir, dir, nil, nil, tier, false, limit)
				model, err := provider.LanguageModel(t.Context(), "gpt-6-astra")
				require.NoError(t, err)
				require.NoError(t, model.(*Model).Run(t.Context(), Turn{
					Prompt: "hi", Resume: resume, Effort: "max", Emit: func(Event) error { return nil },
				}))
				assertRequests := func(ephemeral bool) {
					t.Helper()
					args, err := os.ReadFile(filepath.Join(dir, "args.txt"))
					require.NoError(t, err)
					require.Contains(t, string(args), fmt.Sprintf("model_auto_compact_token_limit=%d", limit))
					require.NotContains(t, string(args), "model_auto_compact_token_limit=160000")
					require.NotContains(t, string(args), "model_context_window=")
					data, err := os.ReadFile(filepath.Join(dir, "requests.jsonl"))
					require.NoError(t, err)
					lines := strings.Split(strings.TrimSpace(string(data)), "\n")
					require.Len(t, lines, 4)
					for _, index := range []int{2, 3} {
						var request struct {
							Method string         `json:"method"`
							Params map[string]any `json:"params"`
						}
						require.NoError(t, json.Unmarshal([]byte(lines[index]), &request))
						if tier == "" {
							require.NotContains(t, request.Params, "serviceTier", "unset must inherit Codex's setting")
						} else {
							require.Equal(t, tier, request.Params["serviceTier"])
						}
						if index == 2 {
							method := "thread/start"
							if resume != "" && !ephemeral {
								method = "thread/resume"
								require.Equal(t, resume, request.Params["threadId"])
							}
							require.Equal(t, method, request.Method)
							require.Equal(t, "gpt-6-astra", request.Params["model"])
							if ephemeral {
								require.Equal(t, true, request.Params["ephemeral"])
								require.Equal(t, "read-only", request.Params["sandbox"])
							} else {
								require.Equal(t, "workspace-write", request.Params["sandbox"])
								require.Equal(t, "on-request", request.Params["approvalPolicy"])
							}
						} else {
							require.Equal(t, "turn/start", request.Method)
							if !ephemeral {
								require.Equal(t, "max", request.Params["effort"])
							}
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
