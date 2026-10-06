package server

import (
	"testing"

	"github.com/charmbracelet/crush/internal/question"
	"github.com/stretchr/testify/require"
)

// A choice's sketch is sent on to the screen with the question.
func TestQuestionChoiceImageSent(t *testing.T) {
	t.Parallel()
	got := questionsToProto([]question.Question{{
		ID: "q", Type: question.TypeSingleChoice, Text: "Which?",
		Choices: []question.Choice{{ID: "a", Label: "A", Image: "/tmp/a.png"}},
	}})
	require.Equal(t, "/tmp/a.png", got[0].Choices[0].Image)
}
