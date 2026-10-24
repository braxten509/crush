package agent

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/secureentry"
)

// Answers from `crush ask` come back as a task result with this name and
// one of these statuses.
const (
	AskName      = "Questions"
	AskAnswered  = "answered"
	AskCancelled = "cancelled"
)

// ask shows an agent's questions in Crush's question form. The answers
// reach the session like a sub-agent's result, so the agent doesn't hold
// its turn open waiting for the user.
func (h *taskHub) ask(req TaskRequest) error {
	if h.c.questions == nil {
		return errors.New("this Crush can't show questions")
	}
	r, secure, err := askQuestions(req.Ask)
	if err != nil {
		return err
	}
	r.SessionID = req.Session
	r.Prepare()
	if err := r.Validate(); err != nil {
		return err
	}
	ctx, release := h.reserveFollowUp(req.Session)
	// The form outlives the turn that opened it: the agent ends its turn
	// and the user answers later, when ctx may be gone. Waiting and
	// delivering on ctx would close the form (or drop the answers) first.
	waitCtx := context.WithoutCancel(ctx)
	started := false
	defer func() {
		if !started {
			release()
		}
	}()
	// A [WithNonInteractive] prompt's run gets no answers, as on a
	// non-interactive coordinator.
	interactive := h.c.interactiveTurn(ctx)
	if secure != nil && !interactive {
		return errors.New("secure_entry questions need the local interactive Crush terminal, which this run doesn't have")
	}
	if _, err := h.c.sessions.Get(ctx, req.Session); err != nil {
		return fmt.Errorf("unknown session %q", req.Session)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.asking {
		return errors.New("other questions are still open; wait for their answers first")
	}
	// Forms with secure fields go only to the local terminal, never the
	// question service that phones and servers see.
	var form *secureentry.Form
	if secure != nil {
		if form, err = secureentry.OpenForm(req.Session, r, secure); err != nil {
			return err
		}
	}
	h.asking = true
	h.askSession = req.Session
	started = true
	go func() {
		defer release()
		var answers []question.Answer
		var statuses map[string]string
		var err error
		switch {
		case form != nil:
			var result secureentry.FormResult
			result = <-form.Result()
			answers, statuses = result.Answers, result.Statuses
			if result.Cancelled {
				err = question.ErrCancelled
			}
		case interactive:
			answers, err = h.c.questions.Ask(waitCtx, r)
		default:
			err = question.ErrCancelled
		}
		h.mu.Lock()
		h.asking = false
		h.mu.Unlock()
		status, out := AskAnswered, ""
		switch {
		case errors.Is(err, question.ErrCancelled):
			status, out = AskCancelled, "The user closed the questions without answering."
			if !interactive {
				out = "Questions were cancelled because this non-interactive run cannot receive user answers. Continue without assuming an answer."
			}
		case err != nil:
			status, out = string(TaskFailed), "Error: "+err.Error()
		default:
			out = formatAskAnswers(answers, r.Questions)
		}
		if form != nil {
			out = strings.TrimSpace(out + "\n\n" + secureEntryStatuses(r.Questions, statuses))
		}
		msg := taskResult{Name: AskName, Status: status, Body: out}.String()
		accept := h.acceptHandoff(req.Session, func() {
			if h.askSession == req.Session {
				h.askSession = ""
			}
		})
		defer accept.Close()
		// The chat must hear the outcome even when the turn that opened the
		// form has ended: by the time the user answers, ctx is gone, and
		// RunAccepted would drop the answer with it.
		if _, err := h.c.RunAccepted(context.WithoutCancel(ctx), accept, req.Session, msg); err != nil {
			slog.Error("Failed to hand answers to their session", "error", err)
		}
	}()
	return nil
}

// secureAskItem is the only shape a secure_entry question may have:
// metadata, never a value.
type secureAskItem struct {
	Type        string `json:"type"`
	Label       string `json:"label,omitempty"`
	Question    string `json:"question"`
	Description string `json:"description"`
	File        string `json:"file"`
	Placeholder string `json:"placeholder,omitempty"`
	Occurrence  int    `json:"occurrence,omitempty"`
}

// askQuestions turns `crush ask` input into a request, plus the destination
// of each secure_entry question by question ID (nil when there are none).
func askQuestions(data json.RawMessage) (question.Request, map[string]secureentry.Spec, error) {
	var params tools.QuestionParams
	if err := json.Unmarshal(data, &params); err != nil {
		return question.Request{}, nil, fmt.Errorf("bad questions JSON: %w", err)
	}
	// Check the ordinary questions in place, so errors keep their numbers.
	checked := params
	checked.Questions = slices.Clone(params.Questions)
	var secureAt []int
	for i, item := range checked.Questions {
		if item.Type == string(question.TypeSecureEntry) {
			secureAt = append(secureAt, i)
			checked.Questions[i].Type = string(question.TypeFreeText)
		}
	}
	qs, err := tools.BuildQuestions(checked)
	if err != nil {
		return question.Request{}, nil, err
	}
	r := question.Request{Questions: qs, ConfirmTitle: params.ConfirmTitle, ConfirmDescription: params.ConfirmDescription}
	if len(secureAt) == 0 {
		return r, nil, nil
	}
	raw, err := rawAskQuestions(data)
	if err != nil || len(raw) != len(qs) {
		return question.Request{}, nil, errors.New("bad questions JSON: questions must be an array")
	}
	specs := make(map[string]secureentry.Spec, len(secureAt))
	for _, i := range secureAt {
		var item secureAskItem
		decoder := json.NewDecoder(bytes.NewReader(raw[i]))
		decoder.DisallowUnknownFields()
		// The decoder's own error could quote the input; report fixed text.
		if decoder.Decode(&item) != nil {
			return question.Request{}, nil, fmt.Errorf("question %d: secure_entry takes only type, label, question, description, file, placeholder and occurrence; never put a value in the JSON", i+1)
		}
		if item.File == "" {
			return question.Request{}, nil, fmt.Errorf("question %d: secure_entry needs the absolute path of a prepared file in \"file\"", i+1)
		}
		id := uuid.NewString()
		qs[i] = question.Question{ID: id, Type: question.TypeSecureEntry, Label: item.Label, Text: item.Question, Description: item.Description}
		specs[id] = secureentry.Spec{File: item.File, Label: cmp.Or(item.Label, item.Question), Placeholder: item.Placeholder, Occurrence: cmp.Or(item.Occurrence, 1)}
	}
	return r, specs, nil
}

// rawAskQuestions returns each question's JSON, accepting the questions as
// an array or as a string-encoded array, like tools.QuestionParams.
func rawAskQuestions(data json.RawMessage) ([]json.RawMessage, error) {
	var envelope struct {
		Questions json.RawMessage `json:"questions"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	var raw []json.RawMessage
	if json.Unmarshal(envelope.Questions, &raw) == nil {
		return raw, nil
	}
	var encoded string
	if err := json.Unmarshal(envelope.Questions, &encoded); err != nil {
		return nil, err
	}
	err := json.Unmarshal([]byte(strings.TrimSpace(encoded)), &raw)
	return raw, err
}

// formatAskAnswers writes the ordinary answers for the agent. Secure
// questions are left out: only their status is reported.
func formatAskAnswers(answers []question.Answer, questions []question.Question) string {
	var ordinary []question.Question
	var ordinaryAnswers []question.Answer
	for _, q := range questions {
		if q.Type == question.TypeSecureEntry {
			continue
		}
		answer := question.Answer{QuestionID: q.ID}
		if i := slices.IndexFunc(answers, func(a question.Answer) bool { return a.QuestionID == q.ID }); i >= 0 {
			answer = answers[i]
		}
		ordinary = append(ordinary, q)
		ordinaryAnswers = append(ordinaryAnswers, answer)
	}
	if len(ordinary) == 0 {
		return ""
	}
	return tools.FormatAnswers(ordinaryAnswers, ordinary)
}

// secureEntryStatuses reports each secure question as saved or cancelled,
// the only results secure entry ever returns.
func secureEntryStatuses(questions []question.Question, statuses map[string]string) string {
	var b strings.Builder
	b.WriteString("Secure entries (no value is returned; do not read, print or send the destination files):")
	for _, q := range questions {
		if q.Type != question.TypeSecureEntry {
			continue
		}
		status := secureentry.StatusCancelled
		if statuses[q.ID] == secureentry.StatusSaved {
			status = secureentry.StatusSaved
		}
		fmt.Fprintf(&b, "\n- %s: %s", cmp.Or(q.Label, q.Text), status)
	}
	return b.String()
}

// secureEntryAsk turns `crush secure-entry` flags into the input of a
// one-question form, so the secret goes through the same inline form as
// secure_entry questions from `crush ask`.
func secureEntryAsk(spec secureentry.Spec) (json.RawMessage, error) {
	label := cmp.Or(strings.TrimSpace(spec.Label), "Secret")
	item := secureAskItem{
		Type:        string(question.TypeSecureEntry),
		Label:       label,
		Question:    "Enter the " + label,
		Description: "Typed into a masked field and saved straight into a local file. The agent never sees it.",
		File:        spec.File,
		Placeholder: spec.Placeholder,
		Occurrence:  spec.Occurrence,
	}
	return json.Marshal(map[string][]secureAskItem{"questions": {item}})
}
