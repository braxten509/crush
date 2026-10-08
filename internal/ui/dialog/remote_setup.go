package dialog

import (
	"image"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/remote"
	"github.com/charmbracelet/crush/internal/ui/common"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

const RemoteSetupID = "remote_setup"

type ActionRemoteSetup struct{ Action string }
type ActionRemoteSetupClose struct{}

// RemoteSetup keeps the same content edge and button slots in every state.
// Only an explicit button press may install software or start a login.
type RemoteSetup struct {
	com      *common.Common
	info     remote.SetupInfo
	checking bool
	selected int
	buttons  []image.Rectangle
}

func NewRemoteSetup(com *common.Common) *RemoteSetup {
	return &RemoteSetup{com: com, checking: true}
}

func (*RemoteSetup) ID() string { return RemoteSetupID }

func (r *RemoteSetup) SetInfo(info remote.SetupInfo) {
	if r.info.State != info.State {
		r.selected = 0
	}
	r.info, r.checking = info, false
}

func (r *RemoteSetup) choices() []string {
	if r.checking {
		return []string{"Cancel"}
	}
	if r.info.Action == "" {
		return []string{"Check again", "Cancel"}
	}
	return []string{r.info.Button, "Check again", "Cancel"}
}

func (r *RemoteSetup) activate() Action {
	choices := r.choices()
	if r.selected == len(choices)-1 {
		return ActionRemoteSetupClose{}
	}
	if r.selected == 0 && r.info.Action != "" {
		return ActionRemoteSetup{Action: r.info.Action}
	}
	return ActionRemoteSetup{Action: "check"}
}

func (r *RemoteSetup) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, CloseKey):
			return ActionRemoteSetupClose{}
		case msg.String() == "shift+tab":
			r.selected = (r.selected + len(r.choices()) - 1) % len(r.choices())
		case msg.Code == tea.KeyTab || msg.Code == tea.KeyRight || msg.Code == tea.KeyDown:
			r.selected = (r.selected + 1) % len(r.choices())
		case msg.Code == tea.KeyLeft || msg.Code == tea.KeyUp:
			r.selected = (r.selected + len(r.choices()) - 1) % len(r.choices())
		case msg.Code == tea.KeyEnter || msg.Text == " ":
			return r.activate()
		}
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			for i, rect := range r.buttons {
				if image.Pt(msg.X, msg.Y).In(rect) {
					r.selected = i
					return r.activate()
				}
			}
		}
	}
	return nil
}

func (r *RemoteSetup) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := r.com.Styles
	width := max(1, min(remoteDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	inner := max(1, width-t.Dialog.View.GetHorizontalFrameSize())
	title, detail := r.info.Title, r.info.Detail
	if r.checking {
		title, detail = "Checking Tailscale…", "Looking for a private connection to Pocket Agents."
	}
	wrap := func(s string) string { return ansi.Wrap(s, max(1, inner-2), "") }
	body := []string{
		t.Dialog.PrimaryText.Render(wrap(title)), "",
		t.Dialog.SecondaryText.Render(wrap(detail)), "",
	}
	// Keep a stable minimum content height while checks update the state.
	text := strings.Join(body, "\n")
	text += strings.Repeat("\n", max(0, 7-lipgloss.Height(text)))
	choices := r.choices()
	buttons := make([]string, len(choices))
	for i, label := range choices {
		buttons[i] = common.ButtonGroup(t, []common.ButtonOpts{{Text: label, Selected: i == r.selected, Padding: 1}}, "")
	}
	buttonRow := strings.Join(buttons, " ")
	stacked := lipgloss.Width(buttonRow) > inner
	if stacked {
		buttonRow = strings.Join(buttons, "\n")
	}
	buttonLine := lipgloss.Height(text)
	rc := NewRenderContext(t, width)
	rc.Title = "Remote Control"
	rc.AddPart(text + "\n" + lipgloss.PlaceHorizontal(inner, lipgloss.Center, buttonRow))
	rc.Help = t.Dialog.HelpView.Render(ansi.Truncate("←/→ choose · enter confirm · esc cancel", max(0, inner-t.Dialog.HelpView.GetHorizontalFrameSize()), ""))
	view := rc.Render()
	DrawCenter(scr, area, view)
	// Hit boxes use the rendered frame geometry, including its title line.
	frameWidth, frameHeight := lipgloss.Width(view), lipgloss.Height(view)
	center := common.CenterRect(area, frameWidth, frameHeight)
	x := center.Min.X + t.Dialog.View.GetMarginLeft() + t.Dialog.View.GetBorderLeftSize() + t.Dialog.View.GetPaddingLeft()
	y := center.Min.Y + t.Dialog.View.GetMarginTop() + t.Dialog.View.GetBorderTopSize() + t.Dialog.View.GetPaddingTop() + 1 + t.Dialog.Title.GetVerticalFrameSize() + buttonLine
	r.buttons = nil
	if frameWidth > area.Dx() || frameHeight > area.Dy() {
		return nil
	}
	if stacked {
		for _, button := range buttons {
			left := x + (inner-lipgloss.Width(button)+1)/2
			r.buttons = append(r.buttons, image.Rect(left, y, left+lipgloss.Width(button), y+lipgloss.Height(button)))
			y += lipgloss.Height(button)
		}
	} else {
		x += (inner - lipgloss.Width(buttonRow) + 1) / 2
		for _, button := range buttons {
			r.buttons = append(r.buttons, image.Rect(x, y, x+lipgloss.Width(button), y+lipgloss.Height(button)))
			x += lipgloss.Width(button) + 1
		}
	}
	return nil
}
