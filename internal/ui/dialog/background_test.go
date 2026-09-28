package dialog

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestBackgroundDialogListsItems(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	com := &common.Common{Styles: &sty}
	b := NewBackground(com,
		[]agent.Task{{ID: "t1", Name: "Slow task", CLI: "claude", Model: "haiku", Status: agent.TaskRunning, Started: time.Now()}},
		[]agent.Process{{PID: 42, Command: "sleep 900", Started: time.Now()}}, "")
	scr := uv.NewScreenBuffer(120, 30)
	b.Draw(scr, scr.Bounds())
	out := ansi.Strip(scr.Render())
	require.True(t, strings.Contains(out, "Slow task"), out)
	require.True(t, strings.Contains(out, "sleep 900"), out)
}
