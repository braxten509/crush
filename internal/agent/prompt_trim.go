package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
)

const (
	trimmedToolEdgeBytes = 700
	trimmedToolMaxBytes  = 1600
	transcriptSubdir     = "transcripts"
)

type transcriptIndex struct {
	toolCalls   map[string][2]int
	toolResults map[string][2]int
	totalLines  int
}

func (a *sessionAgent) transcriptDataDir() string {
	if a.cfg != nil && a.cfg.Config() != nil && a.cfg.Config().Options != nil && a.cfg.Config().Options.DataDirectory != "" {
		return a.cfg.Config().Options.DataDirectory
	}
	return filepath.Join(os.TempDir(), "crush-transcripts")
}

func (a *sessionAgent) historyTokenBudget(model Model, overheadTokens int64) int64 {
	cw := int64(model.CatwalkCfg.ContextWindow)
	maxOut := int64(model.CatwalkCfg.DefaultMaxTokens)
	configured := config.DefaultAutoCompactTokenLimit
	if a.cfg != nil && a.cfg.Config() != nil && a.cfg.Config().Options != nil {
		configured = a.cfg.Config().Options.GetAutoCompactTokenLimit()
	}
	return promptHistoryBudget(cw, maxOut, configured, overheadTokens)
}

func promptHistoryBudget(cw, maxOut, configured, overheadTokens int64) int64 {
	limit := config.FallbackCompactionLimit(cw, configured)
	budget := limit * 75 / 100
	if cw > 0 {
		reservedOut := min(max(maxOut, 0), cw/4)
		windowSafe := (cw - reservedOut) * 72 / 100
		if budget <= 0 || windowSafe < budget {
			budget = windowSafe
		}
	}
	if budget <= 0 {
		return 0
	}
	floor := min(budget/2, 8_000)
	return max(budget-max(overheadTokens, 0), floor)
}

func (a *sessionAgent) trimPreparedPrompt(sessionID string, msgs []message.Message, history []fantasy.Message, model Model, overheadTokens int64) []fantasy.Message {
	budget := a.historyTokenBudget(model, overheadTokens)
	if budget <= 0 || estimateMessageTokens(history) <= budget {
		return history
	}

	transcriptPath, idx, err := writeSearchableTranscript(a.transcriptDataDir(), sessionID, msgs)
	if err != nil {
		slog.Warn("Could not write searchable chat transcript", "session_id", sessionID, "error", err)
	}

	trimmed, shortenedCount := shortenOlderToolParts(history, budget, transcriptPath, idx)
	omittedMessages := 0
	if estimateMessageTokens(trimmed) > budget {
		trimmed, omittedMessages = dropOldestMessagesToFit(trimmed, budget, transcriptPath, idx)
	}

	if (shortenedCount > 0 || omittedMessages > 0) && transcriptPath != "" {
		notice := buildTranscriptSearchNotice(transcriptPath, shortenedCount, omittedMessages, idx.totalLines)
		trimmed = insertTranscriptNotice(trimmed, notice)
	}
	return trimmed
}

func writeSearchableTranscript(dataDir, sessionID string, msgs []message.Message) (string, transcriptIndex, error) {
	idx := transcriptIndex{
		toolCalls:   make(map[string][2]int),
		toolResults: make(map[string][2]int),
	}
	if sessionID == "" {
		sessionID = "session"
	}
	dir := filepath.Join(dataDir, transcriptSubdir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", idx, err
	}
	safeID := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, sessionID)
	path := filepath.Join(dir, safeID+".md")

	var b strings.Builder
	line := 1
	writeBlock := func(s string) (int, int) {
		start := line
		if !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		b.WriteString(s)
		line += strings.Count(s, "\n")
		return start, line - 1
	}

	writeBlock(fmt.Sprintf("# Full Conversation Transcript (%s)\n", sessionID))
	for i, msg := range msgs {
		switch msg.Role {
		case message.User:
			text := strings.TrimSpace(msg.Content().Text)
			if text == "" {
				continue
			}
			writeBlock(fmt.Sprintf("\n## Message %d [user]\n%s\n", i+1, text))
		case message.Assistant:
			who := "assistant"
			if msg.Model != "" {
				who = fmt.Sprintf("assistant: %s/%s", msg.Provider, msg.Model)
			}
			if msg.IsSummaryMessage {
				who = "summary"
			}
			text := strings.TrimSpace(msg.Content().Text)
			calls := msg.ToolCalls()
			if text == "" && len(calls) == 0 {
				continue
			}
			writeBlock(fmt.Sprintf("\n## Message %d [%s]\n", i+1, who))
			if text != "" {
				writeBlock(text + "\n")
			}
			for _, tc := range calls {
				start, end := writeBlock(fmt.Sprintf("\n### Tool Call: %s (id: %s)\n%s\n", tc.Name, tc.ID, tc.Input))
				if tc.ID != "" {
					idx.toolCalls[tc.ID] = [2]int{start, end}
				}
			}
		case message.Tool:
			for _, tr := range msg.ToolResults() {
				status := "ok"
				if tr.IsError {
					status = "error"
				}
				start, end := writeBlock(fmt.Sprintf("\n### Tool Result: %s (id: %s, status: %s)\n%s\n", tr.Name, tr.ToolCallID, status, tr.Content))
				if tr.ToolCallID != "" {
					idx.toolResults[tr.ToolCallID] = [2]int{start, end}
				}
			}
		}
	}
	idx.totalLines = max(line-1, 1)
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return "", idx, err
	}
	return path, idx, nil
}

func shortenOlderToolParts(history []fantasy.Message, budget int64, transcriptPath string, idx transcriptIndex) ([]fantasy.Message, int) {
	cloned := cloneFantasyMessages(history)
	recentBudget := max(budget/3, 4_000)
	protectStart := len(cloned)
	var tailTokens int64
	for i := len(cloned) - 1; i >= 0; i-- {
		msgTokens := estimateMessageTokens(cloned[i : i+1])
		if tailTokens+msgTokens > recentBudget {
			break
		}
		tailTokens += msgTokens
		protectStart = i
		if i > 0 && cloned[i].Role == fantasy.MessageRoleTool && cloned[i-1].Role == fantasy.MessageRoleAssistant {
			prevTokens := estimateMessageTokens(cloned[i-1 : i])
			if tailTokens+prevTokens <= recentBudget*3/2 {
				tailTokens += prevTokens
				protectStart = i - 1
				i--
			}
		}
	}

	shortened := 0
	shortenRange := func(end int, maxBytes, edgeBytes int) {
		for i := 0; i < end; i++ {
			for j, part := range cloned[i].Content {
				switch p := part.(type) {
				case fantasy.ToolResultPart:
					if updated, ok := shortenToolResultPart(p, maxBytes, edgeBytes, transcriptPath, idx.toolResults[p.ToolCallID]); ok {
						cloned[i].Content[j] = updated
						shortened++
					}
				case *fantasy.ToolResultPart:
					if updated, ok := shortenToolResultPart(*p, maxBytes, edgeBytes, transcriptPath, idx.toolResults[p.ToolCallID]); ok {
						cloned[i].Content[j] = updated
						shortened++
					}
				case fantasy.ToolCallPart:
					if updated, ok := shortenToolCallPart(p, maxBytes, edgeBytes, transcriptPath, idx.toolCalls[p.ToolCallID]); ok {
						cloned[i].Content[j] = updated
						shortened++
					}
				case *fantasy.ToolCallPart:
					if updated, ok := shortenToolCallPart(*p, maxBytes, edgeBytes, transcriptPath, idx.toolCalls[p.ToolCallID]); ok {
						cloned[i].Content[j] = updated
						shortened++
					}
				}
			}
		}
	}

	shortenRange(protectStart, trimmedToolMaxBytes, trimmedToolEdgeBytes)
	if estimateMessageTokens(cloned) > budget && protectStart < len(cloned) {
		shortenRange(len(cloned), trimmedToolMaxBytes, trimmedToolEdgeBytes)
	}
	return cloned, shortened
}

func shortenToolResultPart(part fantasy.ToolResultPart, maxBytes, edgeBytes int, transcriptPath string, lines [2]int) (fantasy.ToolResultPart, bool) {
	switch out := part.Output.(type) {
	case fantasy.ToolResultOutputContentText:
		if len(out.Text) <= maxBytes {
			return part, false
		}
		part.Output = fantasy.ToolResultOutputContentText{
			Text: clipWithTranscriptPointer(out.Text, edgeBytes, transcriptPath, lines),
		}
		return part, true
	case *fantasy.ToolResultOutputContentText:
		if len(out.Text) <= maxBytes {
			return part, false
		}
		part.Output = fantasy.ToolResultOutputContentText{
			Text: clipWithTranscriptPointer(out.Text, edgeBytes, transcriptPath, lines),
		}
		return part, true
	case fantasy.ToolResultOutputContentError:
		if out.Error == nil || len(out.Error.Error()) <= maxBytes {
			return part, false
		}
		part.Output = fantasy.ToolResultOutputContentError{
			Error: errors.New(clipWithTranscriptPointer(out.Error.Error(), edgeBytes, transcriptPath, lines)),
		}
		return part, true
	case *fantasy.ToolResultOutputContentError:
		if out.Error == nil || len(out.Error.Error()) <= maxBytes {
			return part, false
		}
		part.Output = fantasy.ToolResultOutputContentError{
			Error: errors.New(clipWithTranscriptPointer(out.Error.Error(), edgeBytes, transcriptPath, lines)),
		}
		return part, true
	case fantasy.ToolResultOutputContentMedia:
		if len(out.Data) <= maxBytes {
			return part, false
		}
		part.Output = fantasy.ToolResultOutputContentText{
			Text: fmt.Sprintf("[Earlier media tool output (%s) omitted to save context]", out.MediaType),
		}
		return part, true
	case *fantasy.ToolResultOutputContentMedia:
		if len(out.Data) <= maxBytes {
			return part, false
		}
		part.Output = fantasy.ToolResultOutputContentText{
			Text: fmt.Sprintf("[Earlier media tool output (%s) omitted to save context]", out.MediaType),
		}
		return part, true
	default:
		return part, false
	}
}

func shortenToolCallPart(part fantasy.ToolCallPart, maxBytes, edgeBytes int, transcriptPath string, lines [2]int) (fantasy.ToolCallPart, bool) {
	if len(part.Input) <= maxBytes {
		return part, false
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(part.Input), &obj); err != nil {
		return part, false
	}
	changed := shortenJSONValues(obj, maxBytes, edgeBytes, transcriptPath, lines)
	if !changed {
		return part, false
	}
	encoded, err := json.Marshal(obj)
	if err != nil {
		return part, false
	}
	part.Input = string(encoded)
	return part, true
}

func shortenJSONValues(v any, maxBytes, edgeBytes int, transcriptPath string, lines [2]int) bool {
	changed := false
	switch node := v.(type) {
	case map[string]any:
		for k, val := range node {
			switch s := val.(type) {
			case string:
				if len(s) > maxBytes {
					node[k] = clipWithTranscriptPointer(s, edgeBytes, transcriptPath, lines)
					changed = true
				}
			default:
				if shortenJSONValues(val, maxBytes, edgeBytes, transcriptPath, lines) {
					changed = true
				}
			}
		}
	case []any:
		for i, item := range node {
			if s, ok := item.(string); ok && len(s) > maxBytes {
				node[i] = clipWithTranscriptPointer(s, edgeBytes, transcriptPath, lines)
				changed = true
			} else if shortenJSONValues(item, maxBytes, edgeBytes, transcriptPath, lines) {
				changed = true
			}
		}
	}
	return changed
}

func clipWithTranscriptPointer(text string, edgeBytes int, transcriptPath string, lines [2]int) string {
	if len(text) <= edgeBytes*2 {
		return text
	}
	head := strings.ToValidUTF8(text[:edgeBytes], "")
	tail := strings.ToValidUTF8(text[len(text)-edgeBytes:], "")
	omittedLines := max(strings.Count(text, "\n")-strings.Count(head, "\n")-strings.Count(tail, "\n"), 1)

	location := ""
	if transcriptPath != "" {
		if lines[0] > 0 && lines[1] >= lines[0] {
			location = fmt.Sprintf(" — full text in %s (lines %d-%d); search with grep or read with view", transcriptPath, lines[0], lines[1])
		} else {
			location = fmt.Sprintf(" — full text in %s; search with grep or read with view", transcriptPath)
		}
	}
	return fmt.Sprintf("%s\n... [%d lines shortened%s] ...\n%s", head, omittedLines, location, tail)
}

func dropOldestMessagesToFit(history []fantasy.Message, budget int64, transcriptPath string, idx transcriptIndex) ([]fantasy.Message, int) {
	if len(history) == 0 {
		return history, 0
	}
	hasLeadingReminder := isSystemReminderMessage(history[0])
	startIdx := 0
	var reminderTokens int64
	if hasLeadingReminder {
		startIdx = 1
		reminderTokens = estimateMessageTokens(history[:1])
	}
	noticeReserve := int64(250)
	available := max(budget-reminderTokens-noticeReserve, 1_000)

	body := history[startIdx:]
	if len(body) == 0 {
		return history, 0
	}

	// Walk backwards from the end of body, keeping whole turns (and never
	// starting on a standalone Tool message without its Assistant message).
	cut := len(body)
	var keptTokens int64
	for i := len(body) - 1; i >= 0; i-- {
		msgTokens := estimateMessageTokens(body[i : i+1])
		if body[i].Role == fantasy.MessageRoleTool && i > 0 && body[i-1].Role == fantasy.MessageRoleAssistant {
			pairTokens := msgTokens + estimateMessageTokens(body[i-1:i])
			if keptTokens+pairTokens > available && cut < len(body) {
				break
			}
			keptTokens += pairTokens
			cut = i - 1
			i--
			continue
		}
		if keptTokens+msgTokens > available && cut < len(body) {
			break
		}
		keptTokens += msgTokens
		cut = i
	}

	for cut < len(body) && body[cut].Role == fantasy.MessageRoleTool {
		cut++
	}
	if cut == len(body) {
		// Always keep at least the final non-tool message (or final assistant+tool pair)
		// and clip its text parts if a single message alone exceeded the budget.
		if len(body) >= 2 && body[len(body)-1].Role == fantasy.MessageRoleTool && body[len(body)-2].Role == fantasy.MessageRoleAssistant {
			cut = len(body) - 2
		} else {
			cut = len(body) - 1
		}
	}

	kept := body[cut:]
	if estimateMessageTokens(kept) > available {
		kept = clipOversizedKeptMessages(kept, available, transcriptPath, idx)
	}

	result := make([]fantasy.Message, 0, startIdx+len(kept))
	if hasLeadingReminder {
		result = append(result, history[0])
	}
	result = append(result, kept...)
	return result, cut
}

func clipOversizedKeptMessages(msgs []fantasy.Message, available int64, transcriptPath string, idx transcriptIndex) []fantasy.Message {
	cloned := cloneFantasyMessages(msgs)
	maxBytes := int(max(available*2, 1_200))
	edgeBytes := max(maxBytes/3, 300)
	for i := range cloned {
		for j, part := range cloned[i].Content {
			switch p := part.(type) {
			case fantasy.TextPart:
				if len(p.Text) > maxBytes {
					cloned[i].Content[j] = fantasy.TextPart{
						Text: clipWithTranscriptPointer(p.Text, edgeBytes, transcriptPath, [2]int{}),
					}
				}
			case *fantasy.TextPart:
				if len(p.Text) > maxBytes {
					cloned[i].Content[j] = fantasy.TextPart{
						Text: clipWithTranscriptPointer(p.Text, edgeBytes, transcriptPath, [2]int{}),
					}
				}
			case fantasy.ToolResultPart:
				if updated, ok := shortenToolResultPart(p, maxBytes, edgeBytes, transcriptPath, idx.toolResults[p.ToolCallID]); ok {
					cloned[i].Content[j] = updated
				}
			case *fantasy.ToolResultPart:
				if updated, ok := shortenToolResultPart(*p, maxBytes, edgeBytes, transcriptPath, idx.toolResults[p.ToolCallID]); ok {
					cloned[i].Content[j] = updated
				}
			}
		}
	}
	return cloned
}

func isSystemReminderMessage(msg fantasy.Message) bool {
	if msg.Role != fantasy.MessageRoleUser || len(msg.Content) != 1 {
		return false
	}
	if tp, ok := fantasy.AsMessagePart[fantasy.TextPart](msg.Content[0]); ok {
		return strings.HasPrefix(tp.Text, "<system_reminder>")
	}
	return false
}

func buildTranscriptSearchNotice(transcriptPath string, shortenedCount, omittedMessages, totalLines int) string {
	var details []string
	if omittedMessages > 0 {
		details = append(details, fmt.Sprintf("%d earlier messages were omitted", omittedMessages))
	}
	if shortenedCount > 0 {
		details = append(details, fmt.Sprintf("%d older tool outputs/inputs were shortened", shortenedCount))
	}
	summary := strings.Join(details, " and ")
	return fmt.Sprintf(`<conversation_history_notice>
To fit this model's context window, %s.
The complete, unshortened conversation transcript (%d lines) is saved on disk at:
%s

If you need any information, file contents, command outputs, or earlier decisions from this conversation, search that file with the grep tool (set path to %q) or read specific lines with the view tool (set file_path to %q with offset and limit).
</conversation_history_notice>`, summary, totalLines, transcriptPath, transcriptPath, transcriptPath)
}

func insertTranscriptNotice(history []fantasy.Message, notice string) []fantasy.Message {
	noticeMsg := fantasy.NewUserMessage(notice)
	if len(history) > 0 && isSystemReminderMessage(history[0]) {
		out := make([]fantasy.Message, 0, len(history)+1)
		out = append(out, history[0], noticeMsg)
		out = append(out, history[1:]...)
		return out
	}
	return append([]fantasy.Message{noticeMsg}, history...)
}
