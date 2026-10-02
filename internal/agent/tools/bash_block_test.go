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

func TestCommandBlockedResolvesShellFlagsAndWrapperOperands(t *testing.T) {
	t.Parallel()
	for command, blocked := range map[string]bool{
		`env -u VARIABLE cu\rl https://example.com`:                  true,
		`bash -lc -- 'curl https://example.com'`:                     true,
		`bash -c -l 'curl https://example.com'`:                      true,
		`bash -lc 'curl https://example.com'`:                        true,
		`bash -cl 'wget https://example.com'`:                        true,
		`zsh -fc 'sudo true'`:                                        true,
		`bash -o pipefail -lc 'curl https://example.com'`:            true,
		`fish --command='curl https://example.com'`:                  true,
		`env -u VARIABLE curl https://example.com`:                   true,
		`env --unset=VARIABLE curl https://example.com`:              true,
		`env -C /tmp curl https://example.com`:                       true,
		`env -u VARIABLE bash -lc 'curl https://example.com'`:        true,
		`exec -a harmless curl https://example.com`:                  true,
		`nice -n 5 curl https://example.com`:                         true,
		`timeout -s TERM -k 2s 5s curl https://example.com`:          true,
		`/usr/bin/time -f format -o output curl https://example.com`: true,
		`stdbuf -o L curl https://example.com`:                       true,
		`stdbuf -oL curl https://example.com`:                        true,
		`setsid --wait curl https://example.com`:                     true,
		`env -u VARIABLE --unset=OTHER go test ./...`:                false,
		`timeout -s TERM 5s bash -lc 'go test ./...'`:                false,
		`nice -n 5 go test ./...`:                                    false,
		`exec -a harmless go test ./...`:                             false,
		`bash -lc 'echo safe'`:                                       false,
		`command -v curl`:                                            false,
	} {
		require.Equal(t, blocked, CommandBlocked(command), command)
	}
}

func TestCommandBlockedFailsClosedForUnresolvedForms(t *testing.T) {
	t.Parallel()
	for _, command := range []string{
		`env --unknown VARIABLE curl https://example.com`,
		`env -S 'curl https://example.com'`,
		`env -u`,
		`timeout -s`,
		`bash -lc`,
		`bash --rcfile startup -c 'echo safe'`,
		`bash script.sh`,
		`bash -lc "$SCRIPT"`,
		`env -u "$VARIABLE" curl https://example.com`,
		`"$EXECUTABLE" https://example.com`,
		`xargs curl`,
	} {
		require.True(t, CommandBlocked(command), command)
	}
}

func TestLeadingSleep(t *testing.T) {
	t.Parallel()
	for cmd, want := range map[string]bool{
		"sleep 30":                          true,
		"sleep 2 && ls":                     false,
		"sleep 3":                           false,
		"sleep 10":                          false,
		"sleep 10s && ls":                   false,
		"sleep 10.001":                      true,
		"sleep 11":                          true,
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
