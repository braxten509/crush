package secretguard

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBlocksChatFolder(t *testing.T) {
	t.Parallel()
	own := "/home/u/.cache/crush-chats/abc"
	bash := func(command string) []byte {
		data, _ := json.Marshal(map[string]string{"command": command})
		return data
	}
	for _, command := range []string{
		"ls /home/u/.cache/crush-chats/abc",
		"cat /home/u/.cache/crush-chats/abc/a.png",
		"ls $CRUSH_CHAT_DIR",
		"ls ~/.cache/go-build",
	} {
		require.False(t, BlocksChatFolder("bash", bash(command), own), command)
	}
	for _, command := range []string{
		"cat /home/u/.cache/crush-chats/xyz/a.png",
		"ls ~/.cache/crush-chats",
		"ls ~/.cache/crush-chats/abcd",
		"ls ~/.cache/crush-chats/abc/../xyz",
		"ls $CRUSH_CHAT_DIR/../xyz",
		"ls ~/.cache/cr*",
		"cd ~/.cache && ls crush-chats",
	} {
		require.True(t, BlocksChatFolder("bash", bash(command), own), command)
	}
	// Outside a chat, every chat folder is off limits.
	require.True(t, BlocksChatFolder("bash", bash("ls "+own), ""))
	// Writes are checked by their path only, so code may name the folder.
	write, _ := json.Marshal(map[string]string{"file_path": "/src/x.go", "content": `"crush-chats"`})
	require.False(t, BlocksChatFolder("Write", write, own))
	write, _ = json.Marshal(map[string]string{"file_path": "/home/u/.cache/crush-chats/xyz/a", "content": "x"})
	require.True(t, BlocksChatFolder("Write", write, own))
}
