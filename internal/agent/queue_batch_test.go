package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/stretchr/testify/require"
)

// Record actual provider inputs, with gates at chosen turn boundaries.
type batchRecordingModel struct {
	finishStreamModel
	mu      sync.Mutex
	calls   []fantasy.Call
	entered chan int
	gates   map[int]chan struct{}
	failAt  int
}

func (m *batchRecordingModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.mu.Lock()
	m.calls = append(m.calls, call)
	number := len(m.calls)
	m.mu.Unlock()
	m.entered <- number
	if gate := m.gates[number]; gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if number == m.failAt {
		return nil, errors.New("batch provider failed")
	}
	return m.finishStreamModel.Stream(ctx, call)
}
func (m *batchRecordingModel) snapshot() []fantasy.Call {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]fantasy.Call(nil), m.calls...)
}
func batchUserTexts(call fantasy.Call) []string {
	var texts []string
	for _, msg := range call.Prompt {
		if msg.Role != fantasy.MessageRoleUser {
			continue
		}
		for _, part := range msg.Content {
			if text, ok := fantasy.AsMessagePart[fantasy.TextPart](part); ok {
				texts = append(texts, text.Text)
			}
		}
	}
	return texts
}
func batchWaitTurn(t *testing.T, model *batchRecordingModel, number int) {
	t.Helper()
	select {
	case got := <-model.entered:
		require.Equal(t, number, got)
	case <-time.After(5 * time.Second):
		t.Fatal("provider turn never started")
	}
}

func TestQueueBatchNaturalAndInterrupt(t *testing.T) {
	for _, interrupt := range []bool{false, true} {
		name := "natural"
		if interrupt {
			name = "interrupt"
		}
		t.Run(name, func(t *testing.T) {
			env := testEnv(t)
			large := &batchRecordingModel{finishStreamModel: finishStreamModel{text: "done"}, entered: make(chan int, 10), gates: map[int]chan struct{}{1: make(chan struct{})}}
			sa := testSessionAgent(env, large, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
			sa.isSubAgent = true
			selected := sa.largeModel.Get()
			selected.CatwalkCfg.SupportsImages = true
			sa.largeModel.Set(selected)
			broker := pubsub.NewBroker[notify.RunComplete]()
			t.Cleanup(broker.Shutdown)
			sa.runComplete = broker
			events := broker.Subscribe(t.Context())
			sess, err := env.sessions.Create(t.Context(), "batch")
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() {
				_, err := sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "active", RunID: "active", SubmissionID: "active-id"})
				done <- err
			}()
			batchWaitTurn(t, large, 1)
			image := message.Attachment{MimeType: "image/png", FileName: "one.png", Content: []byte{0, 255, 1, 2}}
			text := message.Attachment{MimeType: "text/plain", FileName: "notes.txt", Content: []byte("attachment notes")}
			inputs := []SessionAgentCall{
				{SessionID: sess.ID, Prompt: "repeat", RunID: "one", SubmissionID: "one-id", Attachments: []message.Attachment{image, text}},
				{SessionID: sess.ID, Prompt: "repeat", RunID: "two", SubmissionID: "two-id", Attachments: []message.Attachment{{MimeType: "image/jpeg", FileName: "two.jpg", Content: []byte{9, 8, 7}}}},
				{SessionID: sess.ID, Prompt: "hidden", RunID: "three", SubmissionID: "three-id", HiddenUserMessage: true},
			}
			for _, input := range inputs {
				_, err := sa.Run(t.Context(), input)
				require.NoError(t, err)
			}
			require.Equal(t, 3, sa.QueuedPrompts(sess.ID))
			if interrupt {
				sa.Interrupt(sess.ID)
			} else {
				close(large.gates[1])
			}
			select {
			case err := <-done:
				if interrupt {
					require.ErrorIs(t, err, context.Canceled)
				} else {
					require.NoError(t, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("run did not finish")
			}
			batchWaitTurn(t, large, 2)
			require.Eventually(t, func() bool { return !sa.IsSessionBusy(sess.ID) }, 5*time.Second, time.Millisecond)
			calls := large.snapshot()
			require.Len(t, calls, 2, "all queued inputs require one provider turn")
			require.Equal(t, []string{"active", message.PromptWithTextAttachments("repeat", inputs[0].Attachments), "repeat", "hidden"}, batchUserTexts(calls[1]))
			var images [][]byte
			for _, msg := range calls[1].Prompt {
				for _, part := range msg.Content {
					if file, ok := fantasy.AsMessagePart[fantasy.FilePart](part); ok {
						images = append(images, file.Data)
					}
				}
			}
			require.Equal(t, [][]byte{image.Content, inputs[1].Attachments[0].Content}, images)
			msgs, err := env.messages.List(t.Context(), sess.ID)
			require.NoError(t, err)
			var users []message.Message
			var assistants int
			for _, msg := range msgs {
				if msg.Role == message.User {
					users = append(users, msg)
				}
				if msg.Role == message.Assistant {
					assistants++
				}
			}
			require.Len(t, users, 4)
			require.Equal(t, 2, assistants)
			for i, input := range inputs {
				require.Equal(t, input.Prompt, users[i+1].Content().Text)
				require.Equal(t, input.SubmissionID, users[i+1].Content().SubmissionID)
				require.Len(t, users[i+1].BinaryContent(), len(input.Attachments))
				for j, attachment := range input.Attachments {
					require.Equal(t, attachment.Content, users[i+1].BinaryContent()[j].Data)
					require.Equal(t, attachment.MimeType, users[i+1].BinaryContent()[j].MIMEType)
					require.Equal(t, attachment.FileName, users[i+1].BinaryContent()[j].Path)
				}
			}
			require.True(t, users[3].Content().Hidden)
			completions := map[string]notify.RunComplete{}
			for range 4 {
				select {
				case event := <-events:
					require.NotContains(t, completions, event.Payload.SubmissionID)
					completions[event.Payload.SubmissionID] = event.Payload
				case <-time.After(time.Second):
					t.Fatal("missing completion")
				}
			}
			for _, input := range inputs {
				outcome := completions[input.SubmissionID]
				require.Equal(t, input.RunID, outcome.RunID)
				require.Equal(t, "done", outcome.Text)
				require.False(t, outcome.Cancelled)
				require.Empty(t, outcome.Error)
			}
			require.Equal(t, completions["one-id"].MessageID, completions["two-id"].MessageID)
		})
	}
}

func TestQueueBatchIncomingAfterDrainRunsLater(t *testing.T) {
	env := testEnv(t)
	large := &batchRecordingModel{finishStreamModel: finishStreamModel{text: "done"}, entered: make(chan int, 10), gates: map[int]chan struct{}{1: make(chan struct{}), 2: make(chan struct{})}}
	sa := testSessionAgent(env, large, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	sa.isSubAgent = true
	sess, err := env.sessions.Create(t.Context(), "later")
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, err := sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "active"})
		done <- err
	}()
	batchWaitTurn(t, large, 1)
	for _, text := range []string{"first", "second"} {
		_, err := sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: text})
		require.NoError(t, err)
	}
	close(large.gates[1])
	batchWaitTurn(t, large, 2)
	_, err = sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "later"})
	require.NoError(t, err)
	require.Equal(t, []string{"active", "first", "second"}, batchUserTexts(large.snapshot()[1]))
	close(large.gates[2])
	batchWaitTurn(t, large, 3)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("queue did not finish")
	}
	calls := large.snapshot()
	require.Len(t, calls, 3)
	require.Contains(t, batchUserTexts(calls[2]), "later")
}

func TestQueueBatchRecallAndCanceledSiblingSafety(t *testing.T) {
	sa, _, broker := newCancelTestAgentWithRunComplete(t)
	events := broker.Subscribe(t.Context())
	const sid = "batch-cancel"
	first := sa.BeginAccepted(sid)
	second := sa.BeginAccepted(sid)
	sa.Cancel(sid)
	later := sa.BeginAccepted(sid)
	sa.messageQueue.Set(sid, []SessionAgentCall{
		{SessionID: sid, Prompt: "old-one", SubmissionID: "old-one-id", acceptSeq: first.seq},
		{SessionID: sid, Prompt: "old-two", SubmissionID: "old-two-id", acceptSeq: second.seq},
		{SessionID: sid, Prompt: "new-one", SubmissionID: "new-one-id", acceptSeq: later.seq},
		{SessionID: sid, Prompt: "recall", SubmissionID: "recall-id", acceptSeq: later.seq, Attachments: []message.Attachment{{MimeType: "image/png", Content: []byte{1, 2}}}},
	})
	recalled := sa.RecallQueuedPrompt(sid)
	require.Equal(t, "recall-id", recalled.SubmissionID)
	require.Len(t, recalled.Attachments, 1)
	batch, canceled := sa.drainQueueForStep(sid)
	require.Len(t, batch, 1)
	require.Equal(t, "new-one-id", batch[0].SubmissionID)
	require.Len(t, canceled, 2)
	sa.publishCanceledQueueDrops(canceled)
	// Releasing a covered sibling must not remove the mark covering the other.
	first.Close()
	require.True(t, sa.canceledBySeq(sid, second.seq))
	require.False(t, sa.canceledBySeq(sid, later.seq))
	second.Close()
	later.Close()
	ids := map[string]bool{}
	for range 3 {
		select {
		case e := <-events:
			require.True(t, e.Payload.Cancelled)
			ids[e.Payload.SubmissionID] = true
		case <-time.After(time.Second):
			t.Fatal("cancellation was lost")
		}
	}
	require.Equal(t, map[string]bool{"old-one-id": true, "old-two-id": true, "recall-id": true}, ids)
}

func TestQueueBatchCancelOnEntryCompletesAllAndRetainsSiblingMark(t *testing.T) {
	sa, env, broker := newCancelTestAgentWithRunComplete(t)
	events := broker.Subscribe(t.Context())
	sess, err := env.sessions.Create(t.Context(), "entry")
	require.NoError(t, err)
	mu := sa.sessionMu(sess.ID)
	mu.Lock()
	next := sa.reserveBatchLocked([]SessionAgentCall{{SessionID: sess.ID, Prompt: "same", SubmissionID: "one"}, {SessionID: sess.ID, Prompt: "same", SubmissionID: "two"}})
	mu.Unlock()
	sibling := sa.BeginAccepted(sess.ID)
	sa.Cancel(sess.ID)
	_, err = sa.Run(t.Context(), next)
	require.NoError(t, err)
	mu.Lock()
	require.True(t, sa.canceledBySeq(sess.ID, sibling.seq))
	mu.Unlock()
	sibling.Close()
	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 3)
	for _, id := range []string{"one", "two"} {
		select {
		case e := <-events:
			require.Equal(t, id, e.Payload.SubmissionID)
			require.True(t, e.Payload.Cancelled)
		case <-time.After(time.Second):
			t.Fatal("missing batch terminal")
		}
	}
}

func TestQueueBatchProviderErrorCompletesEverySubmission(t *testing.T) {
	env := testEnv(t)
	large := &batchRecordingModel{finishStreamModel: finishStreamModel{text: "done"}, entered: make(chan int, 10), failAt: 1}
	sa := testSessionAgent(env, large, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	sa.isSubAgent = true
	broker := pubsub.NewBroker[notify.RunComplete]()
	t.Cleanup(broker.Shutdown)
	sa.runComplete = broker
	events := broker.Subscribe(t.Context())
	sess, err := env.sessions.Create(t.Context(), "error")
	require.NoError(t, err)
	mu := sa.sessionMu(sess.ID)
	mu.Lock()
	next := sa.reserveBatchLocked([]SessionAgentCall{{SessionID: sess.ID, Prompt: "one", SubmissionID: "one", RunID: "r1"}, {SessionID: sess.ID, Prompt: "two", SubmissionID: "two", RunID: "r2"}})
	mu.Unlock()
	_, err = sa.Run(t.Context(), next)
	require.ErrorContains(t, err, "batch provider failed")
	for _, id := range []string{"one", "two"} {
		select {
		case e := <-events:
			require.Equal(t, id, e.Payload.SubmissionID)
			require.Contains(t, e.Payload.Error, "batch provider failed")
		case <-time.After(time.Second):
			t.Fatal("missing error terminal")
		}
	}
	require.False(t, sa.IsSessionBusy(sess.ID))
}

func TestQueueBatchUnconfirmedSteeringRestoresOriginalsInOrder(t *testing.T) {
	sa, _, broker := newCancelTestAgentWithRunComplete(t)
	events := broker.Subscribe(t.Context())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s := &cliSteps{running: toolRunningForSteer(), a: sa, ctx: ctx, sessionID: "steer"}
	image := message.Attachment{MimeType: "image/png", Content: []byte{4, 5}}
	for _, id := range []string{"one", "two"} {
		sa.enqueueCall(SessionAgentCall{SessionID: s.sessionID, Prompt: "repeat", SubmissionID: id, RunID: id, Attachments: []message.Attachment{image}})
	}
	text, images := s.steerWithImages()
	require.Equal(t, "repeat\n\nrepeat", text)
	require.Len(t, images, 2)
	sa.enqueueCall(SessionAgentCall{SessionID: s.sessionID, Prompt: "later", SubmissionID: "later"})
	s.returnUnsteered()
	batch, canceled := sa.drainQueueForStep(s.sessionID)
	require.Empty(t, canceled)
	require.Len(t, batch, 3)
	require.Equal(t, []string{"one", "two", "later"}, []string{batch[0].SubmissionID, batch[1].SubmissionID, batch[2].SubmissionID})
	require.Equal(t, image, batch[1].Attachments[0])
	// An abandoned native delivery must terminate every pending correlator.
	sa.requeueFront(s.sessionID, batch[:2])
	_, _ = s.steerWithImages()
	cancel()
	s.returnUnsteered()
	for _, id := range []string{"one", "two"} {
		select {
		case e := <-events:
			require.Equal(t, id, e.Payload.SubmissionID)
			require.True(t, e.Payload.Cancelled)
		case <-time.After(time.Second):
			t.Fatal("missing unconfirmed terminal")
		}
	}
}

func TestQueueBatchNativeConfirmationOwnsEveryOriginal(t *testing.T) {
	env := testEnv(t)
	sa := NewSessionAgent(SessionAgentOptions{Sessions: env.sessions, Messages: env.messages}).(*sessionAgent)
	sess, err := env.sessions.Create(t.Context(), "native")
	require.NoError(t, err)
	var consumed []SessionAgentCall
	s := &cliSteps{running: toolRunningForSteer(), a: sa, ctx: t.Context(), sessionID: sess.ID, onQueuedInput: func(calls []SessionAgentCall) { consumed = append(consumed, calls...) }, sc: fantasy.AgentStreamCall{
		PrepareStep: func(ctx context.Context, _ fantasy.PrepareStepFunctionOptions) (context.Context, fantasy.PrepareStepResult, error) {
			return ctx, fantasy.PrepareStepResult{}, nil
		}, OnStepFinish: func(fantasy.StepResult) error { return nil },
	}}
	require.NoError(t, s.begin())
	for _, id := range []string{"one", "two"} {
		sa.enqueueCall(SessionAgentCall{SessionID: sess.ID, Prompt: "repeat", SubmissionID: id, RunID: id})
	}
	text := s.steer()
	require.Equal(t, "repeat\n\nrepeat", text)
	require.NoError(t, s.handle(cliagent.Event{Type: cliagent.EventUserMessage, Text: text}))
	require.Len(t, consumed, 2)
	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	require.Equal(t, "one", msgs[0].Content().SubmissionID)
	require.Equal(t, "two", msgs[1].Content().SubmissionID)
	require.Empty(t, s.takeSteered(text), "a repeated acknowledgement cannot confirm again")
}

func TestQueueBatchChannelScopesRemainSeparateAndOrdered(t *testing.T) {
	sa, _ := newCancelTestAgent(t)
	calls := []SessionAgentCall{{SessionID: "s", Prompt: "local-one"}, {SessionID: "s", Prompt: "local-two"}, {SessionID: "s", Channel: "chat", Prompt: `<channel sender="a">one</channel>`}, {SessionID: "s", Channel: "chat", Prompt: `<channel sender="a">two</channel>`}, {SessionID: "s", Channel: "chat", Prompt: `<channel sender="b">three</channel>`}, {SessionID: "s", Prompt: "local-three"}}
	sa.messageQueue.Set("s", calls)
	for _, size := range []int{2, 2, 1, 1} {
		batch, canceled := sa.drainQueueForStep("s")
		require.Empty(t, canceled)
		require.Len(t, batch, size)
		require.Equal(t, calls[:size], batch)
		calls = calls[size:]
	}
	require.Empty(t, calls)
}

func TestQueueBatchReservationPreventsLateArrivalOvertakingDrain(t *testing.T) {
	env := testEnv(t)
	large := &batchRecordingModel{finishStreamModel: finishStreamModel{text: "done"}, entered: make(chan int, 10)}
	sa := testSessionAgent(env, large, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	sa.isSubAgent = true
	sess, err := env.sessions.Create(t.Context(), "handoff")
	require.NoError(t, err)
	mu := sa.sessionMu(sess.ID)
	mu.Lock()
	next := sa.reserveBatchLocked([]SessionAgentCall{{SessionID: sess.ID, Prompt: "first"}, {SessionID: sess.ID, Prompt: "second"}})
	mu.Unlock()
	require.True(t, sa.IsSessionBusy(sess.ID))
	result, err := sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "late"})
	require.NoError(t, err)
	require.Nil(t, result)
	require.Empty(t, large.snapshot())
	_, err = sa.Run(t.Context(), next)
	require.NoError(t, err)
	calls := large.snapshot()
	require.Len(t, calls, 2)
	require.Equal(t, []string{"first", "second"}, batchUserTexts(calls[0]))
	require.Equal(t, []string{"first", "second", "late"}, batchUserTexts(calls[1]))
}

func TestQueueBatchNativeCLIReceivesOneCombinedPrompt(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
read -r _
echo '{"jsonrpc":"2.0","id":"1","result":{"protocolVersion":1}}'
read -r _
echo '{"jsonrpc":"2.0","id":"2","result":{"sessionId":"fixture"}}'
read -r prompt
printf '%s\n' "$prompt" >> "$CRUSH_BATCH_CAPTURE"
echo '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"done"}}}}'
echo '{"jsonrpc":"2.0","id":"3","result":{"stopReason":"end_turn"}}'
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "grok"), []byte(script), 0755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Keep the attachment cache and fake CLI's state inside the test directory.
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	t.Setenv("HOME", dir)
	cache, err := os.UserCacheDir()
	require.NoError(t, err)
	provider := cliagent.NewProvider(config.TypeGrokCLI, dir, dir, nil, nil, "", false)
	model, err := provider.LanguageModel(t.Context(), "fixture")
	require.NoError(t, err)
	capture := filepath.Join(dir, "capture.jsonl")
	model.(*cliagent.Model).Env = []string{"CRUSH_BATCH_CAPTURE=" + capture}
	env := testEnv(t)
	sa := testSessionAgent(env, model, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	sa.isSubAgent = true
	sess, err := env.sessions.Create(t.Context(), "native")
	require.NoError(t, err)
	broker := pubsub.NewBroker[notify.RunComplete]()
	t.Cleanup(broker.Shutdown)
	sa.runComplete = broker
	events := broker.Subscribe(t.Context())
	calls := []SessionAgentCall{{SessionID: sess.ID, Prompt: "first batch prompt", SubmissionID: "one", Attachments: []message.Attachment{{MimeType: "text/plain", FileName: "note.txt", Content: []byte("inline note")}, {MimeType: "image/png", FileName: "first.png", Content: []byte{1, 2, 3}}}}, {SessionID: sess.ID, Prompt: "second batch prompt", SubmissionID: "two", Attachments: []message.Attachment{{MimeType: "image/png", FileName: "second.png", Content: []byte{9, 8, 7}}}}}
	mu := sa.sessionMu(sess.ID)
	mu.Lock()
	next := sa.reserveBatchLocked(calls)
	mu.Unlock()
	_, err = sa.Run(t.Context(), next)
	require.NoError(t, err)
	data, err := os.ReadFile(capture)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.Len(t, lines, 1, "one native CLI prompt for the complete batch")
	var sent struct {
		Params struct {
			Prompt []struct {
				Text string `json:"text"`
			} `json:"prompt"`
		} `json:"params"`
	}
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &sent))
	require.Len(t, sent.Params.Prompt, 1)
	prompt := sent.Params.Prompt[0].Text
	require.Contains(t, prompt, "first batch prompt")
	require.Contains(t, prompt, "inline note")
	require.Contains(t, prompt, "second batch prompt")
	require.Less(t, strings.Index(prompt, "first batch prompt"), strings.Index(prompt, "second batch prompt"))
	require.Equal(t, 2, strings.Count(prompt, "crush/attachments/"))
	var previous int
	for _, original := range calls {
		for _, attachment := range original.Attachments {
			if !attachment.IsImage() {
				continue
			}
			sum := sha256.Sum256(attachment.Content)
			path := filepath.Join(cache, "crush", "attachments", fmt.Sprintf("%x.png", sum[:16]))
			position := strings.Index(prompt, path)
			require.Greater(t, position, previous, "image paths retain their original order")
			previous = position
			bytes, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, attachment.Content, bytes)
		}
	}
	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 3)
	require.Equal(t, "one", msgs[0].Content().SubmissionID)
	require.Equal(t, "two", msgs[1].Content().SubmissionID)
	for _, id := range []string{"one", "two"} {
		select {
		case event := <-events:
			require.Equal(t, id, event.Payload.SubmissionID)
			require.Equal(t, msgs[2].ID, event.Payload.MessageID)
			require.Equal(t, "done", event.Payload.Text)
			require.False(t, event.Payload.Cancelled)
			require.Empty(t, event.Payload.Error)
		case <-time.After(time.Second):
			t.Fatal("native batch lost an original completion")
		}
	}
}

func TestQueueBatchNativeBinaryAttachmentsKeepBytesAndOrder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	files := []message.Attachment{{MimeType: "application/pdf", FileName: "one.pdf", Content: []byte{0, 1, 255}}, {MimeType: "application/octet-stream", FileName: "two.bin", Content: []byte{8, 9, 0}}}
	text, err := cliPromptWithAttachments("read these", files)
	require.NoError(t, err)
	cache, err := os.UserCacheDir()
	require.NoError(t, err)
	previous := 0
	for _, attachment := range files {
		path := filepath.Join(cache, "crush", "attachments", fmt.Sprintf("%x%s", sha256.Sum256(attachment.Content), filepath.Ext(attachment.FileName)))
		position := strings.Index(text, path)
		require.Greater(t, position, previous)
		previous = position
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, attachment.Content, data)
	}
}

func TestQueueBatchCanceledContinuationReleasesEveryOriginal(t *testing.T) {
	sa, _, broker := newCancelTestAgentWithRunComplete(t)
	events := broker.Subscribe(t.Context())
	releases := make(chan string, 2)
	calls := []SessionAgentCall{{SessionID: "continuation", Prompt: "uncorrelated", queuedSessionRunRelease: func(err error) { require.ErrorIs(t, err, context.Canceled); releases <- "first" }}, {SessionID: "continuation", Prompt: "headless", SubmissionID: "second-id", RunID: "second-run", queuedSessionRunRelease: func(err error) { require.ErrorIs(t, err, context.Canceled); releases <- "second" }}}
	sa.messageQueue.Set("continuation", []SessionAgentCall{{SessionID: "continuation", Prompt: "summary continuation", NotRecallable: true, userMessagesCreated: true, batch: calls}})
	require.Nil(t, sa.RecallQueuedPrompt("continuation"))
	sa.ClearQueue("continuation")
	require.Zero(t, sa.QueuedPrompts("continuation"))
	require.Equal(t, "first", <-releases)
	require.Equal(t, "second", <-releases)
	for _, id := range []string{"", "second-id"} {
		select {
		case event := <-events:
			require.Equal(t, id, event.Payload.SubmissionID)
			require.True(t, event.Payload.Cancelled)
		case <-time.After(time.Second):
			t.Fatal("continuation lost an original cancellation")
		}
	}
}

func TestQueueBatchInterruptSnapshotKeepsLaterArrivalsForNextBatch(t *testing.T) {
	env := testEnv(t)
	large := &batchRecordingModel{finishStreamModel: finishStreamModel{text: "done"}, entered: make(chan int, 10), gates: map[int]chan struct{}{1: make(chan struct{})}}
	sa := testSessionAgent(env, large, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	sa.isSubAgent = true
	sess, err := env.sessions.Create(t.Context(), "interrupt snapshot")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sa.activeRequests.Set(sess.ID, &activeCancel{cancel: cancel})
	for _, text := range []string{"first", "second"} {
		_, err := sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: text})
		require.NoError(t, err)
	}
	sa.Interrupt(sess.ID)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	_, err = sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "later"})
	require.NoError(t, err)
	sa.activeRequests.Del(sess.ID)
	batchWaitTurn(t, large, 1)
	require.Equal(t, []string{"first", "second"}, batchUserTexts(large.snapshot()[0]))
	require.Equal(t, []string{"later"}, sa.QueuedPromptsList(sess.ID))
	close(large.gates[1])
	batchWaitTurn(t, large, 2)
	require.Eventually(t, func() bool { return !sa.IsSessionBusy(sess.ID) }, 5*time.Second, time.Millisecond)
	calls := large.snapshot()
	require.Len(t, calls, 2)
	require.Equal(t, []string{"first", "second", "later"}, batchUserTexts(calls[1]))
}

func TestQueueBatchInterruptIncludesUnconfirmedNativeSteering(t *testing.T) {
	env := testEnv(t)
	large := &batchRecordingModel{finishStreamModel: finishStreamModel{text: "done"}, entered: make(chan int, 10)}
	sa := testSessionAgent(env, large, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	sa.isSubAgent = true
	broker := pubsub.NewBroker[notify.RunComplete]()
	t.Cleanup(broker.Shutdown)
	sa.runComplete = broker
	events := broker.Subscribe(t.Context())
	sess, err := env.sessions.Create(t.Context(), "native pending")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	steps := &cliSteps{running: toolRunningForSteer(), a: sa, ctx: ctx, sessionID: sess.ID}
	sa.steering.Set(sess.ID, steps)
	sa.activeRequests.Set(sess.ID, &activeCancel{cancel: cancel})
	for _, id := range []string{"one", "two"} {
		_, err := sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "repeat", SubmissionID: id, RunID: id})
		require.NoError(t, err)
	}
	text := steps.steer()
	require.Equal(t, "repeat\n\nrepeat", text)
	_, err = sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "third", SubmissionID: "three", RunID: "three"})
	require.NoError(t, err)
	sa.Interrupt(sess.ID)
	require.Empty(t, steps.takeSteered(text))
	steps.returnUnsteered()
	sa.steering.Del(sess.ID)
	sa.activeRequests.Del(sess.ID)
	batchWaitTurn(t, large, 1)
	require.Eventually(t, func() bool { return !sa.IsSessionBusy(sess.ID) }, 5*time.Second, time.Millisecond)
	calls := large.snapshot()
	require.Len(t, calls, 1)
	require.Equal(t, []string{"repeat", "repeat", "third"}, batchUserTexts(calls[0]))
	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 4)
	for i, id := range []string{"one", "two", "three"} {
		require.Equal(t, id, msgs[i].Content().SubmissionID)
		select {
		case event := <-events:
			require.Equal(t, id, event.Payload.SubmissionID)
			require.Equal(t, "done", event.Payload.Text)
		case <-time.After(time.Second):
			t.Fatal("pending native input lost its completion")
		}
	}
}

func TestQueueBatchUsesConfiguredChannelReplyTarget(t *testing.T) {
	reply := &config.MCPChannelReply{MessageParam: "text", User: &config.MCPChannelReplyRoute{Tool: "reply", TargetParam: "target", TargetMeta: "author"}, Group: &config.MCPChannelReplyRoute{Tool: "reply_group", TargetParam: "target", TargetMeta: "room"}}
	require.True(t, sameQueuedChannelRoute(reply, map[string]string{"author": "same", "timestamp": "1"}, map[string]string{"author": "same", "timestamp": "2"}), "non-routing metadata must not split same-target input")
	require.True(t, sameQueuedChannelRoute(reply, map[string]string{"room": "same", "author": "first"}, map[string]string{"room": "same", "author": "second"}), "the configured group route defines the shared destination")
	require.False(t, sameQueuedChannelRoute(reply, map[string]string{"author": "first"}, map[string]string{"author": "second"}))
	require.False(t, sameQueuedChannelRoute(reply, map[string]string{"room": "same"}, map[string]string{"author": "same"}), "different tools represent different reply scopes")
	require.False(t, sameQueuedChannelRoute(nil, map[string]string{"unknown": "first"}, map[string]string{"unknown": "second"}), "unknown routes retain conservative metadata isolation")
}

func TestQueueBatchDispatchDoesNotInheritPreviousHeadlessOwner(t *testing.T) {
	env := testEnv(t)
	large := &batchRecordingModel{finishStreamModel: finishStreamModel{text: "done"}, entered: make(chan int, 10), gates: map[int]chan struct{}{1: make(chan struct{})}}
	sa := testSessionAgent(env, large, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	sa.isSubAgent = true
	broker := pubsub.NewBroker[notify.RunComplete]()
	t.Cleanup(broker.Shutdown)
	sa.runComplete = broker
	events := broker.Subscribe(t.Context())
	sess, err := env.sessions.Create(t.Context(), "headless owner")
	require.NoError(t, err)
	owner := newSessionRun(t.Context(), sess.ID)
	defer owner.cancel()
	done := make(chan error, 1)
	go func() {
		_, err := sa.Run(owner.ctx, SessionAgentCall{SessionID: sess.ID, Prompt: "headless", RunID: "headless", sessionRun: owner, OnComplete: owner.record})
		done <- err
	}()
	batchWaitTurn(t, large, 1)
	_, err = sa.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "local", RunID: "local", SubmissionID: "local-id"})
	require.NoError(t, err)
	close(large.gates[1])
	batchWaitTurn(t, large, 2)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("headless run did not return")
	}
	select {
	case event := <-events:
		require.Equal(t, "local", event.Payload.RunID)
		require.Equal(t, "local-id", event.Payload.SubmissionID)
	case <-time.After(time.Second):
		t.Fatal("queued completion went to the previous headless owner")
	}
	owner.release(nil)
	completion, ok, err := owner.wait()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "headless", completion.RunID)
}

func TestQueueBatchSharedHeadlessOwnerCancellationStopsAllOriginals(t *testing.T) {
	env := testEnv(t)
	large := &batchRecordingModel{finishStreamModel: finishStreamModel{text: "done"}, entered: make(chan int, 10), gates: map[int]chan struct{}{1: make(chan struct{})}}
	sa := testSessionAgent(env, large, &finishStreamModel{text: "title"}, "system").(*sessionAgent)
	sa.isSubAgent = true
	broker := pubsub.NewBroker[notify.RunComplete]()
	t.Cleanup(broker.Shutdown)
	sa.runComplete = broker
	events := broker.Subscribe(t.Context())
	sess, err := env.sessions.Create(t.Context(), "shared headless cancellation")
	require.NoError(t, err)
	owner := newSessionRun(t.Context(), sess.ID)
	defer owner.cancel()
	var calls []SessionAgentCall
	for _, id := range []string{"one", "two"} {
		require.True(t, owner.reserve())
		calls = append(calls, SessionAgentCall{SessionID: sess.ID, Prompt: id, RunID: id, SubmissionID: id, sessionRun: owner, queuedSessionRunRelease: owner.release, OnComplete: func(complete notify.RunComplete) {
			owner.record(complete)
			broker.Publish(pubsub.UpdatedEvent, complete)
		}})
	}
	owner.release(nil)
	mu := sa.sessionMu(sess.ID)
	mu.Lock()
	next := sa.reserveBatchLocked(calls)
	mu.Unlock()
	require.Same(t, owner, next.sessionRun)
	done := make(chan error, 1)
	go func() { _, err := sa.Run(t.Context(), next); done <- err }()
	batchWaitTurn(t, large, 1)
	owner.cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("batch lost shared headless cancellation")
	}
	select {
	case <-owner.done:
	case <-time.After(time.Second):
		t.Fatal("canceled headless batch leaked a reservation")
	}
	for _, id := range []string{"one", "two"} {
		select {
		case event := <-events:
			require.Equal(t, id, event.Payload.SubmissionID)
			require.True(t, event.Payload.Cancelled)
		case <-time.After(time.Second):
			t.Fatal("canceled headless batch lost a terminal")
		}
	}
	require.False(t, sa.IsSessionBusy(sess.ID))
}
