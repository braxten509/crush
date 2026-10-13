package client

import (
	"context"
	"net/url"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
	"github.com/google/uuid"
)

// GetAgentInfo retrieves the agent status for a workspace.
func (c *Client) GetAgentInfo(ctx context.Context, id string) (*proto.AgentInfo, error) {
	info, err := newCall(c, rpc.AgentInfo, "get agent status", "agent status").at(id).get(ctx)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// InitiateAgentProcessing triggers agent initialization on the server.
func (c *Client) InitiateAgentProcessing(ctx context.Context, id string, interactive bool) error {
	_, err := newCall(c, rpc.InitAgent, "initiate session agent processing", "").at(id).
		do(ctx, proto.AgentInitRequest{Interactive: interactive})
	return err
}

// UpdateAgent triggers an agent model update on the server.
func (c *Client) UpdateAgent(ctx context.Context, id string) error {
	_, err := newCall(c, rpc.UpdateAgent, "update agent", "").at(id).get(ctx)
	return err
}

// SetMainAgent switches the workspace's active agent (e.g. "coder" or
// "plan") on the server.
func (c *Client) SetMainAgent(ctx context.Context, id, agentID string) error {
	_, err := newCall(c, rpc.SetMainAgent, "set main agent", "").at(id).
		do(ctx, proto.AgentSetMainRequest{AgentID: agentID})
	return err
}

// SendMessage sends a message to the agent for a workspace.
//
// When runID is non-empty it is echoed back on the resulting
// proto.RunComplete event, giving the caller a unique correlator
// for completion detection. Pass "" when the caller does not need
// to distinguish its own turn's terminal event from any concurrent
// turn on the same session (e.g. interactive TUI usage).
//
// A refusal by the server is a [message.DefinitiveSubmissionError]: the
// prompt was not accepted. A transport failure is returned as is, since
// the server may have accepted the prompt before the connection broke.
func (c *Client) SendMessage(ctx context.Context, id string, sessionID, runID, channel, prompt string, attachments ...message.Attachment) error {
	_, err := newCall(c, rpc.SendMessage, "send message to agent", "").at(id).do(ctx, proto.AgentMessage{
		SubmissionID:      message.SubmissionID(ctx),
		HiddenUserMessage: message.HiddenUserMessage(ctx),
		SessionID:         sessionID,
		RunID:             runID,
		Channel:           channel,
		Prompt:            prompt,
		Attachments:       proto.AttachmentsFromMessage(attachments),
	})
	if isRefused(err) {
		return &message.DefinitiveSubmissionError{Err: err}
	}
	return err
}

// GetAgentSessionInfo retrieves the agent session info for a workspace.
func (c *Client) GetAgentSessionInfo(ctx context.Context, id string, sessionID string) (*proto.AgentSession, error) {
	info, err := newCall(c, rpc.AgentSession, "get session agent info", "session agent info").at(id, sessionID).get(ctx)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// CancelAgentSession cancels an ongoing agent operation for a session.
func (c *Client) CancelAgentSession(ctx context.Context, id string, sessionID string) error {
	_, err := newCall(c, rpc.CancelAgent, "cancel agent session", "").at(id, sessionID).get(ctx)
	return err
}

// InterruptAgentSession stops the active run and lets the next queued prompt run.
func (c *Client) InterruptAgentSession(ctx context.Context, id string, sessionID string) error {
	_, err := newCall(c, rpc.InterruptAgent, "interrupt session agent", "").at(id, sessionID).get(ctx)
	return err
}

// AgentSummarizeSession requests a session summarization.
func (c *Client) AgentSummarizeSession(ctx context.Context, id string, sessionID string) error {
	_, err := newCall(c, rpc.SummarizeSession, "summarize session", "").at(id, sessionID).get(ctx)
	return err
}

// GetAgentSessionQueuedPrompts retrieves the number of queued prompts for a
// session.
func (c *Client) GetAgentSessionQueuedPrompts(ctx context.Context, id string, sessionID string) (int, error) {
	return newCall(c, rpc.QueuedCount, "get session agent queued prompts", "session agent queued prompts").
		at(id, sessionID).get(ctx)
}

// GetAgentSessionQueuedPromptsList retrieves the list of queued prompt
// strings for a session.
func (c *Client) GetAgentSessionQueuedPromptsList(ctx context.Context, id string, sessionID string) ([]string, error) {
	return newCall(c, rpc.QueuedList, "get queued prompts list", "queued prompts list").at(id, sessionID).get(ctx)
}

// GetAgentSessionQueuedPromptSummaries describes the prompts queued for a
// session.
func (c *Client) GetAgentSessionQueuedPromptSummaries(ctx context.Context, id, sessionID string) ([]message.QueuedPromptSummary, error) {
	return newCall(c, rpc.QueuedSummaries, "get queued prompts", "queued prompts").at(id, sessionID).
		with(url.Values{"details": {"true"}}).get(ctx)
}

// ClearAgentSessionQueuedPrompts clears the queued prompts for a session.
func (c *Client) ClearAgentSessionQueuedPrompts(ctx context.Context, id string, sessionID string) error {
	_, err := newCall(c, rpc.ClearQueue, "clear session agent queued prompts", "").at(id, sessionID).get(ctx)
	return err
}

// RecallAgentSessionQueuedPrompt withdraws the newest prompt still owned by
// Crush. The request carries a token the client keeps until the server's
// answer arrives, so a retry after a lost response recovers the same
// prompt instead of withdrawing another one.
func (c *Client) RecallAgentSessionQueuedPrompt(ctx context.Context, id, sessionID string) (*proto.AgentMessage, error) {
	key := id + "\x00" + sessionID
	token, _ := c.recallRequests.LoadOrStore(key, uuid.NewString())
	recall := newCall(c, rpc.RecallQueued, "recall queued prompt", "recalled prompt").at(id, sessionID)
	prompt, err := recall.with(url.Values{"request_id": {token.(string)}}).get(ctx)
	if err != nil {
		return nil, err
	}
	c.recallRequests.CompareAndDelete(key, token)
	// Delivery succeeded. A failed acknowledgement keeps the server's copy;
	// it must never turn a successfully recovered prompt into a UI error.
	go func() {
		ackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = recall.with(url.Values{"request_id": {token.(string)}, "acknowledge": {"true"}}).get(ackCtx)
	}()
	return prompt, nil
}

// RunShellCommand runs a shell command in the workspace without
// triggering the agent. isFirstMessage asks the server to title the
// session from the command.
func (c *Client) RunShellCommand(ctx context.Context, id, sessionID, command string, termWidth int, isFirstMessage bool) (proto.ShellCommandResponse, error) {
	return newCall(c, rpc.RunShellCommand, "run shell command", "shell command response").at(id, sessionID).
		do(ctx, proto.ShellCommandRequest{Command: command, TermWidth: termWidth, IsFirstMessage: isFirstMessage})
}

// GetDefaultSmallModel retrieves the default small model for a provider.
func (c *Client) GetDefaultSmallModel(ctx context.Context, id string, providerID string) (*config.SelectedModel, error) {
	model, err := newCall(c, rpc.DefaultSmallModel, "get default small model", "default small model").at(id).
		with(url.Values{"provider_id": []string{providerID}}).get(ctx)
	if err != nil {
		return nil, err
	}
	return &model, nil
}

// GrantPermission grants a permission on a workspace. The returned
// bool reports whether this call resolved the pending request (true)
// or found it already resolved by a previous caller (false). A false
// value is not an error — it just means another subscriber resolved
// the same request first.
func (c *Client) GrantPermission(ctx context.Context, id string, req proto.PermissionGrant) (bool, error) {
	resp, err := newCall(c, rpc.GrantPermission, "grant permission", "grant permission response").at(id).do(ctx, req)
	return resp.Resolved, err
}

// SetPermissionsSkipRequests sets the skip-requests flag for a workspace.
func (c *Client) SetPermissionsSkipRequests(ctx context.Context, id string, skip bool) error {
	_, err := newCall(c, rpc.SetPermissionsSkip, "set permissions skip requests", "").at(id).
		do(ctx, proto.PermissionSkipRequest{Skip: skip})
	return err
}

// GetPermissionsSkipRequests retrieves the skip-requests flag for a workspace.
func (c *Client) GetPermissionsSkipRequests(ctx context.Context, id string) (bool, error) {
	skip, err := newCall(c, rpc.GetPermissionsSkip, "get permissions skip requests", "permissions skip requests").
		at(id).get(ctx)
	return skip.Skip, err
}

// AnswerQuestionBatch submits answers for a batch question on a
// workspace. Returns true if this call resolved the pending
// request, false if already resolved by another caller.
func (c *Client) AnswerQuestionBatch(ctx context.Context, id string, req proto.QuestionAnswer) (bool, error) {
	resp, err := newCall(c, rpc.AnswerQuestion, "answer question batch", "answer question batch response").
		at(id).do(ctx, req)
	return resp.Resolved, err
}

// CancelQuestionBatch cancels the pending question batch on a
// workspace. Returns true if a question was cancelled, false if
// none was pending.
func (c *Client) CancelQuestionBatch(ctx context.Context, id string) (bool, error) {
	resp, err := newCall(c, rpc.CancelQuestion, "cancel question batch", "cancel question batch response").
		at(id).get(ctx)
	return resp.Resolved, err
}
