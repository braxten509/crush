package model

import (
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/memwatch"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/stretchr/testify/require"
)

func TestMemoryWarningStaysWhileHighAndClears(t *testing.T) {
	t.Parallel()
	m := newFrameTestUI(t)
	high := memwatch.Status{Percent: 88, High: true, Top: []memwatch.Program{{Name: "node", Bytes: 6 << 30}}}

	require.NotNil(t, m.handleMemoryCheck(memoryCheckMsg{status: high}))
	require.Equal(t, util.InfoTypeWarn, m.status.msg.Type)
	require.Equal(t, high.Message(), m.status.msg.Msg)

	// Another message is left alone while it shows...
	m.status.SetInfoMsg(util.NewInfoMsg("Copied"))
	m.handleMemoryCheck(memoryCheckMsg{status: high})
	require.Equal(t, "Copied", m.status.msg.Msg)

	// ...and the warning comes back once it has expired.
	m.status.ClearInfoMsg()
	high.Percent = 90
	m.handleMemoryCheck(memoryCheckMsg{status: high})
	require.Equal(t, high.Message(), m.status.msg.Msg)

	m.handleMemoryCheck(memoryCheckMsg{status: memwatch.Status{Percent: 70}})
	require.True(t, m.status.msg.IsEmpty())
	require.False(t, m.memoryHigh)
}

func TestMemoryLowDoesNotClearOtherMessages(t *testing.T) {
	t.Parallel()
	m := newFrameTestUI(t)
	m.status.SetInfoMsg(util.NewInfoMsg("Copied"))
	m.handleMemoryCheck(memoryCheckMsg{status: memwatch.Status{Percent: 40}})
	require.Equal(t, "Copied", m.status.msg.Msg)
}

func TestMemoryPopupRespectsDisabledNotifications(t *testing.T) {
	t.Parallel()
	m := newFrameTestUI(t)
	cfg := m.com.Config()
	require.NotNil(t, cfg)
	if cfg.Options == nil {
		cfg.Options = &config.Options{}
	}
	cfg.Options.Notifications = "disabled"
	require.Nil(t, m.memoryPopup(memwatch.Status{Percent: 88, High: true}))
}
