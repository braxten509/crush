package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/stretchr/testify/require"
)

func TestSubAgentOutputLimitRecovery(t *testing.T) {
	for _, test := range []struct {
		name        string
		first       fantasy.FinishReason
		second      fantasy.FinishReason
		firstText   string
		wantError   string
		wantContent string
		wantCalls   int
	}{
		{name: "plain report is delivered", first: fantasy.FinishReasonStop, firstText: "report", wantContent: "report", wantCalls: 1},
		{name: "reasoning exhausted before report", first: fantasy.FinishReasonLength, second: fantasy.FinishReasonStop, wantContent: "complete report", wantCalls: 2},
		{name: "partial report replaced with complete report", first: fantasy.FinishReasonLength, firstText: "unfinished", second: fantasy.FinishReasonStop, wantContent: "complete report", wantCalls: 2},
		{name: "repeated exhaustion fails", first: fantasy.FinishReasonLength, second: fantasy.FinishReasonLength, wantError: "max_tokens", wantCalls: 2},
		{name: "refusal is not retried", first: fantasy.FinishReasonContentFilter, wantError: "content_filter", wantCalls: 1},
		{name: "refusal with text is not success", first: fantasy.FinishReasonContentFilter, firstText: "refused", wantError: "content_filter", wantCalls: 1},
		{name: "provider error finish is not success", first: fantasy.FinishReasonError, firstText: "partial", wantError: "error", wantCalls: 1},
		{name: "run stopped on pending tool calls is not success", first: fantasy.FinishReasonToolCalls, firstText: "narration", wantError: "stopped before finishing", wantCalls: 1},
		{name: "unknown finish with text is delivered", first: fantasy.FinishReasonUnknown, firstText: "report", wantContent: "report", wantCalls: 1},
		{name: "other finish with text is delivered", first: fantasy.FinishReasonOther, firstText: "report", wantContent: "report", wantCalls: 1},
		{name: "unknown finish without text fails", first: fantasy.FinishReasonUnknown, wantError: "no text output", wantCalls: 1},
		{name: "whitespace report fails", first: fantasy.FinishReasonStop, firstText: " \n\t", wantError: "no text output", wantCalls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			env := testEnv(t)
			coord := newTestCoordinator(t, env, "test", config.ProviderConfig{ID: "test"})
			parent, err := env.sessions.Create(t.Context(), "parent")
			require.NoError(t, err)
			calls := 0
			var firstCall SessionAgentCall
			agent := newMockAgent("test", 128000, func(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
				calls++
				child, err := env.sessions.Get(ctx, call.SessionID)
				require.NoError(t, err)
				child.Cost += 0.01
				_, err = env.sessions.Save(ctx, child)
				require.NoError(t, err)
				if calls == 1 {
					firstCall = call
					result := agentResultWithText(test.firstText)
					result.Response.FinishReason = test.first
					return result, nil
				}
				require.Equal(t, firstCall.SessionID, call.SessionID)
				require.Equal(t, firstCall.MaxOutputTokens, call.MaxOutputTokens)
				require.Equal(t, firstCall.ProviderOptions, call.ProviderOptions)
				require.NotEqual(t, firstCall.Prompt, call.Prompt)
				require.True(t, call.reportOnly)
				result := agentResultWithText("complete report")
				result.Response.FinishReason = test.second
				return result, nil
			})
			resp, err := coord.runSubAgent(t.Context(), subAgentParams{
				Agent: agent, SessionID: parent.ID, AgentMessageID: "message", ToolCallID: "call", Prompt: "review code", SessionTitle: "review",
			})
			require.NoError(t, err)
			require.Equal(t, test.wantCalls, calls)
			if test.wantError != "" {
				require.True(t, resp.IsError)
				require.Contains(t, resp.Content, test.wantError)
			} else {
				require.False(t, resp.IsError)
				require.Equal(t, test.wantContent, resp.Content)
			}
			updated, err := env.sessions.Get(t.Context(), parent.ID)
			require.NoError(t, err)
			require.InDelta(t, float64(calls)*0.01, updated.Cost, 1e-9, "failed attempts still incurred cost")
		})
	}
}

func testStep(reason fantasy.FinishReason, text string) fantasy.StepResult {
	var content fantasy.ResponseContent
	if text != "" {
		content = fantasy.ResponseContent{fantasy.TextContent{Text: text}}
	}
	return fantasy.StepResult{Response: fantasy.Response{Content: content, FinishReason: reason}}
}

// multiStepResult shapes a result the way fantasy does: its Response is the
// latest step that has text, whatever the last step was.
func multiStepResult(steps ...fantasy.StepResult) *fantasy.AgentResult {
	result := &fantasy.AgentResult{Steps: steps}
	for _, step := range steps {
		if strings.TrimSpace(step.Content.Text()) != "" {
			result.Response = step.Response
		}
	}
	return result
}

func TestTerminalFinishReason(t *testing.T) {
	require.Equal(t, fantasy.FinishReasonLength,
		terminalFinishReason(&fantasy.AgentResult{Response: fantasy.Response{FinishReason: fantasy.FinishReasonLength}}),
		"without steps the response says how it ended")
	result := multiStepResult(testStep(fantasy.FinishReasonToolCalls, "text"), testStep(fantasy.FinishReasonLength, ""))
	require.Equal(t, fantasy.FinishReasonToolCalls, result.Response.FinishReason, "the response is an earlier step's")
	require.Equal(t, fantasy.FinishReasonLength, terminalFinishReason(result), "the last step says how the run ended")
}

func TestSubAgentReportJudgesTheLastStep(t *testing.T) {
	for _, test := range []struct {
		name        string
		first       *fantasy.AgentResult
		wantError   string
		wantContent string
		wantCalls   int
	}{
		{
			name:        "last step out of tokens after an earlier step with text recovers",
			first:       multiStepResult(testStep(fantasy.FinishReasonToolCalls, "I will look."), testStep(fantasy.FinishReasonLength, "")),
			wantContent: "complete report",
			wantCalls:   2,
		},
		{
			name:        "blank last step keeps the earlier report",
			first:       multiStepResult(testStep(fantasy.FinishReasonToolCalls, "the report"), testStep(fantasy.FinishReasonStop, "")),
			wantContent: "the report",
			wantCalls:   1,
		},
		{
			name:      "last step refused after earlier text is not retried",
			first:     multiStepResult(testStep(fantasy.FinishReasonToolCalls, "I will look."), testStep(fantasy.FinishReasonContentFilter, "")),
			wantError: "content_filter",
			wantCalls: 1,
		},
		{
			name:      "last step still calling tools is incomplete",
			first:     multiStepResult(testStep(fantasy.FinishReasonToolCalls, "I will look.")),
			wantError: "stopped before finishing",
			wantCalls: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			agent := newMockAgent("test", 128000, func(context.Context, SessionAgentCall) (*fantasy.AgentResult, error) {
				calls++
				if calls == 1 {
					return test.first, nil
				}
				result := agentResultWithText("complete report")
				result.Response.FinishReason = fantasy.FinishReasonStop
				return result, nil
			})
			result, err := runSubAgentReport(t.Context(), agent, SessionAgentCall{})
			require.Equal(t, test.wantCalls, calls)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantContent, subAgentOutput(result))
		})
	}
}

func TestSubAgentRecoveryHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	agent := newMockAgent("test", 128000, func(context.Context, SessionAgentCall) (*fantasy.AgentResult, error) {
		calls++
		cancel()
		return &fantasy.AgentResult{Response: fantasy.Response{FinishReason: fantasy.FinishReasonLength}}, nil
	})
	_, err := runSubAgentReport(ctx, agent, SessionAgentCall{})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, calls)

	agent.runFunc = func(context.Context, SessionAgentCall) (*fantasy.AgentResult, error) {
		calls++
		return nil, errors.New("provider failed")
	}
	_, err = runSubAgentReport(t.Context(), agent, SessionAgentCall{})
	require.ErrorContains(t, err, "provider failed")
	require.Equal(t, 2, calls, "provider errors are not retried")
}

type reportRecoveryModel struct {
	finishStreamModel
	calls []fantasy.Call
}

func (m *reportRecoveryModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.calls = append(m.calls, call)
	if len(m.calls) == 1 {
		return func(yield func(fantasy.StreamPart) bool) {
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "partial"})
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "partial", Delta: "previous findings"})
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "partial"})
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonLength})
		}, nil
	}
	return m.finishStreamModel.Stream(ctx, call)
}

func TestSubAgentRecoveryRetainsHistoryAndForbidsToolCalls(t *testing.T) {
	env := testEnv(t)
	model := &reportRecoveryModel{finishStreamModel: finishStreamModel{text: "final report"}}
	agent := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	agent.isSubAgent = true
	agent.SetTools([]fantasy.AgentTool{&fakeTool{name: "work", resp: fantasy.NewTextResponse("ok")}})
	sess, err := env.sessions.Create(t.Context(), "worker")
	require.NoError(t, err)
	result, err := runSubAgentReport(t.Context(), agent, SessionAgentCall{SessionID: sess.ID, Prompt: "review code", MaxOutputTokens: 128000})
	require.NoError(t, err)
	require.Equal(t, "final report", subAgentOutput(result))
	require.Len(t, model.calls, 2)
	require.NotNil(t, model.calls[1].ToolChoice)
	require.Equal(t, fantasy.ToolChoiceNone, *model.calls[1].ToolChoice)
	require.Equal(t, model.calls[0].MaxOutputTokens, model.calls[1].MaxOutputTokens)
	require.NotEmpty(t, model.calls[0].Tools)
	// Anthropic refuses tool-call history without tool definitions, so the
	// tools stay defined and tool_choice none is what forbids calling them.
	require.NotEmpty(t, model.calls[1].Tools)
	// Inspect only synthetic history, never real saved model reasoning.
	require.Contains(t, fmt.Sprint(model.calls[1].Prompt), "previous findings")
}

// exhaustedAfterToolModel narrates and calls a tool on its first step, runs
// out of tokens on the next before writing anything, and reports when asked.
type exhaustedAfterToolModel struct {
	finishStreamModel
	calls []fantasy.Call
}

func (m *exhaustedAfterToolModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.calls = append(m.calls, call)
	switch len(m.calls) {
	case 1:
		return func(yield func(fantasy.StreamPart) bool) {
			_ = yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "note"}) &&
				yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "note", Delta: "I will check the code."}) &&
				yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "note"}) &&
				yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolCall, ID: "work1", ToolCallName: "work", ToolCallInput: "{}"}) &&
				yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonToolCalls})
		}, nil
	case 2:
		return func(yield func(fantasy.StreamPart) bool) {
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonLength})
		}, nil
	}
	return m.finishStreamModel.Stream(ctx, call)
}

// A run that wrote some text early and then ran out of tokens on its last
// step must still be recovered: the result's response is the earlier step's.
func TestSubAgentRecoveryWhenTheLastStepRunsOutAfterEarlierText(t *testing.T) {
	env := testEnv(t)
	model := &exhaustedAfterToolModel{finishStreamModel: finishStreamModel{text: "final report"}}
	agent := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	agent.isSubAgent = true
	tool := &fakeTool{name: "work", resp: fantasy.NewTextResponse("ok")}
	agent.SetTools([]fantasy.AgentTool{tool})
	sess, err := env.sessions.Create(t.Context(), "worker")
	require.NoError(t, err)
	result, err := runSubAgentReport(t.Context(), agent, SessionAgentCall{SessionID: sess.ID, Prompt: "review code", MaxOutputTokens: 128000})
	require.NoError(t, err)
	require.True(t, tool.called)
	require.Equal(t, "final report", subAgentOutput(result))
	require.Len(t, model.calls, 3, "tool step, exhausted step, then one recovery")
	require.NotNil(t, model.calls[2].ToolChoice)
	require.Equal(t, fantasy.ToolChoiceNone, *model.calls[2].ToolChoice)
	require.NotEmpty(t, model.calls[2].Tools, "tool-call history needs the tool definitions")
}

// truncatedToolInputModel is cut off by the output limit in the middle of
// writing a tool call, then reports when asked.
type truncatedToolInputModel struct {
	finishStreamModel
	calls []fantasy.Call
}

func (m *truncatedToolInputModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.calls = append(m.calls, call)
	if len(m.calls) == 1 {
		return func(yield func(fantasy.StreamPart) bool) {
			_ = yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputStart, ID: "cut", ToolCallName: "work"}) &&
				yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputDelta, ID: "cut", Delta: `{"path":`}) &&
				yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonLength})
		}, nil
	}
	return m.finishStreamModel.Stream(ctx, call)
}

// unmatchedToolCalls lists tool call IDs that have no result, or results with
// no call, in a prompt: providers reject such history.
func unmatchedToolCalls(prompt fantasy.Prompt) []string {
	calls, results := map[string]bool{}, map[string]bool{}
	for _, msg := range prompt {
		for _, part := range msg.Content {
			if call, ok := fantasy.AsMessagePart[fantasy.ToolCallPart](part); ok {
				calls[call.ToolCallID] = true
			}
			if result, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](part); ok {
				results[result.ToolCallID] = true
			}
		}
	}
	var unmatched []string
	for id := range calls {
		if !results[id] {
			unmatched = append(unmatched, id)
		}
	}
	for id := range results {
		if !calls[id] {
			unmatched = append(unmatched, id)
		}
	}
	sort.Strings(unmatched)
	return unmatched
}

func TestSubAgentRecoveryAfterTruncatedToolInput(t *testing.T) {
	env := testEnv(t)
	model := &truncatedToolInputModel{finishStreamModel: finishStreamModel{text: "final report"}}
	agent := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	agent.isSubAgent = true
	agent.SetTools([]fantasy.AgentTool{&fakeTool{name: "work", resp: fantasy.NewTextResponse("ok")}})
	sess, err := env.sessions.Create(t.Context(), "worker")
	require.NoError(t, err)
	result, err := runSubAgentReport(t.Context(), agent, SessionAgentCall{SessionID: sess.ID, Prompt: "review code", MaxOutputTokens: 128000})
	require.NoError(t, err)
	require.Equal(t, "final report", subAgentOutput(result))
	require.Len(t, model.calls, 2)
	require.Empty(t, unmatchedToolCalls(model.calls[1].Prompt), "the recovery request carries well-formed tool history")
	require.NotEmpty(t, model.calls[1].Tools, "and the tool definitions it needs")
	require.NotNil(t, model.calls[1].ToolChoice)
	require.Equal(t, fantasy.ToolChoiceNone, *model.calls[1].ToolChoice)
}

func TestBackgroundSubAgentReportsExhaustionAsFailed(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var notification string
	c := sessionRunTestCoordinator(t, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		notification = call.Prompt
		return fakeSessionReply(call, "received"), nil
	})
	c.interactive = true
	parent, err := c.sessions.Create(ctx, "parent")
	require.NoError(t, err)
	child, err := c.sessions.CreateTaskSession(ctx, "worker", parent.ID, "worker")
	require.NoError(t, err)
	broker := pubsub.NewBroker[Task]()
	t.Cleanup(broker.Shutdown)
	h := &taskHub{c: c, dir: t.TempDir(), tasks: map[string]*Task{}, cancels: map[string]context.CancelFunc{}, events: broker}
	c.tasks = h
	m := Model{CatwalkCfg: catwalk.Model{DefaultMaxTokens: 4096}, ModelCfg: config.SelectedModel{MaxTokens: 8192}}
	agent := newMockAgent("test", 4096, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		require.Equal(t, int64(8192), call.MaxOutputTokens, "background call honors override")
		return &fantasy.AgentResult{Response: fantasy.Response{FinishReason: fantasy.FinishReasonLength}}, nil
	})
	released := make(chan struct{})
	h.start(ctx, func() { close(released) }, &Task{SessionID: parent.ID, ChildID: child.ID, Status: TaskRunning}, agent, m, config.ProviderConfig{ID: "test"}, "review")
	select {
	case <-released:
	case <-ctx.Done():
		t.Fatal("task did not deliver its result")
	}
	require.Contains(t, notification, "<status>failed</status>")
	require.Contains(t, notification, "max_tokens")
	require.NotContains(t, notification, "(no final message)")
}
