package agent

import (
	"context"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/stretchr/testify/require"
)

// An ask outlives the turn that opened it: the agent ends its turn and the
// user answers later. The form must stay answerable after that turn's
// context ends, and the answers must reach the chat like any task result.
func TestAskAcceptsAnswersAfterItsTurnEnded(t *testing.T) {
	prompts := make(chan string, 1)
	var c *coordinator
	c = sessionRunTestCoordinator(t, func(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		prompts <- call.Prompt
		return fakeSessionReply(call, "ok"), nil
	})
	questions := question.NewService()
	c.questions = questions
	c.interactive = true
	sess, err := c.sessions.Create(t.Context(), "session")
	require.NoError(t, err)

	run, owner := c.tasks.beginSessionRun(t.Context(), sess.ID)
	require.True(t, owner)
	defer c.tasks.endSessionRun(run)

	require.NoError(t, c.tasks.ask(TaskRequest{Session: sess.ID, Ask: []byte(`{"questions":[{"type":"free_text","question":"Name?","description":"d"}]}`)}))
	var request question.Request
	require.Eventually(t, func() bool {
		var ok bool
		request, ok = questions.Pending()
		return ok
	}, 5*time.Second, 10*time.Millisecond)

	// The turn ends before the user answers.
	run.cancel()
	time.Sleep(50 * time.Millisecond)
	_, stillPending := questions.Pending()
	require.True(t, stillPending, "the form stays open after its turn ends")

	require.True(t, questions.AnswerRequest(request.ID, sess.ID, []question.Answer{{QuestionID: request.Questions[0].ID, FillInText: "Ada"}}))

	select {
	case prompt := <-prompts:
		name, status, ok := ParseTaskNotification(prompt)
		require.True(t, ok, "a task result reached the chat: %q", prompt)
		require.Equal(t, AskName, name)
		require.Equal(t, AskAnswered, status)
		require.Contains(t, prompt, "Ada")
	case <-time.After(2 * time.Second):
		t.Fatal("the answers never reached the chat")
	}
}
