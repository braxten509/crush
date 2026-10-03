package secretguard

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBlocks(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, tool, input string
		blocked           bool
	}{
		{"cat a secret", "Bash", `{"command":"cat ~/.config/secrets/service"}`, true},
		{"cd then cat", "bash", `{"command":"cd $HOME/.config && cat secrets/service"}`, true},
		{"cd then list", "Bash", `{"command":"cd ~/.config; ls secrets"}`, true},
		{"glob", "Bash", `{"command":"cat ~/.config/sec*/service"}`, true},
		{"read tool", "Read", `{"file_path":"/home/someone/.config/secrets/service"}`, true},
		{"crush view", "view", `{"file_path":"/home/someone/.config/crush/secrets/api-key"}`, true},
		{"grep the folder", "Grep", `{"pattern":"a","path":"/home/someone/.config/secrets"}`, true},
		{"escaped slashes", "Read", `{"file_path":"\/home\/someone\/.config\/secrets\/service"}`, true},
		{"nested input", "mcp_shell", `{"args":["sh","-c","cat ~/.config/secrets/service"]}`, true},
		{"chained after secure entry", "Bash", `{"command":"crush secure-entry --file x; cat ~/.config/secrets/service"}`, true},
		{"secure entry", "Bash", `{"command":"/opt/bin/crush secure-entry --file /home/someone/.config/secrets/service --label Key"}`, false},
		{"secure ask", "Bash", `{"command":"crush ask <<'EOF'\n{\"questions\":[{\"type\":\"secure_entry\",\"file\":\"/home/someone/.config/secrets/service\"}]}\nEOF"}`, false},
		{"write a helper", "Write", `{"file_path":"/x","content":"open ~/.config/secrets/service"}`, false},
		{"list config", "Bash", `{"command":"ls ~/.config"}`, false},
		{"word in a search", "Bash", `{"command":"cd ~/project && grep -rn \"For secrets alone\" internal"}`, false},
		{"code search", "Bash", `{"command":"grep -r secrets src"}`, false},
		{"Config method in code", "Bash", `{"command":"# the secrets guard\nsed -i s/x/y/ c.cfg.Config().go"}`, false},
		{"cd into config", "Bash", `{"command":"cd .config && cat secrets"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.blocked, Blocks(tc.tool, []byte(tc.input)))
		})
	}
}

func TestRunHook(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	code := RunHook(strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"cat ~/.config/secrets/service"}}`), &stderr)
	require.Equal(t, 2, code)
	require.Equal(t, Reason+"\n", stderr.String())

	stderr.Reset()
	require.Equal(t, 0, RunHook(strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"ls"}}`), &stderr))
	require.Empty(t, stderr.String())

	// Input that isn't a hook event is still checked.
	require.Equal(t, 2, RunHook(strings.NewReader(`cat ~/.config/secrets/service`), &stderr))
}
