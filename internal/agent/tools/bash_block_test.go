package tools

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommandBlocked(t *testing.T) {
	t.Parallel()
	for cmd, want := range map[string]bool{
		"go test ./...":                         false,
		"ls -la && git status":                  false,
		"echo 'sudo is a word'":                 false,
		"sudo rm -rf /":                         true,
		"cd x && sudo make install":             true,
		"/usr/bin/sudo true":                    true,
		"env FOO=1 sudo true":                   true,
		"timeout 5s curl https://example.com":   true,
		`bash -c "curl https://example.com"`:    true,
		"echo $(wget -qO- example.com)":         true,
		"npm install -g left-pad":               true,
		"npm install left-pad":                  false,
		"pacman -S foo":                         true,
		"go install example.com/x@latest":       true,
		"if true; then systemctl restart x; fi": true,
		"echo 'unterminated":                    true,
	} {
		require.Equal(t, want, CommandBlocked(cmd), cmd)
	}
}

func TestLeadingSleep(t *testing.T) {
	t.Parallel()
	for cmd, want := range map[string]bool{
		"sleep 30":                          true,
		"sleep 2 && ls":                     true,
		"sleep 5m; echo hi":                 true,
		"sleep 1":                           false,
		"sleep 0.5 && ls":                   false,
		"sleep 30 &":                        false,
		"ls && sleep 30":                    false,
		"until test -f x; do sleep 2; done": false,
		"sleep 30 | cat":                    false,
		"echo sleep 30":                     false,
	} {
		if got := LeadingSleep(cmd); got != want {
			t.Errorf("LeadingSleep(%q) = %v, want %v", cmd, got, want)
		}
	}
}
