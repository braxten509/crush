package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/x/exp/charmtone"
	"github.com/charmbracelet/x/term"
)

func messagePtrs(msgs []message.Message) []*message.Message {
	ptrs := make([]*message.Message, len(msgs))
	for i := range msgs {
		ptrs[i] = &msgs[i]
	}
	return ptrs
}

func outputSessionJSON(w io.Writer, sess session.Session, msgs []*message.Message) error {
	skills := extractSkillsFromMessages(msgs)
	output := sessionShowOutput{
		Meta: sessionShowMeta{
			ID:               session.HashID(sess.ID),
			UUID:             sess.ID,
			Title:            sess.Title,
			Created:          time.Unix(sess.CreatedAt, 0).Format(time.RFC3339),
			Modified:         time.Unix(sess.UpdatedAt, 0).Format(time.RFC3339),
			Cost:             sess.Cost,
			PromptTokens:     sess.PromptTokens,
			CompletionTokens: sess.CompletionTokens,
			TotalTokens:      sess.PromptTokens + sess.CompletionTokens,
			Skills:           skills,
		},
		Messages: make([]sessionShowMessage, len(msgs)),
	}

	for i, msg := range msgs {
		output.Messages[i] = sessionShowMessage{
			ID:       msg.ID,
			Role:     string(msg.Role),
			Created:  time.Unix(msg.CreatedAt, 0).Format(time.RFC3339),
			Model:    msg.Model,
			Provider: msg.Provider,
			Parts:    convertParts(msg.Parts),
		}
	}

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(output)
}

func outputSessionHuman(ctx context.Context, cfg *config.ConfigStore, sess session.Session, msgs []*message.Message) error {
	styles := common.ThemeStylesFromConfig(cfg.Config())
	toolResults := chat.BuildToolResultMap(msgs)

	width := sessionOutputWidth
	if w, _, err := term.GetSize(os.Stdout.Fd()); err == nil && w > 0 {
		width = w
	}
	contentWidth := min(width, sessionMaxContentWidth)

	keyStyle := lipgloss.NewStyle().Foreground(charmtone.Damson)
	valStyle := lipgloss.NewStyle().Foreground(charmtone.Malibu)

	hash := session.HashID(sess.ID)[:12]
	created := time.Unix(sess.CreatedAt, 0).Format("Mon Jan 2 15:04:05 2006 -0700")

	skills := extractSkillsFromMessages(msgs)

	// Render to buffer to determine actual height
	var buf strings.Builder

	fmt.Fprintln(&buf, keyStyle.Render("ID:    ")+valStyle.Render(hash))
	fmt.Fprintln(&buf, keyStyle.Render("UUID:  ")+valStyle.Render(sess.ID))
	fmt.Fprintln(&buf, keyStyle.Render("Title: ")+valStyle.Render(sess.Title))
	fmt.Fprintln(&buf, keyStyle.Render("Date:  ")+valStyle.Render(created))
	if len(skills) > 0 {
		skillNames := make([]string, len(skills))
		for i, s := range skills {
			timestamp := time.Unix(sess.CreatedAt, 0).Format("15:04:05 -0700")
			if s.LoadedAt != "" {
				if t, err := time.Parse(time.RFC3339, s.LoadedAt); err == nil {
					timestamp = t.Format("15:04:05 -0700")
				}
			}
			skillNames[i] = fmt.Sprintf("%s (%s)", s.Name, timestamp)
		}
		fmt.Fprintln(&buf, keyStyle.Render("Skills: ")+valStyle.Render(strings.Join(skillNames, ", ")))
	}
	fmt.Fprintln(&buf)

	first := true
	for _, msg := range msgs {
		items := chat.ExtractMessageItems(&styles, msg, toolResults, "")
		for _, item := range items {
			if !first {
				fmt.Fprintln(&buf)
			}
			first = false
			fmt.Fprintln(&buf, item.Render(contentWidth))
		}
	}
	fmt.Fprintln(&buf)

	contentHeight := strings.Count(buf.String(), "\n")
	w, cleanup, usingPager := sessionWriter(ctx, contentHeight)
	defer cleanup()

	_, err := io.WriteString(w, buf.String())
	// Ignore broken pipe errors when using a pager. This happens when the user
	// exits the pager early (e.g., pressing 'q' in less), which closes the pipe
	// and causes subsequent writes to fail. These errors are expected user behavior.
	if err != nil && usingPager && isBrokenPipe(err) {
		return nil
	}
	return err
}

type sessionShowMeta struct {
	ID               string             `json:"id"`
	UUID             string             `json:"uuid"`
	Title            string             `json:"title"`
	Created          string             `json:"created"`
	Modified         string             `json:"modified"`
	Cost             float64            `json:"cost"`
	PromptTokens     int64              `json:"prompt_tokens"`
	CompletionTokens int64              `json:"completion_tokens"`
	TotalTokens      int64              `json:"total_tokens"`
	Skills           []sessionShowSkill `json:"skills,omitempty"`
}

type sessionShowSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	LoadedAt    string `json:"loaded_at"`
}

type sessionShowMessage struct {
	ID       string            `json:"id"`
	Role     string            `json:"role"`
	Created  string            `json:"created"`
	Model    string            `json:"model,omitempty"`
	Provider string            `json:"provider,omitempty"`
	Parts    []sessionShowPart `json:"parts"`
}

type sessionShowPart struct {
	Type string `json:"type"`

	// Text content
	Text string `json:"text,omitempty"`

	// Reasoning
	Thinking   string `json:"thinking,omitempty"`
	StartedAt  int64  `json:"started_at,omitempty"`
	FinishedAt int64  `json:"finished_at,omitempty"`

	// Tool call
	ToolCallID string `json:"tool_call_id,omitempty"`
	Name       string `json:"name,omitempty"`
	Input      string `json:"input,omitempty"`

	// Tool result
	Content  string `json:"content,omitempty"`
	IsError  bool   `json:"is_error,omitempty"`
	MIMEType string `json:"mime_type,omitempty"`

	// Binary
	Size int64 `json:"size,omitempty"`

	// Image URL
	URL    string `json:"url,omitempty"`
	Detail string `json:"detail,omitempty"`

	// Finish
	Reason string `json:"reason,omitempty"`
	Time   int64  `json:"time,omitempty"`
}

func extractSkillsFromMessages(msgs []*message.Message) []sessionShowSkill {
	var skills []sessionShowSkill
	seen := make(map[string]bool)

	for _, msg := range msgs {
		for _, part := range msg.Parts {
			if tr, ok := part.(message.ToolResult); ok && tr.Metadata != "" {
				var meta tools.ViewResponseMetadata
				if err := json.Unmarshal([]byte(tr.Metadata), &meta); err == nil {
					if meta.ResourceType == tools.ViewResourceSkill && meta.ResourceName != "" {
						if !seen[meta.ResourceName] {
							seen[meta.ResourceName] = true
							skills = append(skills, sessionShowSkill{
								Name:        meta.ResourceName,
								Description: meta.ResourceDescription,
								LoadedAt:    time.Unix(msg.CreatedAt, 0).Format(time.RFC3339),
							})
						}
					}
				}
			}
		}
	}

	sort.Slice(skills, func(i, j int) bool {
		if skills[i].LoadedAt == skills[j].LoadedAt {
			return skills[i].Name < skills[j].Name
		}
		return skills[i].LoadedAt < skills[j].LoadedAt
	})

	return skills
}

func convertParts(parts []message.ContentPart) []sessionShowPart {
	result := make([]sessionShowPart, 0, len(parts))
	for _, part := range parts {
		switch p := part.(type) {
		case message.TextContent:
			result = append(result, sessionShowPart{
				Type: "text",
				Text: p.Text,
			})
		case message.ReasoningContent:
			result = append(result, sessionShowPart{
				Type:       "reasoning",
				Thinking:   p.Thinking,
				StartedAt:  p.StartedAt,
				FinishedAt: p.FinishedAt,
			})
		case message.ToolCall:
			result = append(result, sessionShowPart{
				Type:       "tool_call",
				ToolCallID: p.ID,
				Name:       p.Name,
				Input:      p.Input,
			})
		case message.ToolResult:
			result = append(result, sessionShowPart{
				Type:       "tool_result",
				ToolCallID: p.ToolCallID,
				Name:       p.Name,
				Content:    p.Content,
				IsError:    p.IsError,
				MIMEType:   p.MIMEType,
			})
		case message.BinaryContent:
			result = append(result, sessionShowPart{
				Type:     "binary",
				MIMEType: p.MIMEType,
				Size:     int64(len(p.Data)),
			})
		case message.ImageURLContent:
			result = append(result, sessionShowPart{
				Type:   "image_url",
				URL:    p.URL,
				Detail: p.Detail,
			})
		case message.Finish:
			result = append(result, sessionShowPart{
				Type:   "finish",
				Reason: string(p.Reason),
				Time:   p.Time,
			})
		default:
			result = append(result, sessionShowPart{
				Type: "unknown",
			})
		}
	}
	return result
}

type sessionShowOutput struct {
	Meta     sessionShowMeta      `json:"meta"`
	Messages []sessionShowMessage `json:"messages"`
}
