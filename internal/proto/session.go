package proto

// Session represents a session in the proto layer.
//
// IsBusy is computed on read (it is not persisted with the session) and
// reflects whether an agent run is currently in flight for this session.
// It is populated by REST handlers in internal/server/proto.go from the
// workspace's AgentCoordinator. The Session SSE event path does not set
// it, since SSE consumers can compute presence from other agent signals.
//
// AttachedClients counts the number of clients currently viewing this
// session — i.e. entries in the workspace's clients map whose
// currentSessionID equals this session's ID and which have at least one
// live SSE stream. Hold-only clients (streams == 0) do not contribute.
// Like IsBusy, it is computed on read by REST handlers.
//
// HasUnfinishedWork (a helper or background job still owes the chat a
// result) and PendingPrompt (the chat waits for a permission or question
// answer) are computed on read too. Session events leave all four computed
// fields empty, so apps must not overwrite them from events.
//
// ContextBudget and EstimatedUsage are stored. A save sends back the whole
// record it read, so both survive it.
type Session struct {
	ID                string         `json:"id"`
	ParentSessionID   string         `json:"parent_session_id"`
	Title             string         `json:"title"`
	MessageCount      int64          `json:"message_count"`
	PromptTokens      int64          `json:"prompt_tokens"`
	CompletionTokens  int64          `json:"completion_tokens"`
	SummaryMessageID  string         `json:"summary_message_id"`
	Cost              float64        `json:"cost"`
	Todos             []Todo         `json:"todos,omitempty"`
	Channel           string         `json:"channel,omitempty"`
	KeptSkills        []KeptSkill    `json:"kept_skills,omitempty"`
	CreatedAt         int64          `json:"created_at"`
	UpdatedAt         int64          `json:"updated_at"`
	ContextBudget     *ContextBudget `json:"context_budget,omitempty"` // nil when the domain Provider is ""
	EstimatedUsage    bool           `json:"estimated_usage"`
	IsBusy            bool           `json:"is_busy"`
	AttachedClients   int            `json:"attached_clients"`
	HasUnfinishedWork bool           `json:"has_unfinished_work"` // computed on read, like is_busy
	PendingPrompt     string         `json:"pending_prompt"`      // computed on read: "" | "permission" | "question"
}

// ContextBudget is the native agent's compaction boundary for one
// provider and model. Zero tokens means the agent has not reported it.
type ContextBudget struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Tokens    int64  `json:"tokens"`
	Estimated bool   `json:"estimated"`
}

// KeptSkill is a skill turned on for a whole session.
type KeptSkill struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// ByAgent is set when the skill is toggleable: the agent may turn it off.
	ByAgent bool `json:"by_agent,omitempty"`
}

// SetKeptSkillRequest turns a skill on or off for a whole session.
type SetKeptSkillRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	On   bool   `json:"on"`
	// Toggleable lets the agent turn the skill off.
	Toggleable bool `json:"toggleable,omitempty"`
}

// Todo represents a single todo entry on a session in the proto layer.
type Todo struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"active_form"`
}
