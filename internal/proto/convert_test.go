package proto

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/commands"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/stretchr/testify/require"
)

// overTheWire marshals v to JSON and decodes it back, mimicking a value
// crossing the client/server boundary.
func overTheWire[T any](t *testing.T, v T) T {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	var out T
	require.NoError(t, json.Unmarshal(data, &out))
	return out
}

func TestSessionRoundTrip(t *testing.T) {
	t.Parallel()
	src := session.Session{
		ID:               "s1",
		ParentSessionID:  "p1",
		Title:            "Title",
		MessageCount:     3,
		PromptTokens:     10,
		CompletionTokens: 20,
		SummaryMessageID: "m9",
		Cost:             1.25,
		Todos: []session.Todo{
			{Content: "do it", Status: session.TodoStatus("in_progress"), ActiveForm: "Doing it"},
		},
		Channel:    "signal",
		KeptSkills: []session.KeptSkill{{ID: "k", Name: "Kept", ByAgent: true}},
		CreatedAt:  100,
		UpdatedAt:  200,
	}
	require.Equal(t, src, SessionToDomain(overTheWire(t, SessionFromDomain(src))))
	require.Equal(t, []session.Session{src}, SessionsToDomain([]Session{SessionFromDomain(src)}))

	// Empty lists stay nil so they are omitted from JSON.
	empty := SessionFromDomain(session.Session{ID: "s2"})
	require.Nil(t, empty.Todos)
	require.Nil(t, empty.KeptSkills)
}

func TestFileRoundTrip(t *testing.T) {
	t.Parallel()
	src := history.File{
		ID: "f1", SessionID: "s1", Path: "a.go", Content: "package a",
		Version: 2, CreatedAt: 1, UpdatedAt: 3,
	}
	require.Equal(t, src, FileToDomain(overTheWire(t, FileFromDomain(src))))
	require.Equal(t, []history.File{src}, FilesToDomain(FilesFromDomain([]history.File{src})))
}

func TestMessageRoundTripAllParts(t *testing.T) {
	t.Parallel()
	savings := 1.5
	src := message.Message{
		ID:                      "m1",
		SessionID:               "s1",
		Role:                    message.Assistant,
		Model:                   "model",
		Provider:                "hyper",
		PrismModelID:            "prism-42",
		PrismModelName:          "GLM",
		PrismHypercreditSavings: &savings,
		CreatedAt:               1,
		UpdatedAt:               2,
		IsSummaryMessage:        true,
		Parts: []message.ContentPart{
			message.TextContent{Text: "hi", Hidden: true, SubmissionID: "sub"},
			message.ReasoningContent{Thinking: "hmm", Signature: "sig", StartedAt: 1, FinishedAt: 2},
			message.ToolCall{ID: "c1", Name: "view", Input: `{"a":1}`, Finished: true},
			message.ToolResult{
				ToolCallID: "c1", Name: "view", Content: "out", Data: "ZGF0YQ==",
				MIMEType: "image/png", Metadata: `{"k":"v"}`, IsError: true,
			},
			message.ImageURLContent{URL: "https://x/y.png", Detail: "high"},
			message.BinaryContent{Path: "/tmp/a.png", MIMEType: "image/png", Data: []byte("abc")},
			message.ShellCommand{Command: "ls", Output: "a\nb", ExitCode: 1},
			message.Finish{Reason: message.FinishReasonEndTurn, Time: 5, Message: "done", Details: "d"},
		},
	}

	wire := MessageFromDomain(src)
	require.Len(t, wire.Parts, len(src.Parts))
	require.Equal(t, src, MessageToDomain(wire))

	// The parts also survive JSON, which is what actually crosses the
	// socket.
	require.Equal(t, src, MessageToDomain(overTheWire(t, wire)))

	require.Equal(t, []message.Message{src}, MessagesToDomain(MessagesFromDomain([]message.Message{src})))
}

func TestPermissionRoundTrip(t *testing.T) {
	t.Parallel()
	req := permission.PermissionRequest{
		ID: "p1", SessionID: "s1", ToolCallID: "c1", ToolName: "bash",
		Description: "run", Action: "execute", Path: "/tmp", Params: "raw",
	}
	require.Equal(t, req, PermissionRequestToDomain(PermissionRequestFromDomain(req)))

	n := permission.PermissionNotification{ToolCallID: "c1", Granted: true, Denied: false}
	require.Equal(t, n, PermissionNotificationToDomain(overTheWire(t, PermissionNotificationFromDomain(n))))
}

func TestQuestionRoundTrip(t *testing.T) {
	t.Parallel()
	req := question.Request{
		ID: "b1", SessionID: "s1", ToolCallID: "c1",
		Questions: []question.Question{{
			ID: "q", Type: question.TypeSingleChoice, Label: "L", Text: "Which?", Description: "D",
			Choices: []question.Choice{{ID: "a", Label: "A", Description: "first", Image: "/tmp/a.png"}},
		}},
		ConfirmTitle:       "Sure?",
		ConfirmDescription: "Really",
	}
	require.Equal(t, req, QuestionRequestToDomain(overTheWire(t, QuestionRequestFromDomain(req))))

	yes := true
	answers := []question.Answer{{QuestionID: "q", SelectedIDs: []string{"a"}, FillInText: "x", Yes: &yes, Notes: map[string]string{"a": "n"}}}
	require.Equal(t, answers, QuestionAnswerToDomain(overTheWire(t, QuestionAnswerFromDomain(answers))))
}

// A choice's sketch is sent on to the screen with the question.
func TestQuestionChoiceImageSent(t *testing.T) {
	t.Parallel()
	got := QuestionsFromDomain([]question.Question{{
		ID: "q", Type: question.TypeSingleChoice, Text: "Which?",
		Choices: []question.Choice{{ID: "a", Label: "A", Image: "/tmp/a.png"}},
	}})
	require.Equal(t, "/tmp/a.png", got[0].Choices[0].Image)
}

// A choice's sketch survives the trip from Crush's background process to
// the screen, so the question form can show it.
func TestQuestionChoiceImageReachesScreen(t *testing.T) {
	t.Parallel()
	got := QuestionsToDomain([]QuestionItem{{
		ID: "q", Type: "single_choice", Question: "Which?",
		Choices: []QuestionChoice{{ID: "a", Label: "A", Image: "/tmp/a.png"}, {ID: "b", Label: "B"}},
	}})
	require.Equal(t, "/tmp/a.png", got[0].Choices[0].Image)
	require.Empty(t, got[0].Choices[1].Image)
}

func TestMCPRoundTrip(t *testing.T) {
	t.Parallel()
	for _, typ := range []mcp.EventType{
		mcp.EventStateChanged,
		mcp.EventToolsListChanged,
		mcp.EventPromptsListChanged,
		mcp.EventResourcesListChanged,
		mcp.EventChannelMessage,
	} {
		ev := mcp.Event{
			Type: typ, Name: "srv", State: mcp.StateConnected,
			Counts:         mcp.Counts{Tools: 1, Prompts: 2, Resources: 3},
			ChannelMessage: "<channel>hi</channel>",
		}
		wire, ok := MCPEventFromDomain(ev)
		require.True(t, ok)
		require.Equal(t, ev, MCPEventToDomain(overTheWire(t, wire)))
	}

	_, ok := MCPEventFromDomain(mcp.Event{Type: mcp.EventType(99)})
	require.False(t, ok, "unknown types must not be coerced to state_changed")
	require.Equal(t, mcp.EventStateChanged, MCPEventTypeToDomain("bogus"))

	info := mcp.ClientInfo{
		Name: "srv", State: mcp.StateConnected,
		Counts:      mcp.Counts{Tools: 4, Prompts: 5, Resources: 6},
		ConnectedAt: time.Unix(1700000000, 0),
		Channel:     true,
	}
	got := MCPClientInfosToDomain(MCPClientInfosFromDomain(map[string]mcp.ClientInfo{"srv": info}))
	require.Equal(t, info, got["srv"])

	pending := []mcp.PendingAuthServer{{Name: "srv", URL: "https://auth"}}
	require.Equal(t, pending, MCPPendingAuthToDomain(overTheWire(t, MCPPendingAuthFromDomain(pending))))

	prompts := []commands.MCPPrompt{{
		ID: "srv:p", Title: "P", Description: "D", PromptID: "p", ClientID: "srv",
		Arguments: []commands.Argument{{ID: "a", Title: "A", Description: "arg", Required: true}},
	}}
	require.Equal(t, prompts, MCPPromptsToDomain(overTheWire(t, MCPPromptsFromDomain(prompts))))
}

func TestAgentEventRoundTrip(t *testing.T) {
	t.Parallel()
	n := notify.Notification{
		SessionID: "s1", SessionTitle: "T", RunID: "r1", Type: notify.TypeAgentError,
		Message: "boom", AWSSOCommand: "aws sso login", AWSSOURL: "https://sso",
	}
	wire := AgentEventFromDomain(n)
	require.Equal(t, AgentEventTypeError, wire.Type)
	require.Equal(t, n, AgentEventToDomain(overTheWire(t, wire)))

	rc := notify.RunComplete{
		SubmissionID: "sub", SessionID: "s1", RunID: "r1", MessageID: "m1",
		Text: "done", Error: "e", Cancelled: true,
	}
	require.Equal(t, rc, RunCompleteToDomain(overTheWire(t, RunCompleteFromDomain(rc))))
}

func TestSkillsRoundTrip(t *testing.T) {
	t.Parallel()
	states := []*skills.SkillState{
		{Name: "ok", Path: "/a", State: skills.StateNormal},
		{Name: "bad", Path: "/b", State: skills.StateError, Err: errors.New("parse failed")},
	}
	got := SkillStatesToDomain(overTheWire(t, SkillsEventFromDomain(skills.Event{States: states})).States)
	require.Len(t, got, 2)
	require.Equal(t, "ok", got[0].Name)
	require.NoError(t, got[0].Err)
	require.EqualError(t, got[1].Err, "parse failed")
	require.Equal(t, skills.StateError, got[1].State)
	require.Nil(t, SkillStatesFromDomain(nil))

	entries := []skills.CatalogEntry{{
		ID: "id", Name: "n", Description: "d", Label: "l",
		Source: skills.SourceType("user"), UserInvocable: true,
	}}
	require.Equal(t, entries, SkillInfosToDomain(overTheWire(t, SkillInfosFromDomain(entries))))

	res := skills.SkillReadResult{Name: "n", Description: "d", Source: skills.SourceType("builtin"), Builtin: true}
	require.Equal(t, res, SkillReadResultToDomain(overTheWire(t, SkillReadResultFromDomain(res))))
}
