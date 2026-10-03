package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/secureentry"
	"github.com/stretchr/testify/require"
)

func TestAskQuestionsWithSecureEntries(t *testing.T) {
	t.Parallel()
	input := `{"questions":[
		{"type":"single_choice","label":"Mode","question":"Which mode?","description":"d","choices":[{"id":"a","label":"A"},{"id":"b","label":"B"}]},
		{"type":"secure_entry","label":"API key","question":"Enter the service key","description":"Used by the service","file":"/tmp/dummy/keys.env","placeholder":"KEY_SLOT","occurrence":2},
		{"type":"secure_entry","question":"Enter the token","description":"d","file":"/tmp/dummy/keys.env"}
	],"confirm_title":"Ready?","confirm_description":"Check."}`
	r, specs, err := askQuestions(json.RawMessage(input))
	require.NoError(t, err)
	require.Len(t, r.Questions, 3)
	require.Equal(t, question.TypeSingleChoice, r.Questions[0].Type)
	require.Equal(t, question.TypeSecureEntry, r.Questions[1].Type)
	require.Equal(t, "API key", r.Questions[1].Label)
	require.Equal(t, "Ready?", r.ConfirmTitle)
	require.Equal(t, secureentry.Spec{File: "/tmp/dummy/keys.env", Label: "API key", Placeholder: "KEY_SLOT", Occurrence: 2}, specs[r.Questions[1].ID])
	require.Equal(t, secureentry.Spec{File: "/tmp/dummy/keys.env", Label: "Enter the token", Occurrence: 1}, specs[r.Questions[2].ID])
	r.Prepare()
	require.NoError(t, r.Validate())

	// The string-encoded form some models send works too.
	encoded, _ := json.Marshal(map[string]any{"questions": `[{"type":"secure_entry","question":"Key?","description":"d","file":"/tmp/dummy/k"}]`})
	r, specs, err = askQuestions(encoded)
	require.NoError(t, err)
	require.Len(t, specs, 1)
	require.Equal(t, question.TypeSecureEntry, r.Questions[0].Type)

	r, specs, err = askQuestions(json.RawMessage(`{"questions":[{"type":"free_text","question":"Name?","description":"d"}]}`))
	require.NoError(t, err)
	require.Nil(t, specs, "ordinary forms keep using the question service")
	require.False(t, r.HasSecureEntry())
}

func TestAskQuestionsRefuseSecureValues(t *testing.T) {
	t.Parallel()
	for name, item := range map[string]string{
		"value":   `{"type":"secure_entry","question":"Key?","description":"d","file":"/tmp/dummy/k","value":"dummy-secret"}`,
		"default": `{"type":"secure_entry","question":"Key?","description":"d","file":"/tmp/dummy/k","default":"dummy-secret"}`,
		"choices": `{"type":"secure_entry","question":"Key?","description":"d","file":"/tmp/dummy/k","choices":[{"id":"dummy-secret","label":"x"},{"id":"b","label":"y"}]}`,
		"no file": `{"type":"secure_entry","question":"Key?","description":"d"}`,
	} {
		_, _, err := askQuestions(json.RawMessage(`{"questions":[{"type":"free_text","question":"Name?","description":"d"},` + item + `]}`))
		require.Error(t, err, name)
		require.Contains(t, err.Error(), "question 2", name)
		require.NotContains(t, err.Error(), "dummy-secret", name)
	}
}

func TestSecureAskResultCarriesOnlyStatuses(t *testing.T) {
	t.Parallel()
	questions := []question.Question{
		{ID: "name", Type: question.TypeFreeText, Text: "Your name?"},
		{ID: "key", Type: question.TypeSecureEntry, Label: "API key", Text: "Enter the key"},
		{ID: "token", Type: question.TypeSecureEntry, Text: "Enter the token"},
	}
	answers := []question.Answer{{QuestionID: "name", FillInText: "Ada"}, {QuestionID: "key"}, {QuestionID: "token"}}
	out := formatAskAnswers(answers, questions)
	require.Contains(t, out, "Q1: Your name?")
	require.Contains(t, out, "User provided: Ada")
	require.NotContains(t, out, "Enter the key")
	statuses := secureEntryStatuses(questions, map[string]string{"key": secureentry.StatusSaved, "token": "anything else"})
	require.Contains(t, statuses, "- API key: saved")
	require.Contains(t, statuses, "- Enter the token: cancelled", "only fixed statuses come back")
	require.Empty(t, formatAskAnswers(answers[1:], questions[1:]))
}

func TestSecureAskDeliversAnswersAndStatusesTogether(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.env")
	require.NoError(t, os.WriteFile(path, []byte("KEY=KEY_SLOT\n"), 0o644))
	forms := make(chan *secureentry.Form, 1)
	detach := secureentry.AttachForms(func(f *secureentry.Form) { forms <- f })
	defer detach()
	prompts := make(chan string, 1)
	c := sessionRunTestCoordinator(t, func(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		prompts <- call.Prompt
		return fakeSessionReply(call, "answers received"), nil
	})
	c.questions = question.NewService()
	sess, err := c.sessions.Create(t.Context(), "session")
	require.NoError(t, err)
	ask := []byte(fmt.Sprintf(`{"questions":[{"type":"free_text","question":"Name?","description":"d"},{"type":"secure_entry","label":"API key","question":"Key?","description":"d","file":%q,"placeholder":"KEY_SLOT"}]}`, path))

	err = c.tasks.ask(TaskRequest{Session: sess.ID, Ask: ask})
	require.ErrorContains(t, err, "local interactive Crush terminal", "a non-interactive run can't show secure entries")
	c.interactive = true
	require.NoError(t, c.tasks.ask(TaskRequest{Session: sess.ID, Ask: ask}))
	require.ErrorContains(t, c.tasks.ask(TaskRequest{Session: sess.ID, Ask: ask}), "still open")
	_, pending := c.questions.(interface {
		Pending() (question.Request, bool)
	}).Pending()
	require.False(t, pending, "the shared question service never sees the form")

	form := <-forms
	ids := []string{form.Request.Questions[0].ID, form.Request.Questions[1].ID}
	const value = "dummy-secret-value"
	answers := []question.Answer{{QuestionID: ids[0], FillInText: "Ada"}, {QuestionID: ids[1]}}
	require.NoError(t, form.Submit(answers, map[string][]byte{ids[1]: []byte(value)}))
	var prompt string
	select {
	case prompt = <-prompts:
	case <-time.After(5 * time.Second):
		t.Fatal("the answers never reached the session")
	}
	name, status, ok := ParseTaskNotification(prompt)
	require.True(t, ok)
	require.Equal(t, AskName, name)
	require.Equal(t, AskAnswered, status)
	require.Contains(t, prompt, "User provided: Ada")
	require.Contains(t, prompt, "- API key: saved")
	require.NotContains(t, prompt, value)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "KEY="+value+"\n", string(data))
}

func TestInstructionsDescribeSecureQuestions(t *testing.T) {
	t.Parallel()
	h := &taskHub{c: &coordinator{cfg: config.NewTestStore(&config.Config{Providers: csync.NewMap[string, config.ProviderConfig]()})}}
	text := h.instructions()
	require.Contains(t, text, `"type":"secure_entry"`)
	require.Contains(t, text, "never include a value")
}
