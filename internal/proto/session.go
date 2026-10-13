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
type Session struct {
	ID               string      `json:"id"`
	ParentSessionID  string      `json:"parent_session_id"`
	Title            string      `json:"title"`
	MessageCount     int64       `json:"message_count"`
	PromptTokens     int64       `json:"prompt_tokens"`
	CompletionTokens int64       `json:"completion_tokens"`
	SummaryMessageID string      `json:"summary_message_id"`
	Cost             float64     `json:"cost"`
	Todos            []Todo      `json:"todos,omitempty"`
	Channel          string      `json:"channel,omitempty"`
	KeptSkills       []KeptSkill `json:"kept_skills,omitempty"`
	CreatedAt        int64       `json:"created_at"`
	UpdatedAt        int64       `json:"updated_at"`
	IsBusy           bool        `json:"is_busy"`
	AttachedClients  int         `json:"attached_clients"`
}

// KeptSkill is a skill turned on for a whole session.
type KeptSkill struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// ByAgent is set when the agent turned the skill on.
	ByAgent bool `json:"by_agent,omitempty"`
}

// SetKeptSkillRequest turns a skill on or off for a whole session.
type SetKeptSkillRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	On   bool   `json:"on"`
}

// Todo represents a single todo entry on a session in the proto layer.
type Todo struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"active_form"`
}
