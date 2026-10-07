package dialog

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/ui/common"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/dustin/go-humanize"
)

const UsageID = "usage"

type ActionRefreshUsage struct{}

// UsageData is the selected CLI's cached subscription and context snapshot.
type UsageData struct {
	Kind                     catwalk.Type
	Provider, Model, ModelID string
	Limits                   []cliagent.Limit
	Context, CompactAt       int64
	HasContext, Estimated    bool
	Refreshing               bool
	Error                    string
	CheckedAt                time.Time
}

type Usage struct {
	com               *common.Common
	help              help.Model
	refresh, scroll   key.Binding
	data              UsageData
	offset, maxOffset int
	now               func() time.Time
}

func NewUsage(com *common.Common, data UsageData) *Usage {
	d := &Usage{com: com, data: data, now: time.Now}
	d.help = help.New()
	d.help.Styles = com.Styles.DialogHelpStyles()
	d.refresh = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh"))
	d.scroll = key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "scroll"))
	return d
}

func (d *Usage) ID() string             { return UsageID }
func (d *Usage) Data() UsageData        { return d.data }
func (d *Usage) SetData(data UsageData) { d.data = data }

func (d *Usage) HandleMsg(msg tea.Msg) Action {
	if msg, ok := msg.(tea.MouseWheelMsg); ok {
		if msg.Button == uv.MouseWheelUp {
			d.offset = max(0, d.offset-3)
		}
		if msg.Button == uv.MouseWheelDown {
			d.offset = min(d.maxOffset, d.offset+3)
		}
	}
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(msg, CloseKey):
			return ActionClose{}
		case key.Matches(msg, d.refresh):
			if d.data.Kind != "" && !d.data.Refreshing {
				return ActionRefreshUsage{}
			}
		case msg.String() == "up" || msg.String() == "k":
			d.offset = max(0, d.offset-1)
		case msg.String() == "down" || msg.String() == "j":
			d.offset = min(d.maxOffset, d.offset+1)
		case msg.String() == "pgup":
			d.offset = max(0, d.offset-5)
		case msg.String() == "pgdown":
			d.offset = min(d.maxOffset, d.offset+5)
		}
	}
	return nil
}

func (d *Usage) body() string {
	t, data, now := d.com.Styles, d.data, d.now()
	lines := []string{t.ComposerFooter.Model.Render(data.Provider), t.ComposerFooter.Text.Render(data.Model), ""}
	if data.Kind == "" {
		lines = append(lines, "Subscription usage is available for CLI-backed models.")
	} else if len(data.Limits) == 0 && !data.Refreshing {
		lines = append(lines, "No usage windows reported by this CLI.")
	}
	for _, limit := range data.Limits {
		used := max(0, min(100, limit.Used))
		lines = append(lines, t.ComposerFooter.Model.Render(limit.Name),
			t.ComposerFooter.Accent.Render(fmt.Sprintf("%.1f%% used · %.1f%% left", used, 100-used)))
		if limit.Model != "" {
			lines = append(lines, "Model: "+limit.Model)
		}
		if limit.ResetsAt.IsZero() {
			lines = append(lines, "Reset time not reported")
		} else {
			lines = append(lines, "Resets: "+limit.ResetsAt.Local().Format("Mon, Jan 2, 2006 · 3:04:05 PM MST (UTC-07:00)"))
			if left := limit.ResetsAt.Sub(now); left > 0 {
				lines = append(lines, "In: "+usageCountdown(left))
			} else {
				lines = append(lines, "Reset time passed; percentages above are last reported.")
			}
		}
		lines = append(lines, "")
	}
	if data.HasContext {
		label := "Context"
		if data.Estimated {
			label += " (estimated)"
		}
		lines = append(lines, t.ComposerFooter.Model.Render(label))
		if data.CompactAt > 0 {
			lines = append(lines,
				fmt.Sprintf("%s / %s tokens before compaction", humanize.Comma(data.Context), humanize.Comma(data.CompactAt)),
				fmt.Sprintf("%s tokens remaining", humanize.Comma(max(int64(0), data.CompactAt-data.Context))), "")
		} else {
			lines = append(lines, fmt.Sprintf("%s tokens used", humanize.Comma(data.Context)), "Compaction limit not reported", "")
		}
	}
	switch {
	case data.Refreshing:
		lines = append(lines, "Refreshing usage…")
	case data.Error != "":
		lines = append(lines, t.LSP.WarningDiagnostic.Render("Refresh failed: "+data.Error))
	case !data.CheckedAt.IsZero():
		lines = append(lines, "Checked: "+data.CheckedAt.Local().Format("3:04:05 PM MST"))
	}
	return strings.Join(lines, "\n")
}

func usageCountdown(d time.Duration) string {
	seconds := int64(d.Round(time.Second) / time.Second)
	days := seconds / 86400
	clock := fmt.Sprintf("%02dh %02dm %02ds", seconds/3600%24, seconds/60%60, seconds%60)
	if days > 0 {
		return fmt.Sprintf("%dd %s", days, clock)
	}
	return clock
}

func (d *Usage) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(78, area.Dx()))
	inner := max(0, width-t.Dialog.View.GetHorizontalFrameSize())
	rc := NewRenderContext(t, width)
	rc.Title = "Usage"
	d.refresh.SetEnabled(d.data.Kind != "" && !d.data.Refreshing)
	rc.Help = renderDialogHelp(t, &d.help, d, inner)
	lines := strings.Split(ansi.Wrap(d.body(), max(1, inner-2), ""), "\n")
	available := max(1, area.Dy()-t.Dialog.View.GetVerticalFrameSize()-t.Dialog.Title.GetVerticalFrameSize()-1-lipgloss.Height(rc.Help))
	d.maxOffset = max(0, len(lines)-available)
	d.offset = min(d.offset, d.maxOffset)
	body := strings.Join(lines[d.offset:min(len(lines), d.offset+available)], "\n")
	rc.AddPart(lipgloss.NewStyle().PaddingLeft(1).Width(inner).Render(body))
	DrawCenter(scr, area, rc.Render())
	return nil
}

func (d *Usage) ShortHelp() []key.Binding  { return []key.Binding{CloseKey, d.refresh, d.scroll} }
func (d *Usage) FullHelp() [][]key.Binding { return [][]key.Binding{d.ShortHelp()} }
