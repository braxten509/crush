package question

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/stretchr/testify/require"
)

type askResult struct {
	answers []Answer
	err     error
}

func startQuestion(t *testing.T, s *questionService, ctx context.Context, events <-chan pubsub.Event[Request], id, sessionID string) <-chan askResult {
	t.Helper()
	done := make(chan askResult, 1)
	go func() {
		answers, err := s.Ask(ctx, Request{ID: id, SessionID: sessionID, Questions: []Question{{
			ID: "question", Type: TypeYesNo, Text: "Continue?", Description: "Choose whether to continue.",
		}}})
		done <- askResult{answers: answers, err: err}
	}()
	select {
	case event := <-events:
		require.Equal(t, id, event.Payload.ID)
	case <-time.After(5 * time.Second):
		t.Fatal("question was not published")
	}
	return done
}

func awaitQuestion(t *testing.T, done <-chan askResult) askResult {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("question was not resolved")
		return askResult{}
	}
}

func TestScopedResolutionRejectsStaleRequestAndSession(t *testing.T) {
	t.Parallel()
	for _, cancel := range []bool{false, true} {
		s := NewService()
		events := s.Subscribe(t.Context())
		old := startQuestion(t, s, t.Context(), events, "old", "s1")
		require.True(t, s.AnswerRequest("old", "s1", nil))
		require.NoError(t, awaitQuestion(t, old).err)
		current := startQuestion(t, s, t.Context(), events, "new", "s2")
		for _, scope := range [][2]string{{"old", "s1"}, {"old", "s2"}, {"new", "s1"}, {"", "s2"}, {"new", ""}} {
			if cancel {
				require.False(t, s.CancelRequest(scope[0], scope[1]))
			} else {
				require.False(t, s.AnswerRequest(scope[0], scope[1], nil))
			}
			pending, ok := s.Pending()
			require.True(t, ok)
			require.Equal(t, "new", pending.ID)
		}
		if cancel {
			require.True(t, s.CancelRequest("new", "s2"))
			require.ErrorIs(t, awaitQuestion(t, current).err, ErrCancelled)
		} else {
			answers := []Answer{{QuestionID: "question", FillInText: "current"}}
			require.True(t, s.AnswerRequest("new", "s2", answers))
			result := awaitQuestion(t, current)
			require.NoError(t, result.err)
			require.Equal(t, answers, result.answers)
		}
	}
}

func TestConcurrentQuestionResolutionHasOneWinner(t *testing.T) {
	t.Parallel()
	s := NewService()
	events := s.Subscribe(t.Context())
	notifications := s.SubscribeNotifications(t.Context())
	done := startQuestion(t, s, t.Context(), events, "batch", "session")
	var winners atomic.Int32
	var callers sync.WaitGroup
	ready := make(chan struct{})
	for i := 0; i < 32; i++ {
		callers.Go(func() {
			<-ready
			var won bool
			switch i % 4 {
			case 0:
				won = s.Answer(nil)
			case 1:
				won = s.Cancel()
			case 2:
				won = s.AnswerRequest("batch", "session", nil)
			case 3:
				won = s.CancelRequest("batch", "session")
			}
			if won {
				winners.Add(1)
			}
		})
	}
	close(ready)
	finished := make(chan struct{})
	go func() { callers.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent resolution blocked")
	}
	require.Equal(t, int32(1), winners.Load())
	result := awaitQuestion(t, done)
	if result.err != nil {
		require.ErrorIs(t, result.err, ErrCancelled)
	}
	_, pending := s.Pending()
	require.False(t, pending)
	select {
	case event := <-notifications:
		require.Equal(t, "batch", event.Payload.BatchID)
	case <-time.After(5 * time.Second):
		t.Fatal("resolution was not published")
	}
	select {
	case <-notifications:
		t.Fatal("resolution was published more than once")
	default:
	}
}

func TestExpiredQuestionCannotClearReplacement(t *testing.T) {
	t.Parallel()
	s := NewService()
	events := s.Subscribe(t.Context())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	old := startQuestion(t, s, ctx, events, "old", "s1")
	current := startQuestion(t, s, t.Context(), events, "new", "s2")
	cancel()
	require.ErrorIs(t, awaitQuestion(t, old).err, context.Canceled)
	pending, ok := s.Pending()
	require.True(t, ok)
	require.Equal(t, "new", pending.ID)
	require.True(t, s.CancelRequest("new", "s2"))
	require.ErrorIs(t, awaitQuestion(t, current).err, ErrCancelled)
}

// The shared service reaches phones and servers, so it never publishes a
// secure entry.
func TestServiceRefusesSecureEntries(t *testing.T) {
	t.Parallel()
	s := NewService()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	events := s.Subscribe(ctx)
	_, err := s.Ask(ctx, Request{SessionID: "s", Questions: []Question{
		{Type: TypeFreeText, Text: "Name?", Description: "d"},
		{Type: TypeSecureEntry, Text: "Key?", Description: "d"},
	}})
	require.ErrorIs(t, err, ErrSecureEntryLocal)
	select {
	case <-events:
		t.Fatal("a secure entry was published")
	case <-time.After(50 * time.Millisecond):
	}
	_, pending := s.Pending()
	require.False(t, pending)
}

func TestSecureEntryValidationAndPrepare(t *testing.T) {
	t.Parallel()
	r := Request{Questions: []Question{
		{Type: TypeSecureEntry, Text: "Key?", Description: "d"},
		{Type: TypeYesNo, Text: "Ok?", Description: "d"},
	}}
	require.True(t, r.HasSecureEntry())
	r.Prepare()
	require.NotEmpty(t, r.ID)
	require.NotEmpty(t, r.Questions[0].ID)
	require.NotEmpty(t, r.ConfirmTitle)
	require.NoError(t, r.Validate())
	r.Questions[0].Choices = []Choice{{ID: "a", Label: "A"}}
	require.ErrorContains(t, r.Validate(), "no choices")
}
