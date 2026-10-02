package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

// The y and n keys answer with the key pressed, whatever is highlighted.
func TestYesNoFormKeysAnswerDirectly(t *testing.T) {
	for _, tc := range []struct {
		name   string
		keys   []tea.KeyPressMsg
		wantOK bool
	}{
		{"y with No highlighted", []tea.KeyPressMsg{{Code: 'y', Text: "y"}}, true},
		{"n with Yes highlighted", []tea.KeyPressMsg{{Code: tea.KeyRight}, {Code: 'n', Text: "n"}}, false},
		{"enter answers the highlight", []tea.KeyPressMsg{{Code: tea.KeyRight}, {Code: tea.KeyEnter}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := styles.CharmtonePantera()
			f := NewQuestionForm(&s, question.Request{Questions: []question.Question{{
				ID: "q", Type: question.TypeYesNo, Text: "Install?", Description: "d",
			}}})
			var got *bool
			ran := false
			f.OnAnswerCmd = func(r []question.Answer) tea.Cmd {
				got = r[0].Yes
				return func() tea.Msg { ran = true; return nil }
			}
			var done bool
			var cmd tea.Cmd
			for _, k := range tc.keys {
				done, cmd = f.HandleKey(k)
			}
			require.True(t, done)
			require.NotNil(t, got)
			require.Equal(t, tc.wantOK, *got)
			require.NotNil(t, cmd, "OnAnswerCmd's command is returned")
			if msg := cmd(); msg != nil {
				for _, c := range msg.(tea.BatchMsg) {
					if c != nil {
						c()
					}
				}
			}
			require.True(t, ran)
		})
	}
}
