package dialog

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

const (
	// RemoteID is the identifier for the Remote Control dialog.
	RemoteID             = "remote"
	remoteDialogMaxWidth = 56
)

// ActionRemoteOff stops sharing the window with the phone.
type ActionRemoteOff struct{}

// RemoteStatus is what the Remote Control dialog shows.
type RemoteStatus struct {
	Host   string
	Port   string
	Phones []string
}

// Remote shows the window's Remote Control share and turns it off.
type Remote struct {
	com    *common.Common
	help   help.Model
	status func() RemoteStatus
	// turnOff is true while the "Turn off" button is selected.
	turnOff bool
	keyMap  struct {
		LeftRight,
		Tab,
		EnterSpace,
		Close key.Binding
	}
}

var _ Dialog = (*Remote)(nil)

// NewRemote creates the Remote Control dialog. status is read on every draw
// so the phone list stays current.
func NewRemote(com *common.Common, status func() RemoteStatus) *Remote {
	r := &Remote{com: com, status: status}
	r.help = help.New()
	r.help.Styles = com.Styles.DialogHelpStyles()
	r.keyMap.LeftRight = key.NewBinding(
		key.WithKeys("left", "right"),
		key.WithHelp("←/→", "choose"),
	)
	r.keyMap.Tab = key.NewBinding(key.WithKeys("tab", "shift+tab"))
	r.keyMap.EnterSpace = key.NewBinding(
		key.WithKeys("enter", " "),
		key.WithHelp("enter", "confirm"),
	)
	r.keyMap.Close = CloseKey
	return r
}

// ID implements [Dialog].
func (*Remote) ID() string {
	return RemoteID
}

// HandleMsg implements [Dialog].
func (r *Remote) HandleMsg(msg tea.Msg) Action {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(msg, r.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, r.keyMap.LeftRight, r.keyMap.Tab):
			r.turnOff = !r.turnOff
		case key.Matches(msg, r.keyMap.EnterSpace):
			if r.turnOff {
				return ActionRemoteOff{}
			}
			return ActionClose{}
		}
	}
	return nil
}

// Draw implements [Dialog].
func (r *Remote) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := r.com.Styles
	width := max(0, min(remoteDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	innerWidth := max(0, width-t.Dialog.View.GetHorizontalFrameSize())
	st := r.status()

	const labelWidth = 10
	valueWidth := max(0, innerWidth-labelWidth-2)
	row := func(label, value string) string {
		return " " + t.Dialog.Permissions.KeyText.Width(labelWidth).Render(label) +
			t.Dialog.Permissions.ValueText.Render(ansi.Truncate(value, valueWidth, "…"))
	}
	phones := "none yet"
	if len(st.Phones) > 0 {
		phones = strings.Join(st.Phones, ", ")
	}
	body := []string{
		" " + t.Dialog.OAuth.Success.Render("On. Your phone can see this chat."),
		"",
		row("Computer", st.Host+" · port "+st.Port),
		row("Phones", phones),
		"",
		t.Dialog.SecondaryText.Render(ansi.Truncate("Open Pocket Agents › PC on your phone.", innerWidth-2, "…")),
		t.Dialog.SecondaryText.Render(ansi.Truncate("Only your own Tailscale devices can connect.", innerWidth-2, "…")),
		"",
	}
	buttons := common.ButtonGroup(t, []common.ButtonOpts{
		{Text: "Turn off", Selected: r.turnOff, Padding: 2},
		{Text: "Keep on", Selected: !r.turnOff, Padding: 2},
	}, " ")
	body = append(body, lipgloss.PlaceHorizontal(innerWidth, lipgloss.Center, buttons))

	rc := NewRenderContext(t, width)
	rc.Title = "Remote Control"
	rc.AddPart(lipgloss.JoinVertical(lipgloss.Left, body...))
	rc.Help = renderDialogHelp(t, &r.help, r, innerWidth)
	DrawCenter(scr, area, rc.Render())
	return nil
}

// ShortHelp implements [help.KeyMap].
func (r *Remote) ShortHelp() []key.Binding {
	return []key.Binding{r.keyMap.LeftRight, r.keyMap.EnterSpace, r.keyMap.Close}
}

// FullHelp implements [help.KeyMap].
func (r *Remote) FullHelp() [][]key.Binding {
	return [][]key.Binding{r.ShortHelp()}
}
