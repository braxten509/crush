package model

import (
	"os"
	"testing"

	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/ui/dialog/questionform"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/stretchr/testify/require"
)

// These tests pin that settings writes behind key presses and dialog
// actions run off the Update goroutine (each is a file write, or an HTTP
// round-trip in client/server mode), and that their outcome reaches the
// user once Update applied the result message.

// TestComposerFooterFormSavesOffLoop pins that saving the Customize
// Composer form writes the config when its command runs and reports the
// result once Update applied it.
func TestComposerFooterFormSavesOffLoop(t *testing.T) {
	t.Parallel()

	for _, fail := range []bool{false, true} {
		u, ws := newCmdStateUI(t)
		if fail {
			ws.setErr = os.ErrPermission
		}
		u.openComposerFooterForm()
		form, ok := u.activeInline.(*questionform.QuestionForm)
		require.True(t, ok)
		cmd := form.OnAnswerCmd([]question.Answer{{SelectedIDs: []string{footerTokens, footerModel}}})
		_, written := ws.set("options.tui.composer_footer")
		require.False(t, written, "the write must not run on the Update goroutine")

		done, ok := findMsg[workDoneMsg](collectMsgs(cmd))
		require.True(t, ok)
		if !fail {
			v, _ := ws.set("options.tui.composer_footer")
			require.Equal(t, []string{footerModel, footerTokens}, v)
		}
		_, reply := u.Update(done)
		info, ok := findMsg[util.InfoMsg](collectMsgs(reply))
		require.True(t, ok)
		if fail {
			require.Equal(t, util.InfoTypeError, info.Type)
		} else {
			require.Equal(t, "Composer updated", info.Msg)
		}
	}
}
