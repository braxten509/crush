package proto

import "time"

// EngineInfo describes a running Crush engine. GET /v1/engine answers
// it, and `crush engine list` prints one per live engine. The legacy
// shared server answers it too, with an empty DataDir.
type EngineInfo struct {
	Protocol      int       `json:"protocol"`
	Version       string    `json:"version"`
	PID           int       `json:"pid"`
	DataDir       string    `json:"data_dir"` // canonical; "" on the legacy server
	Address       string    `json:"address"`
	StartedAt     time.Time `json:"started_at"`
	WorkspaceID   string    `json:"workspace_id,omitempty"`
	WorkspacePath string    `json:"workspace_path,omitempty"` // the folder the AI works in
	Apps          int       `json:"apps"`                     // registered clients
	Closing       bool      `json:"closing"`                  // the engine decided to exit
}

// PromptPending is the prompt_pending event: whether a chat waits for a
// permission or question answer. The engine sends it to every app of
// the workspace when the state changes; it never reaches the phone.
type PromptPending struct {
	SessionID string `json:"session_id"` // the root chat (a helper's prompt counts for its parent chat)
	Kind      string `json:"kind"`       // "permission" | "question"
	Pending   bool   `json:"pending"`
}
