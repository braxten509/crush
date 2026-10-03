package model

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/secureentry"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func secureFormFixture(t *testing.T) (*secureentry.Form, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "key.env")
	require.NoError(t, os.WriteFile(path, []byte("KEY=KEY_SLOT\n"), 0o600))
	forms := make(chan *secureentry.Form, 1)
	detach := secureentry.AttachForms(func(f *secureentry.Form) { forms <- f })
	t.Cleanup(detach)
	request := question.Request{ID: "secure-batch", SessionID: "s1", Questions: []question.Question{
		{ID: "key", Type: question.TypeSecureEntry, Text: "Enter key", Description: "Service key"},
		{ID: "name", Type: question.TypeFreeText, Text: "Name", Description: "Display name"},
	}}
	form, err := secureentry.OpenForm("s1", request, map[string]secureentry.Spec{"key": {File: path, Placeholder: "KEY_SLOT", Occurrence: 1}})
	require.NoError(t, err)
	require.Same(t, form, <-forms)
	return form, path
}

func TestExternalSecureCancellationDismissesOnlyItsOwnForm(t *testing.T) {
	form, _ := secureFormFixture(t)
	u, _ := newSubmissionUI()
	u.Update(form)
	inline := u.activeInline.(*dialog.QuestionForm)
	u.Update(tea.PasteMsg{Content: "dummy-key"})
	form.Cancel()
	select {
	case <-form.Done():
	default:
		t.Fatal("closure was not broadcast")
	}
	u.Update(secureQuestionClosedMsg{form: form})
	require.Nil(t, u.activeInline)
	require.True(t, inline.HasSecureEntry())
	u.openBatchFormDialog(question.Request{ID: "new-form", Questions: []question.Question{{ID: "normal", Type: question.TypeFreeText, Text: "Name", Description: "Name"}}})
	newForm := u.activeInline
	u.Update(secureQuestionClosedMsg{form: form})
	require.Same(t, newForm, u.activeInline)
}

func TestSecureQuestionsUseTheInlineFormAndSurviveOrdinaryNotifications(t *testing.T) {
	form, path := secureFormFixture(t)
	u, _ := newSubmissionUI()
	u.textarea.SetValue("existing draft")
	u.Update(form)
	inline, ok := u.activeInline.(*dialog.QuestionForm)
	require.True(t, ok)
	require.True(t, inline.HasSecureEntry())
	u.Update(tea.PasteMsg{Content: "dummy-key"})
	require.Equal(t, "existing draft", u.textarea.Value())
	u.handleQuestionNotification(question.Notification{})
	require.Same(t, inline, u.activeInline)
	u.openPlanHandoff()
	require.Same(t, inline, u.activeInline)
	u.focusActiveInline(uiFocusMain)
	u.Update(tea.PasteMsg{Content: "dummy-secret-outside-field"})
	require.Equal(t, "existing draft", u.textarea.Value())
	u.openBatchFormDialog(question.Request{ID: "ordinary", Questions: []question.Question{{ID: "normal", Type: question.TypeFreeText, Text: "Name", Description: "Name"}}})
	result := <-form.Result()
	require.True(t, result.Cancelled)
	require.Empty(t, result.Answers)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "KEY=KEY_SLOT\n", string(data))
}
