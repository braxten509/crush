package agent

import (
	"errors"

	"charm.land/fantasy"
)

// emptyMediaNote replaces a tool's media when it carries no bytes, since
// providers reject an empty media block by failing the whole request.
const emptyMediaNote = "The tool returned empty media data, so there is nothing to show here."

const emptyToolOutputNote = "(no output)"

// emptyMediaText is the text a tool result carries once its empty media is
// dropped, keeping whatever the tool said alongside it.
func emptyMediaText(text string) string {
	if text == "" {
		return emptyMediaNote
	}
	return text + "\n" + emptyMediaNote
}

// dropEmptyToolMedia replaces every tool result whose media has no data with
// a text note, so no tool can put an empty media block on the wire.
func dropEmptyToolMedia(messages []fantasy.Message) []fantasy.Message {
	for i, msg := range messages {
		if msg.Role != fantasy.MessageRoleTool {
			continue
		}
		var content []fantasy.MessagePart
		for j, part := range msg.Content {
			result, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](part)
			if !ok {
				continue
			}
			media, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentMedia](result.Output)
			if !ok || media.Data != "" {
				continue
			}
			if content == nil {
				content = append([]fantasy.MessagePart(nil), msg.Content...)
			}
			result.Output = fantasy.ToolResultOutputContentText{Text: emptyMediaText(media.Text)}
			content[j] = result
		}
		if content != nil {
			messages[i].Content = content
		}
	}
	return messages
}

func sanitizeStepMessages(messages []fantasy.Message) []fantasy.Message {
	messages = dropEmptyToolMedia(append([]fantasy.Message(nil), messages...))
	out := make([]fantasy.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == fantasy.MessageRoleTool {
			var content []fantasy.MessagePart
			for j, part := range msg.Content {
				result, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](part)
				if !ok {
					continue
				}
				var replaced fantasy.ToolResultOutputContent
				switch result.Output.GetType() {
				case fantasy.ToolResultContentTypeText:
					if textOut, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](result.Output); ok && textOut.Text == "" {
						replaced = fantasy.ToolResultOutputContentText{Text: emptyToolOutputNote}
					}
				case fantasy.ToolResultContentTypeError:
					if errOut, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentError](result.Output); ok && (errOut.Error == nil || errOut.Error.Error() == "") {
						replaced = fantasy.ToolResultOutputContentError{Error: errors.New(failedToolCallResult)}
					}
				}
				if replaced == nil {
					continue
				}
				if content == nil {
					content = append([]fantasy.MessagePart(nil), msg.Content...)
				}
				result.Output = replaced
				content[j] = result
			}
			if content != nil {
				msg.Content = content
			}
			out = append(out, msg)
			continue
		}

		hasEmptyText := false
		for _, part := range msg.Content {
			if tp, ok := fantasy.AsMessagePart[fantasy.TextPart](part); ok && tp.Text == "" {
				hasEmptyText = true
				break
			}
		}
		if hasEmptyText {
			filtered := make([]fantasy.MessagePart, 0, len(msg.Content))
			for _, part := range msg.Content {
				if tp, ok := fantasy.AsMessagePart[fantasy.TextPart](part); ok && tp.Text == "" {
					continue
				}
				filtered = append(filtered, part)
			}
			msg.Content = filtered
		}
		if len(msg.Content) == 0 {
			continue
		}
		out = append(out, msg)
	}
	return out
}
