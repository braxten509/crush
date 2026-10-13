package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A finished turn says so; "Crush is waiting..." is for questions and
// permission prompts.
func TestFinishedNotificationText(t *testing.T) {
	t.Parallel()
	n := finishedNotification("Phone beeps")
	require.Equal(t, "Crush finished", n.Title)
	require.Equal(t, `"Phone beeps" is done`, n.Message)
}
