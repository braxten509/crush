package agent

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
)

//go:embed templates/summary.md
var summaryPrompt []byte

func (a *sessionAgent) Summarize(ctx context.Context, sessionID string, opts fantasy.ProviderOptions, onAuthRefresh func(context.Context, *fantasy.ProviderError) error) error {
	if a.IsSessionBusy(sessionID) {
		return ErrSessionBusy
	}
	// A summary replaces the agent's memory: resend kept skills in full.
	defer forgetKeptSkillsSent(sessionID)

	// A chat is compacted on its own model (owner, 2026-10-24).
	largeModel := a.turnModel(ctx, sessionID)
	systemPromptPrefix := a.promptPrefixFor(largeModel)

	currentSession, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}
	msgs, err := a.getSessionMessages(ctx, currentSession)
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		// Nothing to summarize.
		return nil
	}

	aiMsgs, _ := a.preparePrompt(msgs, largeModel.CatwalkCfg.SupportsImages)
	aiMsgs = a.trimPreparedPrompt(sessionID, msgs, aiMsgs, largeModel, approxTokenCount(string(summaryPrompt))+approxTokenCount(systemPromptPrefix))

	genCtx, cancel := context.WithCancel(ctx)
	ac := &activeCancel{cancel: cancel}
	a.activeRequests.Set(sessionID, ac)
	defer a.activeRequests.CompareAndDelete(sessionID, ac)
	defer cancel()
	defer func() {
		if flushErr := a.messages.FlushAll(ctx); flushErr != nil {
			slog.Error("Failed to flush pending message updates after summarize", "error", flushErr)
		}
	}()

	agent := fantasy.NewAgent(
		largeModel.Model,
		fantasy.WithSystemPrompt(string(summaryPrompt)),
		fantasy.WithUserAgent(userAgent),
	)
	summaryMessage, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:             message.Assistant,
		Model:            largeModel.ModelCfg.Model,
		Provider:         largeModel.ModelCfg.Provider,
		IsSummaryMessage: true,
	})
	if err != nil {
		return err
	}

	summaryPromptText := buildSummaryPrompt(currentSession.Todos)

	var resp *fantasy.AgentResult
	if cliModel, ok := largeModel.Model.(*cliagent.Model); ok {
		resp, err = a.cliSummarize(genCtx, cliModel, sessionID, largeModel.ModelCfg.ReasoningEffort, msgs, summaryPromptText, &summaryMessage)
	}
	if resp == nil && err == nil {
		resp, err = agent.Stream(genCtx, a.summaryStreamCall(genCtx, sessionID, largeModel, summaryPromptText, aiMsgs, opts, onAuthRefresh, systemPromptPrefix, &summaryMessage))
	}
	if err != nil {
		return a.failSummary(ctx, &summaryMessage, err)
	}

	summaryMessage.AddFinish(message.FinishReasonEndTurn, "", "")
	err = a.messages.Update(genCtx, summaryMessage)
	if err != nil {
		return err
	}

	if err := a.saveSummaryUsage(genCtx, largeModel, currentSession, resp, summaryMessage); err != nil {
		return err
	}
	return a.runQueuedAfterSummary(ctx, sessionID, cancel)
}

// summaryStreamCall streams the summary into summaryMessage as it arrives.
func (a *sessionAgent) summaryStreamCall(genCtx context.Context, sessionID string, model Model, summaryPromptText string, aiMsgs []fantasy.Message, opts fantasy.ProviderOptions, onAuthRefresh func(context.Context, *fantasy.ProviderError) error, systemPromptPrefix string, summaryMessage *message.Message) fantasy.AgentStreamCall {
	return fantasy.AgentStreamCall{
		Prompt:          summaryPromptText,
		Messages:        aiMsgs,
		Headers:         sessionHeaders(sessionID),
		ProviderOptions: opts,
		OnAuthRefresh:   onAuthRefresh,
		ModelProvider: func() fantasy.LanguageModel {
			return model.Model
		},
		PrepareStep: func(callContext context.Context, options fantasy.PrepareStepFunctionOptions) (_ context.Context, prepared fantasy.PrepareStepResult, err error) {
			prepared.Messages = options.Messages
			if systemPromptPrefix != "" {
				prepared.Messages = append([]fantasy.Message{fantasy.NewSystemMessage(systemPromptPrefix)}, prepared.Messages...)
			}
			return callContext, prepared, nil
		},
		OnReasoningDelta: func(id string, text string) error {
			summaryMessage.AppendReasoningContent(text)
			return a.messages.Update(genCtx, *summaryMessage)
		},
		OnReasoningEnd: func(id string, reasoning fantasy.ReasoningContent) error {
			// Handle anthropic signature.
			if anthropicData, ok := reasoning.ProviderMetadata["anthropic"]; ok {
				if signature, ok := anthropicData.(*anthropic.ReasoningOptionMetadata); ok && signature.Signature != "" {
					summaryMessage.AppendReasoningSignature(signature.Signature)
				}
			}
			summaryMessage.FinishThinking()
			return a.messages.Update(genCtx, *summaryMessage)
		},
		OnTextDelta: func(id, text string) error {
			summaryMessage.AppendContent(text)
			return a.messages.Update(genCtx, *summaryMessage)
		},
	}
}

// failSummary ends a summary that failed. A cancelled summary is removed;
// any other failure is recorded on the summary message so the UI stops
// spinning.
func (a *sessionAgent) failSummary(ctx context.Context, summaryMessage *message.Message, err error) error {
	isCancelErr := errors.Is(err, context.Canceled)
	if isCancelErr {
		// User cancelled summarize we need to remove the summary message.
		deleteErr := a.messages.Delete(ctx, summaryMessage.ID)
		return deleteErr
	}
	// Mark the summary message as finished with an error so the UI
	// stops spinning.
	summaryMessage.AddFinish(message.FinishReasonError, "Summarization Error", err.Error())
	if updateErr := a.messages.Update(ctx, *summaryMessage); updateErr != nil {
		return updateErr
	}
	return err
}

// saveSummaryUsage records the summary's cost and makes it the session's
// summary message, resetting the context token counts.
func (a *sessionAgent) saveSummaryUsage(ctx context.Context, largeModel Model, currentSession session.Session, resp *fantasy.AgentResult, summaryMessage message.Message) error {
	openrouterCost := a.stepsOpenrouterCost(resp.Steps)
	// Just in case, get just the last usage info.
	usage := resp.Response.Usage
	_, err := a.sessions.Edit(ctx, currentSession.ID, func(current *session.Session) bool {
		a.updateSessionUsage(largeModel, current, resp.TotalUsage, openrouterCost, false)
		current.SummaryMessageID = summaryMessage.ID
		current.CompletionTokens = summaryCompletionTokens(usage, summaryMessage)
		current.PromptTokens = 0
		current.EstimatedUsage = usageIsZero(usage)
		return true
	})
	return err
}

// runQueuedAfterSummary releases the summary's active request and runs the
// prompts queued while it was summarizing.
func (a *sessionAgent) runQueuedAfterSummary(ctx context.Context, sessionID string, cancel context.CancelFunc) error {
	// Release the active request before processing queued messages so that
	// Run() does not see the session as busy.
	mu := a.sessionMu(sessionID)
	mu.Lock()
	a.activeRequests.Del(sessionID)
	cancel()

	// Process any messages that were queued while summarizing.
	queuedMessages, ok := a.messageQueue.Get(sessionID)
	if !ok || len(queuedMessages) == 0 {
		mu.Unlock()
		return nil
	}
	calls, canceled := a.drainQueueForStepLocked(sessionID)
	if len(calls) == 0 {
		mu.Unlock()
		a.publishCanceledQueueDrops(canceled)
		return nil
	}
	next := a.reserveBatchLocked(calls)
	mu.Unlock()
	a.publishCanceledQueueDrops(canceled)
	_, qErr := a.Run(ctx, next)
	return qErr
}

// buildSummaryPrompt constructs the prompt text for session summarization.
func buildSummaryPrompt(todos []session.Todo) string {
	var sb strings.Builder
	sb.WriteString("Provide a detailed summary of our conversation above.")
	if len(todos) > 0 {
		sb.WriteString("\n\n## Current Todo List\n\n")
		for _, t := range todos {
			fmt.Fprintf(&sb, "- [%s] %s\n", t.Status, t.Content)
		}
		sb.WriteString("\nInclude these tasks and their statuses in your summary. ")
		sb.WriteString("Instruct the resuming assistant to use the `todos` tool to continue tracking progress on these tasks.")
	}
	return sb.String()
}

func summaryCompletionTokens(usage fantasy.Usage, summaryMessage message.Message) int64 {
	if usage.OutputTokens != 0 {
		return usage.OutputTokens
	}
	return approxTokenCount(summaryMessage.Content().Text) + approxTokenCount(summaryMessage.ReasoningContent().String())
}
