package model

import (
	"image"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/completions"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

// These tests pin which part of the UI gets a key, mouse event or paste
// when several could take it. The order is: the secure entry, the
// sub-agent view, the quit key, dialogs, the inline editor (Tab toggles
// it), the background task row, cancel and interrupt while busy, then the
// focused pane: completions, attachments and the editor, or the chat, or
// the sidebar.

// recDialog is a dialog that records the messages it gets.
type recDialog struct {
	id   string
	msgs []tea.Msg
}

func (d *recDialog) ID() string { return d.id }

func (d *recDialog) HandleMsg(msg tea.Msg) dialog.Action {
	d.msgs = append(d.msgs, msg)
	return nil
}

func (d *recDialog) Draw(uv.Screen, uv.Rectangle) *tea.Cursor { return nil }

// recInline is an inline editor that records the input it gets.
type recInline struct {
	keys    []string
	pastes  []string
	clicks  int
	wheel   float64
	focused bool
	done    bool
}

func (e *recInline) HandleKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	e.keys = append(e.keys, msg.String())
	return e.done, nil
}
func (e *recInline) ShortHelp() []key.Binding                 { return nil }
func (e *recInline) Height(int) int                           { return 3 }
func (e *recInline) Draw(uv.Screen, uv.Rectangle) *tea.Cursor { return nil }
func (e *recInline) HeightChanged() bool                      { return false }
func (e *recInline) SetFocused(focused bool)                  { e.focused = focused }

func (e *recInline) HandlePaste(msg tea.PasteMsg) tea.Cmd {
	e.pastes = append(e.pastes, msg.Content)
	return nil
}

func (e *recInline) HandleMouseClick(int, int) (bool, bool) {
	e.clicks++
	return false, true
}
func (e *recInline) SetHover(int, int) {}

func (e *recInline) HandleWheel(_, deltaY float64) { e.wheel += deltaY }

func newRoutingUI(t *testing.T) *UI {
	t.Helper()
	u, _ := newCmdStateUI(t)
	sty := u.com.Styles
	u.mention.popup = completions.New(sty.Completions.Normal, sty.Completions.Focused, sty.Completions.Match)
	u.agentReady = true
	u.textarea.Focus()
	// As after the prompt history loaded.
	u.promptHistory.index = -1
	u.updateLayoutAndSize()
	return u
}

func press(s string) tea.KeyPressMsg {
	switch s {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "ctrl+g":
		return tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl}
	case "ctrl+enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

func setBusy(u *UI) {
	u.agentBusyCache.set(true)
}

func interrupting(u *UI) bool {
	return u.interruptInFlight[u.currentSessionID()]
}

func TestRoutingQuitKeyBeatsDialogsAndInline(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	d := &recDialog{id: "rec"}
	u.dialog.OpenDialog(d)
	inline := &recInline{}
	u.activeInline = inline
	u.Update(press("ctrl+c"))
	require.True(t, u.dialog.ContainsDialog(dialog.QuitID))
	require.Empty(t, d.msgs)
	require.Empty(t, inline.keys)
}

func TestRoutingDialogBeatsInlineAndEditor(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	inline := &recInline{}
	u.activeInline = inline
	d := &recDialog{id: "rec"}
	u.dialog.OpenDialog(d)
	u.Update(press("tab"))
	u.Update(press("x"))
	require.Len(t, d.msgs, 2)
	require.Empty(t, inline.keys)
	require.Equal(t, uiFocusEditor, u.focus, "Tab went to the dialog")
}

func TestRoutingTabTogglesInlineFocus(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	inline := &recInline{}
	u.activeInline = inline
	u.Update(press("tab"))
	require.Equal(t, uiFocusMain, u.focus)
	require.False(t, inline.focused)
	require.Empty(t, inline.keys, "Tab toggles focus before the inline editor sees keys")
	u.Update(press("tab"))
	require.Equal(t, uiFocusEditor, u.focus)
	require.True(t, inline.focused)
}

func TestRoutingFocusedInlineBeatsCancel(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	setBusy(u)
	inline := &recInline{}
	u.activeInline = inline
	u.Update(press("esc"))
	require.Equal(t, []string{"esc"}, inline.keys)
	require.False(t, interrupting(u))
}

func TestRoutingBlurredInlineLetsCancelThrough(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	setBusy(u)
	inline := &recInline{}
	u.activeInline = inline
	u.focus = uiFocusMain
	u.Update(press("esc"))
	require.Empty(t, inline.keys)
	require.True(t, interrupting(u))
}

func TestRoutingInlineDoneRestoresEditor(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	u.textarea.Blur()
	inline := &recInline{done: true}
	u.activeInline = inline
	u.Update(press("y"))
	require.Nil(t, u.activeInline)
	require.True(t, u.textarea.Focused())
}

func TestRoutingTaskRowBeatsCancel(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	setBusy(u)
	u.taskRow.bgProcs = []agent.Process{{PID: 1}}
	u.taskRow.focused = true
	u.Update(press("esc"))
	require.False(t, u.taskRow.focused, "esc leaves the row")
	require.False(t, interrupting(u), "the row took esc")

	// A key the row doesn't use drops the row and goes on to the editor.
	u.taskRow.focused = true
	u.Update(press("q"))
	require.False(t, u.taskRow.focused)
	require.Equal(t, "q", u.textarea.Value())
}

func TestRoutingCompletionsBeatCancel(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	setBusy(u)
	_ = u.mention.begin(0, image.Point{}, 1, 10)
	u.Update(press("esc"))
	require.False(t, interrupting(u), "esc closes the completions first")
}

func TestRoutingCancelWhenBusy(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	setBusy(u)
	u.Update(press("esc"))
	require.True(t, interrupting(u))
}

func TestRoutingEscIdleGoesToEditor(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	u.Update(press("esc"))
	require.False(t, interrupting(u))
}

func TestRoutingInterruptKey(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	setBusy(u)
	u.Update(press("ctrl+enter"))
	require.True(t, interrupting(u))

	// Held down, the key does nothing more.
	v := newRoutingUI(t)
	setBusy(v)
	repeat := press("ctrl+enter")
	repeat.IsRepeat = true
	v.Update(repeat)
	require.False(t, interrupting(v))
}

func TestRoutingOnboardingSwallowsKeys(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	u.state = uiOnboarding
	u.Update(press("a"))
	require.Empty(t, u.textarea.Value())
}

func TestRoutingEditorTypesAndRunsGlobalKeys(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	u.Update(press("h"))
	u.Update(press("i"))
	require.Equal(t, "hi", u.textarea.Value())

	u.Update(press("ctrl+g"))
	require.True(t, u.status.ShowingAll(), "global keys work from the editor")
	require.Equal(t, "hi", u.textarea.Value())
}

func TestRoutingEditorTabFocusesChat(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	u.Update(press("tab"))
	require.Equal(t, uiFocusMain, u.focus)
	require.False(t, u.textarea.Focused())
	require.True(t, u.chat.Focused())

	// On the landing page Tab stays put.
	v := newRoutingUI(t)
	v.state = uiLanding
	v.Update(press("tab"))
	require.Equal(t, uiFocusEditor, v.focus)
}

func TestRoutingChatFocus(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	u.Update(press("tab"))
	require.Equal(t, uiFocusMain, u.focus)

	u.Update(press("x"))
	require.Empty(t, u.textarea.Value(), "the chat doesn't type")

	u.Update(press("ctrl+g"))
	require.True(t, u.status.ShowingAll(), "global keys work from the chat")

	u.Update(press("tab"))
	require.Equal(t, uiFocusEditor, u.focus)
	require.True(t, u.textarea.Focused())
	require.False(t, u.chat.Focused())
}

func TestRoutingSidebarFocus(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	u.focus = uiFocusSidebar
	u.textarea.Blur()
	u.sidebar.scrollable = true
	u.sidebar.maxOffset = 30
	u.Update(press("j"))
	require.Equal(t, 4, u.sidebar.offset)

	u.Update(press("tab"))
	require.Equal(t, uiFocusEditor, u.focus)
	require.True(t, u.textarea.Focused())
}

func TestRoutingSubagentViewTakesKeys(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	setBusy(u)
	u.subagentView = &subagentView{task: agent.Task{SessionID: "s1"}, chat: NewChat(u.com, config.ScrollbarDefault), cancel: func() {}}
	u.Update(press("esc"))
	require.Nil(t, u.subagentView, "esc closes the sub-agent view")
	require.False(t, interrupting(u), "and doesn't cancel the agent")
}

func TestRoutingPaste(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	inline := &recInline{}
	u.activeInline = inline
	u.Update(tea.PasteMsg{Content: "one"})
	require.Equal(t, []string{"one"}, inline.pastes, "a focused inline editor takes pastes")

	d := &recDialog{id: "rec"}
	u.activeInline = nil
	u.dialog.OpenDialog(d)
	u.Update(tea.PasteMsg{Content: "two"})
	require.Len(t, d.msgs, 1, "dialogs come next")
	require.Empty(t, u.textarea.Value())

	u.dialog.CloseDialog("rec")
	u.Update(tea.PasteMsg{Content: "three"})
	require.Equal(t, "three", u.textarea.Value())

	u.focus = uiFocusMain
	u.Update(tea.PasteMsg{Content: "four"})
	require.Equal(t, "three", u.textarea.Value(), "the chat ignores pastes")
}

func TestRoutingPasteInlineBeatsDialog(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	inline := &recInline{}
	u.activeInline = inline
	d := &recDialog{id: "rec"}
	u.dialog.OpenDialog(d)
	u.Update(tea.PasteMsg{Content: "one"})
	require.Equal(t, []string{"one"}, inline.pastes, "a focused inline editor takes pastes even under a dialog")
	require.Empty(t, d.msgs)

	u.focus = uiFocusMain
	u.Update(tea.PasteMsg{Content: "two"})
	require.Len(t, d.msgs, 1, "a blurred one does not")
}

func TestRoutingMouseClick(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	inline := &recInline{}
	u.activeInline = inline
	d := &recDialog{id: "rec"}
	u.dialog.OpenDialog(d)
	click := tea.MouseClickMsg{X: 1, Y: u.layout.editor.Min.Y, Button: tea.MouseLeft}
	u.Update(click)
	require.Len(t, d.msgs, 1, "dialogs get clicks first")
	require.Zero(t, inline.clicks)

	u.dialog.CloseDialog("rec")
	u.Update(click)
	require.Equal(t, 1, inline.clicks, "then the inline editor")
}

func TestRoutingWheel(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	inline := &recInline{}
	u.activeInline = inline
	u.updateLayoutAndSize()
	d := &recDialog{id: "rec"}
	u.dialog.OpenDialog(d)

	overEditor := common.CoalescedWheelMsg{Mouse: tea.Mouse{X: 1, Y: u.layout.editor.Min.Y}, DeltaY: 2}
	u.Update(overEditor)
	require.Equal(t, 2.0, inline.wheel, "the inline editor under the pointer scrolls even over a dialog")
	require.Empty(t, d.msgs)

	overChat := common.CoalescedWheelMsg{Mouse: tea.Mouse{X: 1, Y: u.layout.main.Min.Y}, DeltaY: 2}
	u.Update(overChat)
	require.Len(t, d.msgs, 1, "elsewhere the dialog gets the wheel")
	require.Equal(t, 2.0, inline.wheel)
}

func TestRoutingWheelOverSidebar(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	u.focus = uiFocusSidebar
	u.sidebar.maxOffset = 30
	u.Update(common.CoalescedWheelMsg{Mouse: tea.Mouse{X: 1, Y: u.layout.main.Min.Y}, DeltaY: 3})
	require.Equal(t, 3, u.sidebar.offset, "a focused sidebar takes the wheel wherever the pointer is")
	require.True(t, u.sidebar.scrollbarVisible)
}
