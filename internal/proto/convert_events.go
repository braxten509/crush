package proto

import (
	"errors"

	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/commands"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/skills"
)

// PermissionRequestFromDomain converts a permission request into its
// wire form.
func PermissionRequestFromDomain(p permission.PermissionRequest) PermissionRequest {
	return PermissionRequest{
		ID:          p.ID,
		SessionID:   p.SessionID,
		ToolCallID:  p.ToolCallID,
		ToolName:    p.ToolName,
		Description: p.Description,
		Action:      p.Action,
		Path:        p.Path,
		Params:      p.Params,
	}
}

// PermissionRequestToDomain converts a wire permission request back
// into the domain type.
func PermissionRequestToDomain(p PermissionRequest) permission.PermissionRequest {
	return permission.PermissionRequest{
		ID:          p.ID,
		SessionID:   p.SessionID,
		ToolCallID:  p.ToolCallID,
		ToolName:    p.ToolName,
		Description: p.Description,
		Action:      p.Action,
		Path:        p.Path,
		Params:      p.Params,
	}
}

// PermissionNotificationFromDomain converts a permission notification
// into its wire form.
func PermissionNotificationFromDomain(n permission.PermissionNotification) PermissionNotification {
	return PermissionNotification{
		SessionID:  n.SessionID,
		ToolCallID: n.ToolCallID,
		Granted:    n.Granted,
		Denied:     n.Denied,
	}
}

// PermissionNotificationToDomain converts a wire permission
// notification back into the domain type.
func PermissionNotificationToDomain(n PermissionNotification) permission.PermissionNotification {
	return permission.PermissionNotification{
		SessionID:  n.SessionID,
		ToolCallID: n.ToolCallID,
		Granted:    n.Granted,
		Denied:     n.Denied,
	}
}

// QuestionRequestFromDomain converts a question batch into its wire
// form.
func QuestionRequestFromDomain(r question.Request) QuestionRequest {
	return QuestionRequest{
		ID:                 r.ID,
		SessionID:          r.SessionID,
		ToolCallID:         r.ToolCallID,
		Questions:          QuestionsFromDomain(r.Questions),
		ConfirmTitle:       r.ConfirmTitle,
		ConfirmDescription: r.ConfirmDescription,
	}
}

// QuestionRequestToDomain converts a wire question batch back into the
// domain type.
func QuestionRequestToDomain(r QuestionRequest) question.Request {
	return question.Request{
		ID:                 r.ID,
		SessionID:          r.SessionID,
		ToolCallID:         r.ToolCallID,
		Questions:          QuestionsToDomain(r.Questions),
		ConfirmTitle:       r.ConfirmTitle,
		ConfirmDescription: r.ConfirmDescription,
	}
}

// QuestionsFromDomain converts questions into their wire form.
func QuestionsFromDomain(qs []question.Question) []QuestionItem {
	if len(qs) == 0 {
		return nil
	}
	out := make([]QuestionItem, len(qs))
	for i, q := range qs {
		choices := make([]QuestionChoice, len(q.Choices))
		for j, c := range q.Choices {
			choices[j] = QuestionChoice{
				ID:          c.ID,
				Label:       c.Label,
				Description: c.Description,
				Image:       c.Image,
			}
		}
		out[i] = QuestionItem{
			ID:          q.ID,
			Type:        string(q.Type),
			Label:       q.Label,
			Question:    q.Text,
			Description: q.Description,
			Choices:     choices,
		}
	}
	return out
}

// QuestionsToDomain converts wire questions back into the domain type.
func QuestionsToDomain(qs []QuestionItem) []question.Question {
	if len(qs) == 0 {
		return nil
	}
	out := make([]question.Question, len(qs))
	for i, q := range qs {
		choices := make([]question.Choice, len(q.Choices))
		for j, c := range q.Choices {
			choices[j] = question.Choice{
				ID:          c.ID,
				Label:       c.Label,
				Description: c.Description,
				Image:       c.Image,
			}
		}
		out[i] = question.Question{
			ID:          q.ID,
			Type:        question.Type(q.Type),
			Label:       q.Label,
			Text:        q.Question,
			Description: q.Description,
			Choices:     choices,
		}
	}
	return out
}

// QuestionAnswerFromDomain converts answers into the wire request body.
func QuestionAnswerFromDomain(responses []question.Answer) QuestionAnswer {
	out := QuestionAnswer{Responses: make([]QuestionResponse, len(responses))}
	for i, r := range responses {
		out.Responses[i] = QuestionResponse{
			QuestionID:  r.QuestionID,
			SelectedIDs: r.SelectedIDs,
			FillInText:  r.FillInText,
			Yes:         r.Yes,
			Notes:       r.Notes,
		}
	}
	return out
}

// QuestionAnswerToDomain converts the wire request body back into
// answers.
func QuestionAnswerToDomain(req QuestionAnswer) []question.Answer {
	out := make([]question.Answer, len(req.Responses))
	for i, r := range req.Responses {
		out[i] = question.Answer{
			QuestionID:  r.QuestionID,
			SelectedIDs: r.SelectedIDs,
			FillInText:  r.FillInText,
			Yes:         r.Yes,
			Notes:       r.Notes,
		}
	}
	return out
}

// MCPEventTypeFromDomain maps an MCP event type to its wire form. It
// returns "" for types the wire does not carry, so callers can drop
// the event rather than coercing it to state_changed.
func MCPEventTypeFromDomain(t mcp.EventType) MCPEventType {
	switch t {
	case mcp.EventStateChanged:
		return MCPEventStateChanged
	case mcp.EventToolsListChanged:
		return MCPEventToolsListChanged
	case mcp.EventPromptsListChanged:
		return MCPEventPromptsListChanged
	case mcp.EventResourcesListChanged:
		return MCPEventResourcesListChanged
	case mcp.EventChannelMessage:
		return MCPEventChannelMessage
	default:
		return ""
	}
}

// MCPEventTypeToDomain maps a wire MCP event type back to the domain
// type, treating unknown values as state changes.
func MCPEventTypeToDomain(t MCPEventType) mcp.EventType {
	switch t {
	case MCPEventStateChanged:
		return mcp.EventStateChanged
	case MCPEventToolsListChanged:
		return mcp.EventToolsListChanged
	case MCPEventPromptsListChanged:
		return mcp.EventPromptsListChanged
	case MCPEventResourcesListChanged:
		return mcp.EventResourcesListChanged
	case MCPEventChannelMessage:
		return mcp.EventChannelMessage
	default:
		return mcp.EventStateChanged
	}
}

// MCPEventFromDomain converts an MCP event into its wire form. The
// second result is false when the event type has no wire form.
func MCPEventFromDomain(e mcp.Event) (MCPEvent, bool) {
	t := MCPEventTypeFromDomain(e.Type)
	if t == "" {
		return MCPEvent{}, false
	}
	return MCPEvent{
		Type:           t,
		Name:           e.Name,
		State:          MCPState(e.State),
		Error:          e.Error,
		ToolCount:      e.Counts.Tools,
		PromptCount:    e.Counts.Prompts,
		ResourceCount:  e.Counts.Resources,
		ChannelMessage: e.ChannelMessage,
	}, true
}

// MCPEventToDomain converts a wire MCP event back into the domain type.
func MCPEventToDomain(e MCPEvent) mcp.Event {
	return mcp.Event{
		Type:  MCPEventTypeToDomain(e.Type),
		Name:  e.Name,
		State: mcp.State(e.State),
		Error: e.Error,
		Counts: mcp.Counts{
			Tools:     e.ToolCount,
			Prompts:   e.PromptCount,
			Resources: e.ResourceCount,
		},
		ChannelMessage: e.ChannelMessage,
	}
}

// MCPClientInfoFromDomain converts an MCP client's state into its wire
// form.
func MCPClientInfoFromDomain(v mcp.ClientInfo) MCPClientInfo {
	return MCPClientInfo{
		Name:          v.Name,
		State:         MCPState(v.State),
		Error:         v.Error,
		ToolCount:     v.Counts.Tools,
		PromptCount:   v.Counts.Prompts,
		ResourceCount: v.Counts.Resources,
		ConnectedAt:   v.ConnectedAt,
		Channel:       v.Channel,
	}
}

// MCPClientInfoToDomain converts a wire MCP client state back into the
// domain type.
func MCPClientInfoToDomain(v MCPClientInfo) mcp.ClientInfo {
	return mcp.ClientInfo{
		Name:  v.Name,
		State: mcp.State(v.State),
		Error: v.Error,
		Counts: mcp.Counts{
			Tools:     v.ToolCount,
			Prompts:   v.PromptCount,
			Resources: v.ResourceCount,
		},
		ConnectedAt: v.ConnectedAt,
		Channel:     v.Channel,
	}
}

// MCPClientInfosFromDomain converts a map of MCP client states.
func MCPClientInfosFromDomain(states map[string]mcp.ClientInfo) map[string]MCPClientInfo {
	out := make(map[string]MCPClientInfo, len(states))
	for k, v := range states {
		out[k] = MCPClientInfoFromDomain(v)
	}
	return out
}

// MCPClientInfosToDomain converts a map of wire MCP client states.
func MCPClientInfosToDomain(states map[string]MCPClientInfo) map[string]mcp.ClientInfo {
	out := make(map[string]mcp.ClientInfo, len(states))
	for k, v := range states {
		out[k] = MCPClientInfoToDomain(v)
	}
	return out
}

// MCPPendingAuthFromDomain converts the servers awaiting OAuth into
// their wire form.
func MCPPendingAuthFromDomain(pending []mcp.PendingAuthServer) []MCPPendingAuthServer {
	out := make([]MCPPendingAuthServer, len(pending))
	for i, p := range pending {
		out[i] = MCPPendingAuthServer{Name: p.Name, URL: p.URL}
	}
	return out
}

// MCPPendingAuthToDomain converts wire pending-auth entries back into
// the domain type.
func MCPPendingAuthToDomain(pending []MCPPendingAuthServer) []mcp.PendingAuthServer {
	out := make([]mcp.PendingAuthServer, len(pending))
	for i, p := range pending {
		out[i] = mcp.PendingAuthServer{Name: p.Name, URL: p.URL}
	}
	return out
}

// MCPPromptsFromDomain converts MCP prompts into their wire form.
func MCPPromptsFromDomain(prompts []commands.MCPPrompt) []MCPPrompt {
	out := make([]MCPPrompt, len(prompts))
	for i, prompt := range prompts {
		arguments := make([]MCPPromptArgument, len(prompt.Arguments))
		for j, argument := range prompt.Arguments {
			arguments[j] = MCPPromptArgument{
				ID:          argument.ID,
				Title:       argument.Title,
				Description: argument.Description,
				Required:    argument.Required,
			}
		}
		out[i] = MCPPrompt{
			ID:          prompt.ID,
			Title:       prompt.Title,
			Description: prompt.Description,
			PromptID:    prompt.PromptID,
			ClientID:    prompt.ClientID,
			Arguments:   arguments,
		}
	}
	return out
}

// MCPPromptsToDomain converts wire MCP prompts back into the domain
// type.
func MCPPromptsToDomain(prompts []MCPPrompt) []commands.MCPPrompt {
	out := make([]commands.MCPPrompt, len(prompts))
	for i, prompt := range prompts {
		arguments := make([]commands.Argument, len(prompt.Arguments))
		for j, argument := range prompt.Arguments {
			arguments[j] = commands.Argument{
				ID:          argument.ID,
				Title:       argument.Title,
				Description: argument.Description,
				Required:    argument.Required,
			}
		}
		out[i] = commands.MCPPrompt{
			ID:          prompt.ID,
			Title:       prompt.Title,
			Description: prompt.Description,
			PromptID:    prompt.PromptID,
			ClientID:    prompt.ClientID,
			Arguments:   arguments,
		}
	}
	return out
}

// AgentEventFromDomain converts an agent notification into its wire
// form. The human-readable message travels in Error, which the client
// maps back into Notification.Message.
func AgentEventFromDomain(n notify.Notification) AgentEvent {
	out := AgentEvent{
		SessionID:    n.SessionID,
		SessionTitle: n.SessionTitle,
		RunID:        n.RunID,
		Type:         AgentEventType(n.Type),
		AWSSOCommand: n.AWSSOCommand,
		AWSSOURL:     n.AWSSOURL,
	}
	if n.Message != "" {
		out.Error = errors.New(n.Message)
	}
	if n.Type == notify.TypeAgentError {
		out.Type = AgentEventTypeError
	}
	return out
}

// AgentEventToDomain converts a wire agent event back into an agent
// notification.
func AgentEventToDomain(e AgentEvent) notify.Notification {
	n := notify.Notification{
		SessionID:    e.SessionID,
		SessionTitle: e.SessionTitle,
		RunID:        e.RunID,
		Type:         notify.Type(e.Type),
		AWSSOCommand: e.AWSSOCommand,
		AWSSOURL:     e.AWSSOURL,
	}
	if e.Error != nil {
		n.Message = e.Error.Error()
	}
	return n
}

// RunCompleteFromDomain converts a run-complete notification into its
// wire form.
func RunCompleteFromDomain(r notify.RunComplete) RunComplete {
	return RunComplete{
		SubmissionID: r.SubmissionID,
		SessionID:    r.SessionID,
		RunID:        r.RunID,
		MessageID:    r.MessageID,
		Text:         r.Text,
		Error:        r.Error,
		Cancelled:    r.Cancelled,
	}
}

// RunCompleteToDomain converts a wire run-complete event back into the
// domain type.
func RunCompleteToDomain(r RunComplete) notify.RunComplete {
	return notify.RunComplete{
		SubmissionID: r.SubmissionID,
		SessionID:    r.SessionID,
		RunID:        r.RunID,
		MessageID:    r.MessageID,
		Text:         r.Text,
		Error:        r.Error,
		Cancelled:    r.Cancelled,
	}
}

// SkillStatesFromDomain converts skill discovery states into their wire
// form. Errors are flattened to strings because error does not
// round-trip over JSON. It returns nil for an empty list.
func SkillStatesFromDomain(states []*skills.SkillState) []SkillState {
	if len(states) == 0 {
		return nil
	}
	out := make([]SkillState, len(states))
	for i, s := range states {
		entry := SkillState{
			Name:  s.Name,
			Path:  s.Path,
			State: SkillDiscoveryState(s.State),
		}
		if s.Err != nil {
			entry.Error = s.Err.Error()
		}
		out[i] = entry
	}
	return out
}

// SkillStatesToDomain reconstructs skill discovery states from their
// wire form. Non-empty Error strings become synthetic error values; the
// TUI never type-asserts on Err.
func SkillStatesToDomain(in []SkillState) []*skills.SkillState {
	if len(in) == 0 {
		return nil
	}
	out := make([]*skills.SkillState, len(in))
	for i, s := range in {
		state := &skills.SkillState{
			Name:  s.Name,
			Path:  s.Path,
			State: skills.DiscoveryState(s.State),
		}
		if s.Error != "" {
			state.Err = errors.New(s.Error)
		}
		out[i] = state
	}
	return out
}

// SkillsEventFromDomain converts a skills event into its wire form.
func SkillsEventFromDomain(e skills.Event) SkillsEvent {
	return SkillsEvent{States: SkillStatesFromDomain(e.States)}
}

// SkillInfosFromDomain converts catalog entries into their wire form.
func SkillInfosFromDomain(entries []skills.CatalogEntry) []SkillInfo {
	out := make([]SkillInfo, len(entries))
	for i, entry := range entries {
		out[i] = SkillInfo{
			ID:            entry.ID,
			Name:          entry.Name,
			Description:   entry.Description,
			Label:         entry.Label,
			Source:        string(entry.Source),
			UserInvocable: entry.UserInvocable,
		}
	}
	return out
}

// SkillInfosToDomain converts wire skill infos back into catalog
// entries.
func SkillInfosToDomain(entries []SkillInfo) []skills.CatalogEntry {
	out := make([]skills.CatalogEntry, len(entries))
	for i, entry := range entries {
		out[i] = skills.CatalogEntry{
			ID:            entry.ID,
			Name:          entry.Name,
			Description:   entry.Description,
			Label:         entry.Label,
			Source:        skills.SourceType(entry.Source),
			UserInvocable: entry.UserInvocable,
		}
	}
	return out
}

// SkillReadResultFromDomain converts skill read metadata into its wire
// form.
func SkillReadResultFromDomain(r skills.SkillReadResult) SkillReadResult {
	return SkillReadResult{
		Name:        r.Name,
		Description: r.Description,
		Source:      string(r.Source),
		Builtin:     r.Builtin,
	}
}

// SkillReadResultToDomain converts wire skill read metadata back into
// the domain type.
func SkillReadResultToDomain(r SkillReadResult) skills.SkillReadResult {
	return skills.SkillReadResult{
		Name:        r.Name,
		Description: r.Description,
		Source:      skills.SourceType(r.Source),
		Builtin:     r.Builtin,
	}
}
