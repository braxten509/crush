package proto

import (
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/session"
)

// This file and the other convert_*.go files hold every conversion
// between domain types and their wire representation. The server
// converts domain values to proto before encoding them; the client
// workspace converts them back. Keeping both directions side by side
// stops the pairs from drifting apart.

// SessionFromDomain converts a domain session into its wire form. The
// computed-on-read fields (IsBusy, AttachedClients, HasUnfinishedWork,
// PendingPrompt) are left zero for the caller to fill in.
func SessionFromDomain(s session.Session) Session {
	return Session{
		ID:               s.ID,
		ParentSessionID:  s.ParentSessionID,
		Title:            s.Title,
		SummaryMessageID: s.SummaryMessageID,
		MessageCount:     s.MessageCount,
		PromptTokens:     s.PromptTokens,
		CompletionTokens: s.CompletionTokens,
		Cost:             s.Cost,
		Todos:            TodosFromDomain(s.Todos),
		Channel:          s.Channel,
		KeptSkills:       KeptSkillsFromDomain(s.KeptSkills),
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.UpdatedAt,
		ContextBudget:    ContextBudgetFromDomain(s.ContextBudget),
		EstimatedUsage:   s.EstimatedUsage,
	}
}

// SessionToDomain converts a wire session into the domain
// session.Session. Fields that exist only on the wire (computed-on-read
// signals like IsBusy and AttachedClients) are intentionally dropped:
// session.Session models persisted state, not transient runtime signals.
// UI features that need those signals should read them from the proto
// payload before this conversion runs.
func SessionToDomain(s Session) session.Session {
	return session.Session{
		ID:               s.ID,
		ParentSessionID:  s.ParentSessionID,
		Title:            s.Title,
		SummaryMessageID: s.SummaryMessageID,
		MessageCount:     s.MessageCount,
		PromptTokens:     s.PromptTokens,
		CompletionTokens: s.CompletionTokens,
		Cost:             s.Cost,
		Todos:            TodosToDomain(s.Todos),
		Channel:          s.Channel,
		KeptSkills:       KeptSkillsToDomain(s.KeptSkills),
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.UpdatedAt,
		ContextBudget:    ContextBudgetToDomain(s.ContextBudget),
		EstimatedUsage:   s.EstimatedUsage,
	}
}

// ContextBudgetFromDomain converts a session's context budget into its
// wire form. It returns nil when the budget has no provider, the state
// session.Save treats as "no budget".
func ContextBudgetFromDomain(b session.ContextBudget) *ContextBudget {
	if b.Provider == "" {
		return nil
	}
	return &ContextBudget{
		Provider:  b.Provider,
		Model:     b.Model,
		Tokens:    b.Tokens,
		Estimated: b.Estimated,
	}
}

// ContextBudgetToDomain converts a wire context budget back. Nil maps to
// the zero budget.
func ContextBudgetToDomain(b *ContextBudget) session.ContextBudget {
	if b == nil {
		return session.ContextBudget{}
	}
	return session.ContextBudget{
		Provider:  b.Provider,
		Model:     b.Model,
		Tokens:    b.Tokens,
		Estimated: b.Estimated,
	}
}

// SessionsToDomain converts a slice of wire sessions.
func SessionsToDomain(in []Session) []session.Session {
	out := make([]session.Session, len(in))
	for i, s := range in {
		out[i] = SessionToDomain(s)
	}
	return out
}

// TodosFromDomain converts session todos into their wire form. It
// returns nil for an empty list so the field is omitted from JSON.
func TodosFromDomain(todos []session.Todo) []Todo {
	if len(todos) == 0 {
		return nil
	}
	out := make([]Todo, len(todos))
	for i, t := range todos {
		out[i] = Todo{
			Content:    t.Content,
			Status:     string(t.Status),
			ActiveForm: t.ActiveForm,
		}
	}
	return out
}

// TodosToDomain converts wire todos back into session todos.
func TodosToDomain(todos []Todo) []session.Todo {
	if len(todos) == 0 {
		return nil
	}
	out := make([]session.Todo, len(todos))
	for i, t := range todos {
		out[i] = session.Todo{
			Content:    t.Content,
			Status:     session.TodoStatus(t.Status),
			ActiveForm: t.ActiveForm,
		}
	}
	return out
}

// KeptSkillsFromDomain converts a session's kept skills into their wire
// form.
func KeptSkillsFromDomain(kept []session.KeptSkill) []KeptSkill {
	if len(kept) == 0 {
		return nil
	}
	out := make([]KeptSkill, len(kept))
	for i, skill := range kept {
		out[i] = KeptSkill{ID: skill.ID, Name: skill.Name, ByAgent: skill.ByAgent}
	}
	return out
}

// KeptSkillsToDomain converts wire kept skills back into session kept
// skills.
func KeptSkillsToDomain(kept []KeptSkill) []session.KeptSkill {
	if len(kept) == 0 {
		return nil
	}
	out := make([]session.KeptSkill, len(kept))
	for i, skill := range kept {
		out[i] = session.KeptSkill{ID: skill.ID, Name: skill.Name, ByAgent: skill.ByAgent}
	}
	return out
}

// FileFromDomain converts a history file into its wire form.
func FileFromDomain(f history.File) File {
	return File{
		ID:        f.ID,
		SessionID: f.SessionID,
		Path:      f.Path,
		Content:   f.Content,
		Version:   f.Version,
		CreatedAt: f.CreatedAt,
		UpdatedAt: f.UpdatedAt,
	}
}

// FileToDomain converts a wire file back into a history file.
func FileToDomain(f File) history.File {
	return history.File{
		ID:        f.ID,
		SessionID: f.SessionID,
		Path:      f.Path,
		Content:   f.Content,
		Version:   f.Version,
		CreatedAt: f.CreatedAt,
		UpdatedAt: f.UpdatedAt,
	}
}

// FilesFromDomain converts a slice of history files.
func FilesFromDomain(files []history.File) []File {
	out := make([]File, len(files))
	for i, f := range files {
		out[i] = FileFromDomain(f)
	}
	return out
}

// FilesToDomain converts a slice of wire files.
func FilesToDomain(files []File) []history.File {
	out := make([]history.File, len(files))
	for i, f := range files {
		out[i] = FileToDomain(f)
	}
	return out
}
