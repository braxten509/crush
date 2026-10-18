package secureentry

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSecureFieldStatus: Fields reports each field as Field.Saved and
// Field.Available do, in question order, with its label and no file path.
func TestSecureFieldStatus(t *testing.T) {
	path := template(t, "A=FIRST\nB=SECOND\n")
	other := template(t, "C=OTHER\n")
	form := openTestForm(t, map[string]Spec{
		"first":  {File: path, Label: "Main key", Placeholder: "FIRST", Occurrence: 1},
		"second": {File: path, Placeholder: "SECOND", Occurrence: 1},
		"other":  {File: other, Label: "Other key", Placeholder: "OTHER", Occurrence: 1},
	})
	agrees := func() []FieldInfo {
		t.Helper()
		fields := form.Fields()
		for _, fi := range fields {
			require.Equal(t, form.Field(fi.QuestionID).Saved(), fi.Saved, fi.QuestionID)
			require.Equal(t, form.Field(fi.QuestionID).Available(), fi.Available, fi.QuestionID)
		}
		return fields
	}

	// A fresh field is open.
	require.Equal(t, []FieldInfo{
		{QuestionID: "first", Label: "Main key", Available: true},
		{QuestionID: "second", Label: "API key", Available: true},
		{QuestionID: "other", Label: "Other key", Available: true},
	}, agrees())

	// Once its file was written (here: the state after this file's rename
	// and a later file's failure), the filled field is saved and the
	// unfilled field of the same file is unavailable.
	form.mu.Lock()
	form.fields["first"].group.written = true
	form.fields["first"].saved = true
	form.mu.Unlock()
	require.Equal(t, []FieldInfo{
		{QuestionID: "first", Label: "Main key", Saved: true},
		{QuestionID: "second", Label: "API key"},
		{QuestionID: "other", Label: "Other key", Available: true},
	}, agrees())
	described := fmt.Sprintf("%+v %#v", form.Fields(), form.Fields())
	for _, file := range []string{path, other} {
		require.NotContains(t, described, file)
		require.NotContains(t, described, filepath.Base(file))
	}

	// A closed form takes no more values.
	form.Cancel()
	for _, fi := range agrees() {
		require.False(t, fi.Available, fi.QuestionID)
	}
}
