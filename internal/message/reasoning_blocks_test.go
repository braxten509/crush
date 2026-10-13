package message

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"github.com/stretchr/testify/require"
)

type sentBlock struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	Thinking  string `json:"thinking"`
	Signature string `json:"signature"`
	Data      string `json:"data"`
	ID        string `json:"id"`
}

func sendToAnthropic(t *testing.T, history []fantasy.Message) [][]sentBlock {
	t.Helper()
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"stop"}}`))
	}))
	defer srv.Close()
	provider, err := anthropic.New(anthropic.WithBaseURL(srv.URL), anthropic.WithAPIKey("test"))
	require.NoError(t, err)
	model, err := provider.LanguageModel(t.Context(), "claude-test")
	require.NoError(t, err)
	prompt := append([]fantasy.Message{fantasy.NewUserMessage("start")}, history...)
	prompt = append(prompt, fantasy.NewUserMessage("Continue"))
	_, _ = model.Generate(t.Context(), fantasy.Call{Prompt: prompt})
	require.NotEmpty(t, body)

	var req struct {
		Messages []struct {
			Role    string      `json:"role"`
			Content []sentBlock `json:"content"`
		} `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(body, &req))
	var assistant [][]sentBlock
	for _, msg := range req.Messages {
		if msg.Role == "assistant" {
			assistant = append(assistant, msg.Content)
		}
	}
	return assistant
}

func blockSummary(blocks []sentBlock) []string {
	var out []string
	for _, b := range blocks {
		switch b.Type {
		case "thinking":
			out = append(out, "thinking:"+b.Thinking+":"+b.Signature)
		case "redacted_thinking":
			out = append(out, "redacted:"+b.Data)
		case "text":
			out = append(out, "text:"+b.Text)
		case "tool_use":
			out = append(out, "tool:"+b.ID)
		default:
			out = append(out, b.Type)
		}
	}
	return out
}

func TestReasoningSentBackBeforeTextLikeReceived(t *testing.T) {
	t.Parallel()

	msg := Message{Role: Assistant}
	msg.StartReasoning("plan")
	msg.AppendReasoningSignature("SIG1")
	msg.FinishThinking()
	msg.AppendContent("Checkpoint saved.")
	msg.AddToolCall(ToolCall{ID: "call1", Name: "bash", Input: `{}`, Finished: true})

	got := sendToAnthropic(t, msg.ToAIMessage())
	require.Len(t, got, 1)
	require.Equal(t, []string{"thinking:plan:SIG1", "text:Checkpoint saved.", "tool:call1"}, blockSummary(got[0]))
}

func TestEachReasoningBlockKeepsItsOwnSignatureAndPlace(t *testing.T) {
	t.Parallel()

	msg := Message{Role: Assistant}
	msg.StartReasoning("first")
	msg.AppendReasoningContent(" thought")
	msg.AppendReasoningSignature("SIG1")
	msg.FinishThinking()
	msg.AppendContent("Looking now. ")
	msg.StartReasoning("")
	msg.AppendReasoningContent("second")
	msg.AppendReasoningSignature("SIG2")
	msg.FinishThinking()
	msg.AppendContent("Running it.")
	msg.AddToolCall(ToolCall{ID: "call1", Name: "bash", Input: `{}`, Finished: true})
	msg.StartReasoning("third")
	msg.AppendReasoningSignature("SIG3")
	msg.FinishThinking()
	msg.AddToolCall(ToolCall{ID: "call2", Name: "view", Input: `{}`, Finished: true})

	encoded, err := marshalParts(msg.Parts)
	require.NoError(t, err)
	msg.Parts, err = unmarshalParts(encoded)
	require.NoError(t, err)

	got := sendToAnthropic(t, msg.ToAIMessage())
	require.Len(t, got, 1)
	require.Equal(t, []string{
		"thinking:first thought:SIG1",
		"text:Looking now.",
		"thinking:second:SIG2",
		"text:Running it.",
		"tool:call1",
		"thinking:third:SIG3",
		"tool:call2",
	}, blockSummary(got[0]))
	require.Equal(t, "first thought\n\nsecond\n\nthird", msg.ReasoningContent().Thinking)
}

func TestRedactedReasoningIsSentBack(t *testing.T) {
	t.Parallel()

	msg := Message{Role: Assistant}
	msg.StartReasoning("")
	msg.SetReasoningRedactedData("SECRET")
	msg.FinishThinking()
	msg.AppendContent("Done.")

	got := sendToAnthropic(t, msg.ToAIMessage())
	require.Len(t, got, 1)
	require.Equal(t, []string{"redacted:SECRET", "text:Done."}, blockSummary(got[0]))
}

func TestOldGluedReasoningIsLeftOutForAnthropic(t *testing.T) {
	t.Parallel()

	msg := Message{Role: Assistant, Parts: []ContentPart{
		ReasoningContent{Thinking: "one two", Signature: "SIG1SIG2", StartedAt: 1, FinishedAt: 2},
		TextContent{Text: "Answer."},
		ToolCall{ID: "call1", Name: "bash", Input: `{}`, Finished: true},
	}}

	got := sendToAnthropic(t, msg.ToAIMessage())
	require.Len(t, got, 1)
	require.Equal(t, []string{"text:Answer.", "tool:call1"}, blockSummary(got[0]))
}

func TestReasoningStillOpenWhileLaterBlockStreams(t *testing.T) {
	t.Parallel()

	msg := Message{Role: Assistant}
	msg.StartReasoning("a")
	msg.FinishThinking()
	msg.StartReasoning("b")
	require.Zero(t, msg.ReasoningContent().FinishedAt)
	require.True(t, msg.IsThinking())
	msg.FinishThinking()
	require.NotZero(t, msg.ReasoningContent().FinishedAt)
}
