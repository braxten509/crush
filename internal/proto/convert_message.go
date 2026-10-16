package proto

import "github.com/charmbracelet/crush/internal/message"

// MessageFromDomain converts a domain message into its wire form. Parts
// of an unknown type are dropped.
func MessageFromDomain(m message.Message) Message {
	msg := Message{
		ActivityAt:              m.ActivityAt,
		Activity:                m.Activity,
		ActivityDetail:          m.ActivityDetail,
		ID:                      m.ID,
		SessionID:               m.SessionID,
		Role:                    MessageRole(m.Role),
		Model:                   m.Model,
		Provider:                m.Provider,
		PrismModelID:            m.PrismModelID,
		PrismModelName:          m.PrismModelName,
		PrismHypercreditSavings: m.PrismHypercreditSavings,
		PrismDollarSavings:      m.PrismDollarSavings,
		CreatedAt:               m.CreatedAt,
		UpdatedAt:               m.UpdatedAt,
		IsSummaryMessage:        m.IsSummaryMessage,
		IsCompacting:            m.IsCompacting,
	}
	for _, p := range m.Parts {
		if part := partFromDomain(p); part != nil {
			msg.Parts = append(msg.Parts, part)
		}
	}
	return msg
}

func partFromDomain(p message.ContentPart) ContentPart {
	switch v := p.(type) {
	case message.TextContent:
		return TextContent{Text: v.Text, Hidden: v.Hidden, SubmissionID: v.SubmissionID}
	case message.ReasoningContent:
		return ReasoningContent{
			Thinking:   v.Thinking,
			Signature:  v.Signature,
			StartedAt:  v.StartedAt,
			FinishedAt: v.FinishedAt,
		}
	case message.ToolCall:
		return ToolCall{
			ID:       v.ID,
			Name:     v.Name,
			Input:    v.Input,
			Finished: v.Finished,
		}
	case message.ToolResult:
		return ToolResult{
			Review:     v.Review,
			ToolCallID: v.ToolCallID,
			Name:       v.Name,
			Content:    v.Content,
			Data:       v.Data,
			MIMEType:   v.MIMEType,
			Metadata:   v.Metadata,
			IsError:    v.IsError,
		}
	case message.Finish:
		return Finish{
			Reason:  FinishReason(v.Reason),
			Time:    v.Time,
			Message: v.Message,
			Details: v.Details,
		}
	case message.ImageURLContent:
		return ImageURLContent{URL: v.URL, Detail: v.Detail}
	case message.BinaryContent:
		return BinaryContent{Path: v.Path, MIMEType: v.MIMEType, Data: v.Data}
	case message.ShellCommand:
		return ShellCommand{
			Command:  v.Command,
			Output:   v.Output,
			ExitCode: v.ExitCode,
		}
	}
	return nil
}

// MessageToDomain converts a wire message back into a domain message.
// Parts of an unknown type are dropped.
func MessageToDomain(m Message) message.Message {
	msg := message.Message{
		ActivityAt:              m.ActivityAt,
		Activity:                m.Activity,
		ActivityDetail:          m.ActivityDetail,
		ID:                      m.ID,
		SessionID:               m.SessionID,
		Role:                    message.MessageRole(m.Role),
		Model:                   m.Model,
		Provider:                m.Provider,
		PrismModelID:            m.PrismModelID,
		PrismModelName:          m.PrismModelName,
		PrismHypercreditSavings: m.PrismHypercreditSavings,
		PrismDollarSavings:      m.PrismDollarSavings,
		CreatedAt:               m.CreatedAt,
		UpdatedAt:               m.UpdatedAt,
		IsSummaryMessage:        m.IsSummaryMessage,
		IsCompacting:            m.IsCompacting,
	}
	for _, p := range m.Parts {
		if part := partToDomain(p); part != nil {
			msg.Parts = append(msg.Parts, part)
		}
	}
	return msg
}

func partToDomain(p ContentPart) message.ContentPart {
	switch v := p.(type) {
	case TextContent:
		return message.TextContent{Text: v.Text, Hidden: v.Hidden, SubmissionID: v.SubmissionID}
	case ReasoningContent:
		return message.ReasoningContent{
			Thinking:   v.Thinking,
			Signature:  v.Signature,
			StartedAt:  v.StartedAt,
			FinishedAt: v.FinishedAt,
		}
	case ToolCall:
		return message.ToolCall{
			ID:       v.ID,
			Name:     v.Name,
			Input:    v.Input,
			Finished: v.Finished,
		}
	case ToolResult:
		return message.ToolResult{
			Review:     v.Review,
			ToolCallID: v.ToolCallID,
			Name:       v.Name,
			Content:    v.Content,
			Data:       v.Data,
			MIMEType:   v.MIMEType,
			Metadata:   v.Metadata,
			IsError:    v.IsError,
		}
	case Finish:
		return message.Finish{
			Reason:  message.FinishReason(v.Reason),
			Time:    v.Time,
			Message: v.Message,
			Details: v.Details,
		}
	case ImageURLContent:
		return message.ImageURLContent{URL: v.URL, Detail: v.Detail}
	case BinaryContent:
		return message.BinaryContent{Path: v.Path, MIMEType: v.MIMEType, Data: v.Data}
	case ShellCommand:
		return message.ShellCommand{
			Command:  v.Command,
			Output:   v.Output,
			ExitCode: v.ExitCode,
		}
	}
	return nil
}

// MessagesFromDomain converts a slice of domain messages.
func MessagesFromDomain(msgs []message.Message) []Message {
	out := make([]Message, len(msgs))
	for i, m := range msgs {
		out[i] = MessageFromDomain(m)
	}
	return out
}

// MessagesToDomain converts a slice of wire messages.
func MessagesToDomain(msgs []Message) []message.Message {
	out := make([]message.Message, len(msgs))
	for i, m := range msgs {
		out[i] = MessageToDomain(m)
	}
	return out
}
