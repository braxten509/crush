package secureentry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/question"
	"github.com/stretchr/testify/require"
)

const formExisting = "dummy-existing-key"

// formRequest has an ordinary question, then a secure question per ID.
func formRequest(ids ...string) question.Request {
	r := question.Request{ID: "batch", Questions: []question.Question{
		{ID: "mode", Type: question.TypeSingleChoice, Text: "Mode?", Description: "d", Choices: []question.Choice{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}},
	}}
	for _, id := range ids {
		r.Questions = append(r.Questions, question.Question{ID: id, Type: question.TypeSecureEntry, Text: id + " key", Description: "d"})
	}
	return r
}

func openTestForm(t *testing.T, specs map[string]Spec) *Form {
	t.Helper()
	forms := make(chan *Form, 1)
	detach := AttachForms(func(f *Form) { forms <- f })
	t.Cleanup(detach)
	form, err := OpenForm("session", formRequest("first", "second", "other"), specs)
	require.NoError(t, err)
	require.Same(t, form, <-forms, "only the local TUI receives the form")
	return form
}

func TestFormSavesEverySlotOnSubmit(t *testing.T) {
	path := template(t, "EXISTING="+formExisting+"\nFIRST=FIRST_SLOT\nSECOND=SECOND_SLOT\n")
	otherPath := template(t, "OTHER=%s\n")
	form := openTestForm(t, map[string]Spec{
		"first":  {File: path, Placeholder: "FIRST_SLOT", Occurrence: 1},
		"second": {File: path, Placeholder: "SECOND_SLOT", Occurrence: 1},
		"other":  {File: otherPath, Occurrence: 1},
	})
	values := map[string][]byte{"first": []byte("dummy-one-%s-$HOME"), "second": []byte("dummy-two"), "other": []byte("dummy-three")}
	answers := []question.Answer{
		{QuestionID: "mode", SelectedIDs: []string{"b"}},
		{QuestionID: "first", FillInText: "dummy-one-%s-$HOME"}, // a buggy caller's leak is dropped
		{QuestionID: "second"},
		{QuestionID: "other"},
	}
	require.NoError(t, form.Submit(answers, values))
	result := <-form.Result()
	require.False(t, result.Cancelled)
	require.Equal(t, map[string]string{"first": StatusSaved, "second": StatusSaved, "other": StatusSaved}, result.Statuses)
	require.Equal(t, []string{"b"}, result.Answers[0].SelectedIDs)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	for _, value := range values {
		require.NotContains(t, string(encoded), string(value))
		require.NotContains(t, fmt.Sprintf("%+v %#v", form, form.Field("first")), string(value))
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "EXISTING="+formExisting+"\nFIRST=dummy-one-%s-$HOME\nSECOND=dummy-two\n", string(data))
	data, err = os.ReadFile(otherPath)
	require.NoError(t, err)
	require.Equal(t, "OTHER=dummy-three\n", string(data))
	for _, file := range []string{path, otherPath} {
		info, err := os.Stat(file)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		entries, err := os.ReadDir(filepath.Dir(file))
		require.NoError(t, err)
		require.Len(t, entries, 1, "no temporary file is left behind")
	}
	require.ErrorIs(t, form.Submit(answers, values), ErrClosed)
}

func TestFormCancelAndEmptySlotsWriteNothing(t *testing.T) {
	path := template(t, "A=%s\nB=%s\n")
	otherPath := template(t, "C=%s\n")
	specs := map[string]Spec{
		"first":  {File: path, Occurrence: 1},
		"second": {File: path, Occurrence: 2},
		"other":  {File: otherPath, Occurrence: 1},
	}
	form := openTestForm(t, specs)
	_, err := OpenForm("session", formRequest("first", "second", "other"), specs)
	require.ErrorContains(t, err, "already open")
	form.Cancel()
	result := <-form.Result()
	require.True(t, result.Cancelled)
	require.Equal(t, map[string]string{"first": StatusCancelled, "second": StatusCancelled, "other": StatusCancelled}, result.Statuses)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "A=%s\nB=%s\n", string(data))

	// Occurrences count in the file as prepared; an empty slot is skipped.
	form = openTestForm(t, specs)
	require.NoError(t, form.Submit(nil, map[string][]byte{"second": []byte("dummy-b")}))
	result = <-form.Result()
	require.Equal(t, map[string]string{"first": StatusCancelled, "second": StatusSaved, "other": StatusCancelled}, result.Statuses)
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "A=%s\nB=dummy-b\n", string(data))
	data, err = os.ReadFile(otherPath)
	require.NoError(t, err)
	require.Equal(t, "C=%s\n", string(data))
}

func TestPartialSaveMakesUnenteredSlotsUnavailable(t *testing.T) {
	path := template(t, "A=FIRST\nB=SECOND\n")
	other := template(t, "C=OTHER\n")
	form := openTestForm(t, map[string]Spec{
		"first":  {File: path, Placeholder: "FIRST", Occurrence: 1},
		"second": {File: path, Placeholder: "SECOND", Occurrence: 1},
		"other":  {File: other, Placeholder: "OTHER", Occurrence: 1},
	})
	// State after this file committed but a later file's rename failed.
	form.mu.Lock()
	form.fields["first"].group.written = true
	form.fields["first"].saved = true
	form.mu.Unlock()
	require.False(t, form.Field("second").Available())
	require.True(t, form.Field("other").Available())
	err := form.Submit([]question.Answer{{QuestionID: "second"}}, map[string][]byte{"second": []byte("dummy-late-value")})
	require.ErrorContains(t, err, "already saved")
	select {
	case <-form.Result():
		t.Fatal("must not report a silently skipped new value as completed")
	default:
	}
}

func TestFormFailedSaveChangesNothingAndCanRetry(t *testing.T) {
	path := template(t, "A=%s\nB=B_SLOT\n")
	otherPath := template(t, "C=%s\n")
	form := openTestForm(t, map[string]Spec{
		"first":  {File: path, Occurrence: 1},
		"second": {File: path, Placeholder: "B_SLOT", Occurrence: 1},
		"other":  {File: otherPath, Occurrence: 1},
	})
	values := map[string][]byte{"first": []byte("dummy-a"), "second": []byte("dummy-b"), "other": []byte("dummy-c")}

	unchanged := func() {
		t.Helper()
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, "A=%s\nB=B_SLOT\n", string(data))
		for _, dir := range []string{filepath.Dir(path), filepath.Dir(otherPath)} {
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Len(t, entries, 1, "no temporary file is left behind")
		}
		select {
		case <-form.Result():
			t.Fatal("a failed save must not resolve the form")
		default:
		}
	}

	bad := map[string][]byte{"first": []byte("dummy-a"), "second": []byte("dummy\nkey")}
	var fieldErr *FieldError
	require.ErrorAs(t, form.Submit(nil, bad), &fieldErr)
	require.Equal(t, "second", fieldErr.QuestionID)
	unchanged()

	// The second file changes after the first is staged: neither is written.
	require.NoError(t, os.WriteFile(otherPath, []byte("C=%s\n"+formExisting+"\n"), 0o600))
	err := form.Submit(nil, values)
	require.ErrorAs(t, err, &fieldErr)
	require.Equal(t, "other", fieldErr.QuestionID)
	require.NotContains(t, err.Error(), formExisting)
	unchanged()

	// Restoring the file lets the same form save on retry.
	require.NoError(t, os.WriteFile(otherPath, []byte("C=%s\n"), 0o600))
	require.NoError(t, form.Submit(nil, values))
	result := <-form.Result()
	require.Equal(t, map[string]string{"first": StatusSaved, "second": StatusSaved, "other": StatusSaved}, result.Statuses)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "A=dummy-a\nB=dummy-b\n", string(data))
}

func TestFormPreflight(t *testing.T) {
	path := template(t, "EXISTING="+formExisting+"\nA=%s\nB=%s\n")
	_, err := OpenForm("session", formRequest("first"), map[string]Spec{"first": {File: path, Occurrence: 1}})
	require.ErrorContains(t, err, "local Crush terminal")

	forms := make(chan *Form, 1)
	detach := AttachForms(func(f *Form) { forms <- f })
	defer detach()
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(filepath.Dir(path), link))
	both := formRequest("first", "second")
	for name, tc := range map[string]struct {
		request question.Request
		specs   map[string]Spec
	}{
		"shared slot":    {both, map[string]Spec{"first": {File: path, Occurrence: 1}, "second": {File: path, Occurrence: 1}}},
		"overlap":        {both, map[string]Spec{"first": {File: path, Placeholder: "A=%s", Occurrence: 1}, "second": {File: path, Placeholder: "%s", Occurrence: 1}}},
		"missing":        {formRequest("first"), map[string]Spec{"first": {File: path, Placeholder: "MISSING", Occurrence: 1}}},
		"occurrence":     {formRequest("first"), map[string]Spec{"first": {File: path, Occurrence: 3}}},
		"relative":       {formRequest("first"), map[string]Spec{"first": {File: "credentials.env", Occurrence: 1}}},
		"two spellings":  {both, map[string]Spec{"first": {File: path, Occurrence: 1}, "second": {File: filepath.Join(link, filepath.Base(path)), Occurrence: 2}}},
		"unknown":        {formRequest("first"), map[string]Spec{"first": {File: path, Occurrence: 1}, "nope": {File: path, Occurrence: 2}}},
		"no destination": {both, map[string]Spec{"first": {File: path, Occurrence: 1}}},
		"no secure":      {formRequest(), map[string]Spec{}},
	} {
		_, err := OpenForm("session", tc.request, tc.specs)
		require.Error(t, err, name)
		require.NotContains(t, err.Error(), formExisting, name)
	}
	select {
	case <-forms:
		t.Fatal("a form that failed preflight must not open")
	default:
	}
	form, err := OpenForm("session", both, map[string]Spec{"first": {File: path, Occurrence: 1}, "second": {File: path, Occurrence: 2}})
	require.NoError(t, err, "failed preflights leave nothing open")
	require.Same(t, form, <-forms)
	detach()
	require.True(t, (<-form.Result()).Cancelled, "detaching the terminal cancels the form")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "EXISTING="+formExisting+"\nA=%s\nB=%s\n", string(data))
}
