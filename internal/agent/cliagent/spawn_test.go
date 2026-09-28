package cliagent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsSpawnCommand(t *testing.T) {
	t.Parallel()
	for cmd, want := range map[string]bool{
		`crush spawn --cli codex --name "Review (auth)" "check the auth code"`:   true,
		"crush spawn --cli codex --name x <<'EOF'\nreview $HOME and `this`\nEOF": true,
		"crush spawn --stop t2":                              true,
		"crush spawn --cli codex <<EOF\n$(rm -rf ~)\nEOF":    false,
		"crush spawn --cli codex x && rm -rf ~":              false,
		`crush spawn --cli codex "$(whoami)"`:                false,
		"crush spawn --cli codex x; ls":                      false,
		"crush spawn --cli codex <<'EOF'\nhi\nEOF\nrm -rf ~": false,
		"crush spawn --cli codex x\nrm -rf ~":                false,
		"crush spawner":                                      false,
		"crush spawn --cli codex x > /etc/passwd":            false,
		"crush spawn --cli a --name x <<'EOF'\nhi\nEOF\ncrush spawn --cli b <<'EOF'\nyo\nEOF": true,
		"crush spawn --cli a <<'EOF'\nhi\nEOF\nrm -rf ~\n":                                    false,
		"crush spawn --cli a <<'EOF'\nno end":                                                 false,
		"rm -rf ~ # crush spawn":                                                              false,
		"crush ask <<'EOF'\n{\"questions\":[{\"question\":\"$(x)?\"}]}\nEOF":                  true,
		"crush ask <<'EOF'\n{}\nEOF\nrm -rf ~":                                                false,
		"crush asker":                                                                         false,
	} {
		require.Equal(t, want, IsSpawnCommand(cmd), cmd)
	}
}
