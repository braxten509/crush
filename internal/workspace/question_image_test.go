package workspace

import (
	"testing"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/stretchr/testify/require"
)

// A choice's sketch survives the trip from Crush's background process to
// the screen, so the question form can show it.
func TestQuestionChoiceImageReachesScreen(t *testing.T) {
	t.Parallel()
	got := protoQuestionsToDomain([]proto.QuestionItem{{
		ID: "q", Type: "single_choice", Question: "Which?",
		Choices: []proto.QuestionChoice{{ID: "a", Label: "A", Image: "/tmp/a.png"}, {ID: "b", Label: "B"}},
	}})
	require.Equal(t, "/tmp/a.png", got[0].Choices[0].Image)
	require.Empty(t, got[0].Choices[1].Image)
}
