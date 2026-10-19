package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// TestRemotePasswordStdin: with --stdin the password comes as two lines
// on standard input, with the same checks and texts as typed (G-20).
func TestRemotePasswordStdin(t *testing.T) {
	t.Setenv("CRUSH_GLOBAL_DATA", t.TempDir())
	require.NoError(t, remotePasswordCmd.Flags().Set("stdin", "true"))
	t.Cleanup(func() { _ = remotePasswordCmd.Flags().Set("stdin", "false") })
	run := func(input string) (string, error) {
		var out bytes.Buffer
		remotePasswordCmd.SetIn(strings.NewReader(input))
		remotePasswordCmd.SetOut(&out)
		remotePasswordCmd.SetErr(&out)
		t.Cleanup(func() {
			remotePasswordCmd.SetIn(nil)
			remotePasswordCmd.SetOut(nil)
			remotePasswordCmd.SetErr(nil)
		})
		err := remotePasswordCmd.RunE(remotePasswordCmd, nil)
		return out.String(), err
	}

	_, err := run("correct horse\nwrong horse\n")
	require.EqualError(t, err, "the two passwords don't match; nothing changed")
	_, err = run("short\nshort\n")
	require.EqualError(t, err, "the password needs at least 8 characters")
	_, err = run("only one line\n")
	require.EqualError(t, err, "expected the password twice, one line each, on standard input")
	_, statErr := os.Stat(remotePasswordFile())
	require.True(t, os.IsNotExist(statErr), "nothing was saved")

	out, err := run("correct horse\r\ncorrect horse")
	require.NoError(t, err)
	require.Equal(t, "Password set. The phone asks for it before it opens a new Crush window here.\n", out)
	require.NotContains(t, out, "correct horse")
	hash, err := os.ReadFile(remotePasswordFile())
	require.NoError(t, err)
	require.NoError(t, bcrypt.CompareHashAndPassword(hash, []byte("correct horse")))
}
