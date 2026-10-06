package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// `crush ask` keeps each choice's sketch on its way to the question form.
func TestAskKeepsChoiceImage(t *testing.T) {
	t.Parallel()
	pic := filepath.Join(t.TempDir(), "a.png")
	require.NoError(t, os.WriteFile(pic, []byte("\x89PNG"), 0o600))
	data, err := json.Marshal(map[string]any{"questions": []any{map[string]any{
		"type": "single_choice", "label": "Look", "question": "Which look?", "description": "Pick one.",
		"choices": []any{
			map[string]any{"id": "a", "label": "Arcade", "image": pic},
			map[string]any{"id": "b", "label": "Normal"},
		},
	}}})
	require.NoError(t, err)
	r, _, err := askQuestions(data)
	require.NoError(t, err)
	require.Equal(t, pic, r.Questions[0].Choices[0].Image)
	require.Empty(t, r.Questions[0].Choices[1].Image)
}
