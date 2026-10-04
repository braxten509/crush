package message

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/stretchr/testify/require"
)

func fixtureReview() ToolResult {
	return ToolResult{ToolCallID: "call", Name: "bash", Content: "command output stays visible", Metadata: `{"output":"command output stays visible"}`,
		Review: &filechange.Review{Root: "/workspace", Changes: []filechange.Change{{Path: "/workspace/main.go", Before: &filechange.State{Content: "before\n"}, After: &filechange.State{Content: "after\n"}}}}}
}

func TestParentPromptRetentionAlsoExpiresOldSubagentDiffs(t *testing.T) {
	svc, parentID := newTestService(t)
	s := svc.(*service)
	_, err := svc.Create(t.Context(), parentID, CreateMessageParams{Role: User, Parts: []ContentPart{TextContent{Text: "First prompt"}}})
	require.NoError(t, err)
	_, err = s.q.CreateSession(t.Context(), db.CreateSessionParams{ID: "child", ParentSessionID: sql.NullString{String: parentID, Valid: true}, Title: "Child"})
	require.NoError(t, err)
	child, err := svc.Create(t.Context(), "child", CreateMessageParams{Role: Tool, Parts: []ContentPart{fixtureReview()}})
	require.NoError(t, err)
	for range 4 {
		_, err := svc.Create(t.Context(), parentID, CreateMessageParams{Role: User, Parts: []ContentPart{TextContent{Text: "Later prompt"}}})
		require.NoError(t, err)
	}
	_, err = svc.LoadReview(t.Context(), child.ID)
	require.NoError(t, err, "the child ran during one of the latest five prompts")
	_, err = svc.Create(t.Context(), parentID, CreateMessageParams{Role: User, Parts: []ContentPart{TextContent{Text: "Sixth prompt"}}})
	require.NoError(t, err)
	_, err = svc.LoadReview(t.Context(), child.ID)
	require.ErrorIs(t, err, ErrReviewExpired)
	msgs, err := svc.List(t.Context(), "child")
	require.NoError(t, err)
	require.Len(t, msgs, 1, "subagent text and tool history remain")
}

func TestOversizedEditMetadataCannotBecomeAFakeDeletion(t *testing.T) {
	svc, sessionID := newTestService(t)
	metadata, err := json.Marshal(map[string]string{"old_content": strings.Repeat("old", reviewTextBudget), "new_content": "new"})
	require.NoError(t, err)
	msg, err := svc.Create(t.Context(), sessionID, CreateMessageParams{Role: Tool, Parts: []ContentPart{ToolResult{ToolCallID: "large", Name: "edit", Metadata: string(metadata)}}})
	require.NoError(t, err)
	full, err := svc.LoadReview(t.Context(), msg.ID)
	require.NoError(t, err)
	require.NotContains(t, full.ToolResults()[0].Metadata, "old_content")
	require.NotContains(t, full.ToolResults()[0].Metadata, "new_content")
	require.Contains(t, full.ToolResults()[0].Metadata, "review_omitted")
}

func TestReviewRetentionKeepsFivePromptsAndAllCommands(t *testing.T) {
	svc, sessionID := newTestService(t, WithDebounce(0))
	var tools []Message
	for i := range 7 {
		_, err := svc.Create(t.Context(), sessionID, CreateMessageParams{Role: User, Parts: []ContentPart{TextContent{Text: fmt.Sprintf("prompt %d", i)}}})
		require.NoError(t, err)
		_, err = svc.Create(t.Context(), sessionID, CreateMessageParams{Role: Assistant, Parts: []ContentPart{
			TextContent{Text: fmt.Sprintf("reply %d", i)}, ToolCall{ID: "call", Name: "bash", Input: `{"command":"echo visible"}`, Finished: true}, Finish{Reason: FinishReasonEndTurn},
		}})
		require.NoError(t, err)
		msg, err := svc.Create(t.Context(), sessionID, CreateMessageParams{Role: Tool, Parts: []ContentPart{fixtureReview()}})
		require.NoError(t, err)
		tools = append(tools, msg)
	}
	for i, tool := range tools {
		full, err := svc.LoadReview(t.Context(), tool.ID)
		if i < 2 {
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
		require.Equal(t, "before\n", full.ToolResults()[0].Review.Changes[0].Before.Content)
	}
	msgs, err := svc.List(t.Context(), sessionID)
	require.NoError(t, err)
	require.Len(t, msgs, 21)
	for _, msg := range msgs {
		if msg.Role == User {
			continue
		}
		if msg.Role == Tool {
			result := msg.ToolResults()[0]
			require.Equal(t, "command output stays visible", result.Content)
			require.NotNil(t, result.Review.Summary)
			require.Empty(t, result.Review.Changes)
		} else {
			require.Equal(t, `{"command":"echo visible"}`, msg.ToolCalls()[0].Input)
		}
	}
	// A delayed result cannot recreate an expired full diff.
	old := tools[0]
	old.Parts = []ContentPart{fixtureReview(), Finish{Reason: FinishReasonEndTurn}}
	require.NoError(t, svc.Update(t.Context(), old))
	_, err = svc.LoadReview(t.Context(), old.ID)
	require.Error(t, err)
}

func TestProgressTextAndTaskResultsDoNotExpireDiffs(t *testing.T) {
	svc, sessionID := newTestService(t, WithDebounce(0))
	_, err := svc.Create(t.Context(), sessionID, CreateMessageParams{Role: User, Parts: []ContentPart{TextContent{Text: "one long request"}}})
	require.NoError(t, err)
	tool, err := svc.Create(t.Context(), sessionID, CreateMessageParams{Role: Tool, Parts: []ContentPart{fixtureReview()}})
	require.NoError(t, err)
	for i := range 12 {
		_, err := svc.Create(t.Context(), sessionID, CreateMessageParams{Role: Assistant, Parts: []ContentPart{TextContent{Text: fmt.Sprintf("progress %d", i)}, Finish{Reason: FinishReasonEndTurn}}})
		require.NoError(t, err)
		_, err = svc.Create(t.Context(), sessionID, CreateMessageParams{Role: User, Parts: []ContentPart{TextContent{Text: fmt.Sprintf("<crush-task-result>\n<name>job %d</name>\n</crush-task-result>", i)}}})
		require.NoError(t, err)
	}
	full, err := svc.LoadReview(t.Context(), tool.ID)
	require.NoError(t, err)
	require.Equal(t, "after\n", full.ToolResults()[0].Review.Changes[0].After.Content)
}

func TestReviewListStaysSmallAndDetailPayloadIsBounded(t *testing.T) {
	svc, sessionID := newTestService(t)
	result := fixtureReview()
	result.Review.Changes[0].After.Content = strings.Repeat("large content\n", 400_000)
	msg, err := svc.Create(t.Context(), sessionID, CreateMessageParams{Role: Tool, Parts: []ContentPart{result}})
	require.NoError(t, err)
	encoded, err := marshalParts(msg.Parts)
	require.NoError(t, err)
	require.Less(t, len(encoded), 2048)
	full, err := svc.LoadReview(t.Context(), msg.ID)
	require.NoError(t, err)
	require.Empty(t, full.ToolResults()[0].Review.Changes[0].After.Content)
	require.Contains(t, full.ToolResults()[0].Review.Changes[0].After.Omitted, "size limit")
	require.NotEmpty(t, result.Review.Changes[0].After.Content, "caller's snapshot remains immutable")
}

func TestLegacyReviewMigrationPreservesOutputAndClassifiesClone(t *testing.T) {
	svc, sessionID := newTestService(t)
	s := svc.(*service)
	input, _ := json.Marshal([]partWrapper{{Type: toolCallType, Data: ToolCall{ID: "call", Name: "bash", Input: `{"command":"git clone source destination"}`}}})
	_, err := s.q.CreateMessage(t.Context(), db.CreateMessageParams{ID: "assistant", SessionID: sessionID, Role: string(Assistant), Parts: string(input)})
	require.NoError(t, err)
	result := fixtureReview()
	result.Review.Changes[0].Before = nil
	parts, err := marshalParts([]ContentPart{result})
	require.NoError(t, err)
	_, err = s.q.CreateMessage(t.Context(), db.CreateMessageParams{ID: "legacy", SessionID: sessionID, Role: string(Tool), Parts: string(parts)})
	require.NoError(t, err)
	msgs, err := svc.List(t.Context(), sessionID)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	row, err := s.q.GetMessage(t.Context(), "legacy")
	require.NoError(t, err)
	require.NotContains(t, row.Parts, `after\\n`)
	msg, err := s.fromDBItem(row)
	require.NoError(t, err)
	require.Equal(t, 1, msg.ToolResults()[0].Review.Summary.Checkouts)
	_, err = svc.LoadReview(context.Background(), "legacy")
	require.NoError(t, err)
}
