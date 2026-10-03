package dialog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/secureentry"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

const secureTemplate = "EXISTING=dummy-existing-key\nKEY=KEY_SLOT\nTOKEN=TOKEN_SLOT\n"

// openSecureForm prepares a dummy file and a form with a free-text question
// and two secure slots in that file.
func openSecureForm(t *testing.T, questions ...question.Question) (*QuestionForm, *secureentry.Form, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "keys.env")
	require.NoError(t, os.WriteFile(path, []byte(secureTemplate), 0o644))
	if len(questions) == 0 {
		questions = []question.Question{
			{ID: "name", Type: question.TypeFreeText, Label: "Name", Text: "Your name?", Description: "d"},
			{ID: "key", Type: question.TypeSecureEntry, Label: "API key", Text: "Enter the service key", Description: "Used by the service"},
			{ID: "token", Type: question.TypeSecureEntry, Label: "Token", Text: "Enter the token", Description: "d"},
		}
	}
	specs := map[string]secureentry.Spec{}
	for _, q := range questions {
		if q.Type == question.TypeSecureEntry {
			placeholder := "KEY_SLOT"
			if q.ID == "token" {
				placeholder = "TOKEN_SLOT"
			}
			specs[q.ID] = secureentry.Spec{File: path, Placeholder: placeholder, Occurrence: 1}
		}
	}
	forms := make(chan *secureentry.Form, 1)
	detach := secureentry.AttachForms(func(f *secureentry.Form) { forms <- f })
	t.Cleanup(detach)
	req := question.Request{ID: "batch", Questions: questions, ConfirmTitle: "Ready?", ConfirmDescription: "Check."}
	_, err := secureentry.OpenForm("session", req, specs)
	require.NoError(t, err)
	form := <-forms
	sty := styles.CharmtonePantera()
	f := NewSecureQuestionForm(&sty, form)
	f.SetFocused(true)
	return f, form, path
}

func typeText(f *QuestionForm, text string) {
	for _, r := range text {
		f.HandleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func enter(f *QuestionForm) bool {
	done, _ := f.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	return done
}

func renderForm(f *QuestionForm, width, height int) string {
	scr := uv.NewScreenBuffer(width, height)
	f.Draw(scr, scr.Bounds())
	return ansi.Strip(scr.Render())
}

func requireFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, want, string(data))
}

func requireNoResult(t *testing.T, form *secureentry.Form) {
	t.Helper()
	select {
	case <-form.Result():
		t.Fatal("the form resolved early")
	default:
	}
}

func TestSecureQuestionFormSubmitsTogether(t *testing.T) {
	f, form, path := openSecureForm(t)
	const key, token = "dum]my-[key-1", "dummy-token-2"

	typeText(f, "Ada")
	require.False(t, enter(f))
	require.Equal(t, 1, f.activeIdx)
	typeText(f, key)
	require.Equal(t, 1, f.activeIdx, "[ and ] are typed into the secret, not tab switches")
	require.False(t, enter(f))
	f.HandlePaste(tea.PasteMsg{Content: token + "\n"})
	require.False(t, enter(f))
	require.True(t, f.isConfirmTab())

	requireFile(t, path, secureTemplate)
	requireNoResult(t, form)
	for _, size := range [][2]int{{120, 30}, {60, 20}, {30, 12}} {
		for tab := range len(f.labels) {
			f.switchTab(tab)
			out := renderForm(f, size[0], size[1])
			require.NotContains(t, out, "dum]my")
			require.NotContains(t, out, token)
		}
	}
	out := renderForm(f, 120, 30)
	require.Contains(t, out, "API key: Entered · saved on submit")
	require.Contains(t, out, "Name: Ada")
	t.Log("\n" + out)
	f.switchTab(1)
	out = renderForm(f, 120, f.Height(120))
	require.Contains(t, out, "•••••")
	t.Log("\n" + out)
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%d", "%x"} {
		require.NotContains(t, fmt.Sprintf(verb, f), "dummy", verb)
		require.NotContains(t, fmt.Sprintf(verb, f.questions[1]), "dummy", verb)
	}
	require.Equal(t, question.Answer{QuestionID: "key"}, f.questions[1].Response())

	f.switchTab(len(f.labels) - 1)
	require.True(t, enter(f))
	result := <-form.Result()
	require.False(t, result.Cancelled)
	require.Equal(t, map[string]string{"key": secureentry.StatusSaved, "token": secureentry.StatusSaved}, result.Statuses)
	require.Equal(t, "Ada", result.Answers[0].FillInText)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "dum]my")
	require.NotContains(t, string(encoded), token)
	requireFile(t, path, "EXISTING=dummy-existing-key\nKEY="+key+"\nTOKEN="+token+"\n")
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	for i := range f.numQuestions {
		if sq := f.secureQuestion(i); sq != nil {
			require.Empty(t, sq.buffer.runes, "buffers are cleared after saving")
		}
	}
}

func TestSecureQuestionFormCancelWritesNothing(t *testing.T) {
	f, form, path := openSecureForm(t)
	f.switchTab(1)
	typeText(f, "dummy-never-saved")
	done, _ := f.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	require.True(t, done)
	result := <-form.Result()
	require.True(t, result.Cancelled)
	require.Equal(t, map[string]string{"key": secureentry.StatusCancelled, "token": secureentry.StatusCancelled}, result.Statuses)
	requireFile(t, path, secureTemplate)
	require.Empty(t, f.secureQuestion(1).buffer.runes)
}

func TestSecureQuestionFormFailedSaveStaysOpen(t *testing.T) {
	f, form, path := openSecureForm(t)
	f.switchTab(2)
	typeText(f, "dummy-token")
	f.switchTab(len(f.labels) - 1)

	// Same inode, different contents: the save must refuse.
	require.NoError(t, os.WriteFile(path, []byte(secureTemplate+"# edited\n"), 0o600))
	require.False(t, enter(f), "a failed save keeps the form open")
	require.Equal(t, 2, f.activeIdx, "the failing field is shown")
	require.Contains(t, renderForm(f, 120, 30), "destination changed")
	requireNoResult(t, form)
	requireFile(t, path, secureTemplate+"# edited\n")
	require.NotEmpty(t, f.secureQuestion(2).buffer.runes, "the entry survives for a retry")

	require.NoError(t, os.WriteFile(path, []byte(secureTemplate), 0o600))
	f.switchTab(len(f.labels) - 1)
	require.True(t, enter(f))
	result := <-form.Result()
	require.Equal(t, map[string]string{"key": secureentry.StatusCancelled, "token": secureentry.StatusSaved}, result.Statuses)
	requireFile(t, path, "EXISTING=dummy-existing-key\nKEY=KEY_SLOT\nTOKEN=dummy-token\n")
}

func TestSecureQuestionSingleFieldAndClipboard(t *testing.T) {
	f, form, path := openSecureForm(t, question.Question{ID: "key", Type: question.TypeSecureEntry, Text: "Enter the key", Description: "d"})
	require.False(t, enter(f))
	require.Contains(t, renderForm(f, 80, 20), "Enter a value first.")

	original, originalTimeout := clipboardText, clipboardTimeout
	t.Cleanup(func() { clipboardText, clipboardTimeout = original, originalTimeout })
	clipboardTimeout = 20 * time.Millisecond
	late := []byte("dummy-late")
	release := make(chan struct{})
	returned := make(chan struct{})
	clipboardText = func() []byte { <-release; close(returned); return late }
	_, cmd := f.HandleKey(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	f.HandleSecurePaste(cmd().(SecureQuestionPaste))
	require.Empty(t, f.secureQuestion(0).buffer.runes, "a slow clipboard pastes nothing")
	close(release)
	<-returned

	clipboardText = func() []byte { return []byte("dummy-clip\n") }
	_, cmd = f.HandleKey(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	f.HandleSecurePaste(cmd().(SecureQuestionPaste))
	require.Len(t, f.secureQuestion(0).buffer.runes, len("dummy-clip"))
	require.True(t, enter(f), "one question submits on enter")
	require.Equal(t, secureentry.StatusSaved, (<-form.Result()).Statuses["key"])
	requireFile(t, path, "EXISTING=dummy-existing-key\nKEY=dummy-clip\nTOKEN=TOKEN_SLOT\n")
}

func TestSecureQuestionNeedsLocalForm(t *testing.T) {
	sty := styles.CharmtonePantera()
	f := NewQuestionForm(&sty, question.Request{Questions: []question.Question{
		{ID: "key", Type: question.TypeSecureEntry, Text: "Enter the key", Description: "d"},
	}})
	f.SetFocused(true)
	typeText(f, "dummy")
	f.HandlePaste(tea.PasteMsg{Content: "dummy"})
	require.Empty(t, f.secureQuestion(0).buffer.runes)
	out := renderForm(f, 100, 20)
	require.Contains(t, out, "only in the local Crush terminal")
	require.NotContains(t, out, "dummy")
}

func TestClosedSecureFormRejectsInputAndDismissalWipesTheBuffer(t *testing.T) {
	f, form, _ := openSecureForm(t, question.Question{ID: "key", Type: question.TypeSecureEntry, Text: "Key", Description: "Key"})
	typeText(f, "dummy-existing")
	form.Cancel()
	typeText(f, "should-not-append")
	f.HandlePaste(tea.PasteMsg{Content: "should-not-paste"})
	require.Equal(t, "dummy-existing", string(f.secureQuestion(0).buffer.runes))
	f.Dismiss()
	require.Empty(t, f.secureQuestion(0).buffer.runes)
	value := []byte("dummy-late")
	f.HandleSecurePaste(SecureQuestionPaste{question: f.secureQuestion(0), value: value})
	require.Equal(t, make([]byte, len(value)), value)
}
