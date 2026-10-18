package agent

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/shell"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// A prompt marked with WithNonInteractive on an interactive coordinator, as
// server-mode `crush run` sends one to a workspace an app has open, runs as
// a non-interactive coordinator's prompts do.

// TestNonInteractivePromptOnInteractiveCoordinator: the marked prompt's run
// waits for its follow-ups. The agent's own unmarked turn in its chat, as an
// agent CLI's reply between turns is, joins that run, and both are
// non-interactive calls. An unmarked prompt in another chat, sent while the
// run is open, stays interactive and returns at once.
func TestNonInteractivePromptOnInteractiveCoordinator(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	reserved, deliver := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	nonInteractive := map[string]bool{}
	var c *coordinator
	c = sessionRunTestCoordinator(t, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		mu.Lock()
		nonInteractive[call.Prompt] = call.NonInteractive
		mu.Unlock()
		if call.Prompt == "main" {
			_, release := c.tasks.reserveFollowUp(call.SessionID)
			close(reserved)
			go func() {
				defer release()
				select {
				case <-deliver:
				case <-ctx.Done():
					return
				}
				// As cliagent.OnUnprompted starts it: hidden, no submission ID.
				replyCtx := withCLIContinue(message.WithHiddenUserMessage(context.Background()))
				_, _ = c.Run(replyCtx, call.SessionID, "reply")
			}()
		}
		return fakeSessionReply(call, call.Prompt), nil
	})
	c.interactive = true

	done := make(chan *fantasy.AgentResult, 1)
	go func() {
		result, err := c.Run(WithNonInteractive(WithRunID(ctx, "run-main")), "session", "main")
		if err != nil {
			t.Errorf("run: %v", err)
		}
		done <- result
	}()
	select {
	case <-reserved:
	case <-ctx.Done():
		t.Fatal("the marked prompt never ran")
	}
	result, err := c.Run(ctx, "other", "interactive")
	require.NoError(t, err)
	require.Equal(t, "interactive", result.Response.Content.Text())
	select {
	case <-done:
		t.Fatal("the marked prompt's run ended before its follow-up")
	default:
	}

	close(deliver)
	select {
	case result := <-done:
		require.Equal(t, "reply", result.Response.Content.Text(), "the run ends with the turn that joined it")
	case <-ctx.Done():
		t.Fatal("the marked prompt's run did not end")
	}
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, map[string]bool{"main": true, "interactive": false, "reply": true}, nonInteractive)
}

// TestUserPromptDuringMarkedRunRunsOnItsOwn: an app's own prompt, which has
// a submission ID as every app prompt does, sent to a chat whose marked
// prompt's run is open does not join that run, unlike the agent's own turns
// there. It is an interactive call with its own answer, and the run's
// answer stays the run's.
func TestUserPromptDuringMarkedRunRunsOnItsOwn(t *testing.T) {
	userPromptDuringMarkedRun(t, func(ctx context.Context) context.Context {
		return message.WithSubmissionID(ctx, "typed")
	})
}

// TestHiddenUserPromptDuringMarkedRunRunsOnItsOwn: the terminal's hidden
// prompt, such as "Implement the plan." on leaving plan mode, has no
// submission ID but is still the user's, so it does not join the run either.
func TestHiddenUserPromptDuringMarkedRunRunsOnItsOwn(t *testing.T) {
	userPromptDuringMarkedRun(t, message.WithHiddenUserMessage)
}

func userPromptDuringMarkedRun(t *testing.T, userCtx func(context.Context) context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	releases := make(chan func(), 1)
	var mu sync.Mutex
	nonInteractive := map[string]bool{}
	var c *coordinator
	c = sessionRunTestCoordinator(t, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		mu.Lock()
		nonInteractive[call.Prompt] = call.NonInteractive
		mu.Unlock()
		if call.Prompt == "main" {
			_, release := c.tasks.reserveFollowUp(call.SessionID)
			releases <- release
		}
		return fakeSessionReply(call, call.Prompt), nil
	})
	c.interactive = true

	done := make(chan *fantasy.AgentResult, 1)
	go func() {
		result, err := c.Run(WithNonInteractive(WithRunID(ctx, "run-main")), "session", "main")
		if err != nil {
			t.Errorf("run: %v", err)
		}
		done <- result
	}()
	var release func()
	select {
	case release = <-releases:
	case <-ctx.Done():
		t.Fatal("the marked prompt never ran")
	}
	result, err := c.Run(userCtx(ctx), "session", "typed")
	require.NoError(t, err)
	require.Equal(t, "typed", result.Response.Content.Text())

	release()
	select {
	case result := <-done:
		require.Equal(t, "main", result.Response.Content.Text(), "the run's answer is its own")
	case <-ctx.Done():
		t.Fatal("the marked prompt's run did not end")
	}
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, map[string]bool{"main": true, "typed": false}, nonInteractive)
}

// TestNonInteractivePromptAskIsCancelled: `crush ask` from a marked prompt's
// run gets the cancelled answer at once, as on a non-interactive
// coordinator, instead of a form nothing answers.
func TestNonInteractivePromptAskIsCancelled(t *testing.T) {
	var c *coordinator
	asked := make(chan struct{})
	c = sessionRunTestCoordinator(t, func(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		if call.Prompt == "main" {
			if err := c.tasks.ask(TaskRequest{Session: call.SessionID, Ask: []byte(`{"questions":[{"type":"free_text","question":"Choose?","description":"Need input"}]}`)}); err != nil {
				return nil, err
			}
			result := fakeSessionReply(call, "questions open")
			close(asked)
			return result, nil
		}
		select {
		case <-asked:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if name, status, ok := ParseTaskNotification(call.Prompt); !ok || name != AskName || status != AskCancelled {
			return nil, errors.New("question result was not cancelled")
		}
		return fakeSessionReply(call, "questions cancelled"), nil
	})
	c.interactive = true
	c.questions = question.NewService()
	sess, err := c.sessions.Create(t.Context(), "session")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result, err := c.Run(WithNonInteractive(ctx), sess.ID, "main")
	require.NoError(t, err)
	require.Equal(t, "questions cancelled", result.Response.Content.Text())
	require.False(t, c.questions.Cancel(), "no question form should be left waiting")
}

// TestNonInteractivePromptWaitsForMCP: a marked prompt waits for MCP
// initialization, as a non-interactive run does.
func TestNonInteractivePromptWaitsForMCP(t *testing.T) {
	coord := newGateTestCoordinator(t, true)
	mcp.ArmInit()
	t.Cleanup(mcp.DisarmInit)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := coord.run(WithNonInteractive(ctx), nil, "test-session", "hello")
	require.ErrorContains(t, err, "MCP initialization")
}

// TestNonInteractivePromptJobIsNotSaved: a marked prompt's job ends with
// its run, as a non-interactive coordinator's does, so it is not saved for
// a later Crush to report. An unmarked prompt's job still is.
func TestNonInteractivePromptJobIsNotSaved(t *testing.T) {
	c := resumeTestCoordinator(t, replyOK)
	h := resumeTestHub(t, c)
	parent, _ := helperChats(t, c)
	jobs := shell.GetBackgroundShellManager()
	saved := func(ctx context.Context) bool {
		job, err := jobs.Start(t.Context(), t.TempDir(), nil, "sleep 30", "job", h.env(parent.ID)...)
		require.NoError(t, err)
		t.Cleanup(func() {
			_ = jobs.Kill(job.ID)
			_ = jobs.Remove(job.ID)
		})
		h.watchJob(ctx, func() {}, parent.ID, uuid.NewString(), job, false)
		return slices.ContainsFunc(readSavedJobs(t, h.savedDataPath(savedJobsFile)), func(s savedJob) bool { return s.ID == job.ID })
	}
	require.True(t, saved(t.Context()), "an unmarked prompt's job is saved")
	require.False(t, saved(WithNonInteractive(t.Context())), "a marked prompt's job is not")
}

// TestNonInteractivePromptsAreNotMerged: queued marked and unmarked prompts
// never share a turn, whose tools differ.
func TestNonInteractivePromptsAreNotMerged(t *testing.T) {
	var s sessionScheduler
	marked, unmarked := SessionAgentCall{NonInteractive: true}, SessionAgentCall{}
	require.False(t, s.compatibleQueuedCalls(marked, unmarked))
	require.False(t, s.compatibleQueuedCalls(unmarked, marked))
	require.True(t, s.compatibleQueuedCalls(marked, marked))
}
