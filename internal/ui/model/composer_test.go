package model

import (
	"testing"

	"charm.land/bubbles/v2/textarea"
	"github.com/stretchr/testify/require"
)

func TestComposerPlaceholder(t *testing.T) {
	t.Parallel()

	c := composer{readyPlaceholder: "Ready!", workingPlaceholder: "Working!"}
	require.Equal(t, "Ready!", c.placeholder(false, false, false))
	require.Equal(t, "Working!", c.placeholder(true, false, false))
	require.Equal(t, "Let's plan", c.placeholder(false, true, false))
	require.Equal(t, "Working!", c.placeholder(true, true, false), "busy wins over planning")
	require.Equal(t, "Go crazy", c.placeholder(true, false, true), "YOLO wins over busy")
	require.Equal(t, "Let's plan", c.placeholder(false, true, true), "planning ignores YOLO")

	c.bangMode = true
	require.Equal(t, "Run a shell command", c.placeholder(true, true, true))
}

func TestComposerHistoryDraft(t *testing.T) {
	t.Parallel()

	c := composer{textarea: textarea.New()}
	c.promptHistory.index = 2
	c.textarea.SetValue("new")
	c.updateHistoryDraft("new")
	require.Equal(t, 2, c.promptHistory.index, "no change, no draft")
	c.updateHistoryDraft("old")
	require.Equal(t, "new", c.promptHistory.draft)
	require.Equal(t, -1, c.promptHistory.index)

	c.historyReset()
	require.Empty(t, c.promptHistory.draft)
	require.Equal(t, -1, c.promptHistory.index)
}
