package proto

import (
	"encoding/json"
	"testing"

	"github.com/charmbracelet/crush/internal/question"
	"github.com/stretchr/testify/require"
)

// TestQuestionItemCarriesSelected: a multi_choice question's opening
// checks reach the wire as selected and come back (G-13).
func TestQuestionItemCarriesSelected(t *testing.T) {
	t.Parallel()
	req := question.Request{ID: "r1", SessionID: "s1", Questions: []question.Question{{
		ID: "q1", Type: question.TypeMultiChoice, Text: "Pick", Description: "d",
		Choices:  []question.Choice{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
		Selected: []string{"b"},
	}}}
	wire := QuestionRequestFromDomain(req)
	data, err := json.Marshal(wire)
	require.NoError(t, err)
	var raw struct {
		Questions []map[string]any `json:"questions"`
	}
	require.NoError(t, json.Unmarshal(data, &raw))
	require.Equal(t, []any{"b"}, raw.Questions[0]["selected"])
	require.Equal(t, []string{"b"}, QuestionRequestToDomain(wire).Questions[0].Selected)
}
