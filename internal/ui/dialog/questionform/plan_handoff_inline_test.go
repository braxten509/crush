package questionform

import (
	"image"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

func newTestPlanHandoff() *PlanHandoffInline {
	sty := styles.CharmtonePantera()
	return NewPlanHandoffInline(&common.Common{Styles: &sty})
}

func TestPlanHandoffRequestChangesKeyboard(t *testing.T) {
	t.Parallel()

	p := newTestPlanHandoff()
	p.SetFocused(true)

	done, cmd := p.HandleKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
	require.False(t, done)
	require.Nil(t, cmd)
	require.True(t, p.editing)
	require.True(t, p.editor.Focused())
	require.True(t, p.HeightChanged())
	require.Greater(t, p.Height(80), 3)

	called := false
	p.OnRequestChanges = func(feedback string) tea.Cmd {
		called = true
		require.Equal(t, "Revise the scope", feedback)
		return nil
	}

	done, cmd = p.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.False(t, done, "empty feedback must not submit")
	require.Nil(t, cmd)
	require.False(t, called)

	p.editor.SetValue("  Revise the scope  ")
	done, cmd = p.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.True(t, done)
	require.Nil(t, cmd)
	require.True(t, called)
}

func TestPlanHandoffRequestChangesNewline(t *testing.T) {
	t.Parallel()

	tests := map[string]tea.KeyPressMsg{
		"shift enter": {Code: tea.KeyEnter, Mod: tea.ModShift},
		"ctrl j":      {Code: 'j', Mod: tea.ModCtrl},
	}
	for name, msg := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p := newTestPlanHandoff()
			p.SetFocused(true)
			p.HandleKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
			p.editor.SetValue("First line")

			done, _ := p.HandleKey(msg)

			require.False(t, done)
			require.Equal(t, "First line\n", p.editor.Value())
		})
	}
}

func TestPlanHandoffRequestChangesMouseSelection(t *testing.T) {
	t.Parallel()

	p := newTestPlanHandoff()
	p.SetFocused(true)
	p.HandleKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
	p.editor.SetValue("copy me")
	p.SetWidth(80)
	scr := uv.NewScreenBuffer(80, p.Height(80))
	p.Draw(scr, image.Rect(0, 0, 80, p.Height(80)))

	textX, textY := p.editorTextArea.Min.X, p.editorTextArea.Min.Y
	require.True(t, p.HandleMouseDown(textX, textY))
	require.True(t, p.HandleMouseDrag(textX+4, textY))
	require.Equal(t, "copy", p.SelectedText())
	handled, cmd := p.HandleMouseRelease(textX+4, textY)
	require.True(t, handled)
	require.NotNil(t, cmd)
}

func TestPlanHandoffRequestChangesMouseSelectionAcrossLines(t *testing.T) {
	t.Parallel()

	t.Run("logical lines in reverse", func(t *testing.T) {
		t.Parallel()

		p := newTestPlanHandoff()
		p.SetFocused(true)
		p.HandleKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
		p.editor.SetValue("first\nsecond")
		p.SetWidth(80)
		scr := uv.NewScreenBuffer(80, p.Height(80))
		p.Draw(scr, image.Rect(0, 0, 80, p.Height(80)))

		textX, textY := p.editorTextArea.Min.X, p.editorTextArea.Min.Y
		require.True(t, p.HandleMouseDown(textX+6, textY+1))
		require.True(t, p.HandleMouseDrag(textX, textY))
		require.Equal(t, "first\nsecond", p.SelectedText())
	})

	t.Run("soft wrapped line", func(t *testing.T) {
		t.Parallel()

		p := newTestPlanHandoff()
		p.SetFocused(true)
		p.HandleKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
		p.SetWidth(14)
		p.editor.SetValue("one two three")
		scr := uv.NewScreenBuffer(14, p.Height(14))
		p.Draw(scr, image.Rect(0, 0, 14, p.Height(14)))

		textX, textY := p.editorTextArea.Min.X, p.editorTextArea.Min.Y
		require.True(t, p.HandleMouseDown(textX, textY))
		require.True(t, p.HandleMouseDrag(textX+5, textY+1))
		require.Equal(t, "one two three", p.SelectedText())
	})
}

func TestPlanHandoffEscapePreservesDraft(t *testing.T) {
	t.Parallel()

	p := newTestPlanHandoff()
	p.SetFocused(true)
	p.HandleKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
	p.editor.SetValue("Keep this draft")
	editingHeight := p.Height(80)

	done, cmd := p.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	require.False(t, done)
	require.Nil(t, cmd)
	require.False(t, p.editing)
	require.False(t, p.editor.Focused())
	require.Equal(t, "Keep this draft", p.editor.Value())
	require.True(t, p.HeightChanged())
	require.Less(t, p.Height(80), editingHeight)

	done, _ = p.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.False(t, done)
	require.True(t, p.editing)
	require.Equal(t, "Keep this draft", p.editor.Value())
}

func TestPlanHandoffEscapeFromChoiceRequestsCollapse(t *testing.T) {
	t.Parallel()

	p := newTestPlanHandoff()
	done, cmd := p.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	require.False(t, done)
	require.NotNil(t, cmd)
	_, ok := cmd().(dialog.CollapseInlineMsg)
	require.True(t, ok)
}

func TestPlanHandoffPasteAndCursor(t *testing.T) {
	t.Parallel()

	p := newTestPlanHandoff()
	p.SetFocused(true)
	p.HandleKey(tea.KeyPressMsg{Code: 'n', Text: "n"})

	p.HandlePaste(tea.PasteMsg{Content: "Pasted feedback"})
	require.Equal(t, "Pasted feedback", p.editor.Value())

	scr := uv.NewScreenBuffer(80, p.Height(80))
	cursor := p.Draw(scr, image.Rect(0, 0, 80, p.Height(80)))
	require.NotNil(t, cursor)
	require.GreaterOrEqual(t, cursor.Y, 2)
}

func TestPlanHandoffMouseRequestChanges(t *testing.T) {
	t.Parallel()

	p := newTestPlanHandoff()
	p.SetFocused(true)
	scr := uv.NewScreenBuffer(80, 10)
	p.Draw(scr, image.Rect(0, 0, 80, 10))

	x, y := planHandoffButtonPoint(t, p, 2)
	done, handled := p.HandleMouseClick(x, y)
	require.False(t, done)
	require.True(t, handled)
	require.True(t, p.editing)
	require.True(t, p.editor.Focused())
}

// When the chat has focus the handoff must keep its focused layout instead
// of collapsing to a one-line prompt.
func TestPlanHandoffBlurredViewKeepsChoiceLayout(t *testing.T) {
	t.Parallel()

	p := newTestPlanHandoff()
	p.SetFocused(true)
	focused := p.Height(80)

	p.SetFocused(false)
	require.Equal(t, focused, p.Height(80))
}

func TestPlanHandoffSetWidthRecalculatesFeedbackHeight(t *testing.T) {
	t.Parallel()

	p := newTestPlanHandoff()
	p.SetFocused(true)
	p.HandleKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
	p.SetWidth(80)
	p.editor.SetValue(strings.Repeat("feedback ", 7))
	wideHeight := p.Height(80)
	p.HeightChanged()

	p.SetWidth(20)
	narrowHeight := p.Height(20)

	require.Greater(t, narrowHeight, wideHeight)
	require.True(t, p.HeightChanged())
}

func TestPlanHandoffStartCoding(t *testing.T) {
	t.Parallel()

	p := newTestPlanHandoff()
	confirmed := 0
	p.OnConfirm = func(yolo bool) tea.Cmd {
		require.False(t, yolo)
		return func() tea.Msg {
			confirmed++
			return nil
		}
	}

	done, cmd := p.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.True(t, done)
	require.NotNil(t, cmd)
	cmd()
	require.Equal(t, 1, confirmed)
}

func TestPlanHandoffCodingKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  tea.KeyPressMsg
		yolo bool
	}{
		{"coding lowercase", tea.KeyPressMsg{Code: 'c', Text: "c"}, false},
		{"coding uppercase", tea.KeyPressMsg{Code: 'C', Text: "C"}, false},
		{"yolo lowercase", tea.KeyPressMsg{Code: 'y', Text: "y"}, true},
		{"yolo uppercase", tea.KeyPressMsg{Code: 'Y', Text: "Y"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := newTestPlanHandoff()
			confirmed := false
			p.OnConfirm = func(yolo bool) tea.Cmd {
				require.Equal(t, tt.yolo, yolo)
				confirmed = true
				return nil
			}

			done, _ := p.HandleKey(tt.key)
			require.True(t, done)
			require.True(t, confirmed)
		})
	}
}

func TestPlanHandoffShortHelpShowsCodingKeys(t *testing.T) {
	t.Parallel()

	p := newTestPlanHandoff()
	var coding, yolo bool
	for _, binding := range p.ShortHelp() {
		coding = coding || slices.Contains(binding.Keys(), "c")
		yolo = yolo || slices.Contains(binding.Keys(), "y")
	}
	require.True(t, coding, "the help lists the coding key")
	require.True(t, yolo, "the help lists the YOLO key")
}

func planHandoffButtonPoint(t *testing.T, p *PlanHandoffInline, index int) (int, int) {
	t.Helper()
	for y := range 10 {
		for x := range 80 {
			if common.HitButtonIndex(p.compositor, x, y) == index {
				return x, y
			}
		}
	}
	t.Fatalf("button %d was not rendered", index)
	return 0, 0
}

func TestPlanHandoffYOLO(t *testing.T) {
	t.Parallel()
	for _, mouse := range []bool{false, true} {
		p := newTestPlanHandoff()
		confirmed := false
		p.OnConfirm = func(yolo bool) tea.Cmd {
			require.True(t, yolo)
			confirmed = true
			return nil
		}
		if mouse {
			scr := uv.NewScreenBuffer(24, p.Height(24))
			p.Draw(scr, image.Rect(0, 0, 24, p.Height(24)))
			x, y := planHandoffButtonPoint(t, p, 1)
			done, handled := p.HandleMouseClick(x, y)
			require.True(t, done)
			require.True(t, handled)
		} else {
			p.HandleKey(tea.KeyPressMsg{Code: tea.KeyRight})
			done, _ := p.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
			require.True(t, done)
		}
		require.True(t, confirmed)
	}
}

func TestPlanHandoffChoiceWrapAndBottomGap(t *testing.T) {
	t.Parallel()
	p := newTestPlanHandoff()
	p.HandleKey(tea.KeyPressMsg{Code: tea.KeyLeft})
	require.Equal(t, 2, p.selectedChoice)
	p.HandleKey(tea.KeyPressMsg{Code: tea.KeyRight})
	require.Equal(t, 0, p.selectedChoice)
}
