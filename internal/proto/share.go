package proto

// Phone sharing (/remote) and secure entry through the Crush engine
// (engine-design.md 5.7 and 5.8).

// RemoteStart starts the phone share of the chat the calling app shows.
type RemoteStart struct {
	// SessionID is the chat; "" for the app's new chat, which the share
	// moves to once the app shows a chat.
	SessionID string `json:"session_id,omitempty"`
	// LaunchID is the window's CRUSH_REMOTE_LAUNCH value, if the launcher
	// opened it.
	LaunchID string `json:"launch_id,omitempty"`
}

// RemoteStop stops a chat's phone share, whichever app started it.
type RemoteStop struct {
	// SessionID is the chat; "" for the calling app's new chat.
	SessionID string `json:"session_id,omitempty"`
}

// RemoteSetup is a setup step that keeps sharing from starting
// (remote.SetupInfo), as the terminal's setup dialog shows it.
type RemoteSetup struct {
	// State is the remote.SetupState: "ready", "missing", "unavailable",
	// "login", "stopped", "approval", "waiting" or "unsupported".
	State  string `json:"state"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	// Action fixes it with `crush remote setup <action>`.
	Action string `json:"action,omitempty"`
	// Button is the main button's label, such as "Install Tailscale".
	Button string `json:"button,omitempty"`
}

// RemoteStatus describes a chat's phone share.
type RemoteStatus struct {
	// State is "off", "on" or "setup".
	State string `json:"state"`
	// SessionID is the shared chat; "" for the receiving app's new chat.
	SessionID string       `json:"session_id"`
	Host      string       `json:"host,omitempty"`
	Port      int          `json:"port,omitempty"`
	Phones    []string     `json:"phones,omitempty"`
	Setup     *RemoteSetup `json:"setup,omitempty"`
	// Error is a failed start that is not a setup issue.
	Error string `json:"error,omitempty"`
}

// RemotePresence is what an app's window shows.
type RemotePresence struct {
	// SessionID is the chat the window shows; "" on the start screen.
	SessionID string `json:"session_id"`
	Focused   bool   `json:"focused"`
	Plan      bool   `json:"plan"`
}

// RemoteAction is a phone action that only a window showing the shared chat
// can run, because it changes the window's own state.
type RemoteAction struct {
	ActionID string `json:"action_id"`
	// Kind is "new_chat_send", "yolo" or "mode".
	Kind string `json:"kind"`
	// SessionID is the chat the phone saw; "" for new_chat_send.
	SessionID string `json:"session_id"`
	// Text is new_chat_send's prompt, which the engine sends once the chat
	// exists; the window may show it at once.
	Text string `json:"text,omitempty"`
	// Uploads are new_chat_send's attachments, which the engine keeps for
	// this action.
	Uploads []RemoteUpload `json:"uploads,omitempty"`
}

// RemoteUpload refers to a phone attachment. The bytes stay in the engine.
type RemoteUpload struct {
	UploadID string `json:"upload_id"`
	Name     string `json:"name"`
	Mime     string `json:"mime"`
	Size     int    `json:"size"`
}

// RemoteActionDone is the window's outcome of a RemoteAction.
type RemoteActionDone struct {
	// Error is "" when the action is done.
	Error string `json:"error,omitempty"`
	// SessionID is, for new_chat_send, the chat the window created and now
	// shows.
	SessionID string `json:"session_id,omitempty"`
}

// SecureField is one secure question of a form. Only its label travels:
// the destination file stays in the engine.
type SecureField struct {
	QuestionID string `json:"question_id"`
	// Label is the field's name, e.g. "API key".
	Label string `json:"label"`
	// Status is "open", "saved" or "unavailable".
	Status string `json:"status"`
}

// SecureForm is a question form with secure fields, sent to the apps that
// view its chat.
type SecureForm struct {
	FormID    string `json:"form_id"`
	SessionID string `json:"session_id"`
	// Request holds every question; secure questions are metadata.
	Request QuestionRequest `json:"request"`
	Fields  []SecureField   `json:"fields"`
}

// SecureFormClosed tells a recipient that a form closed.
type SecureFormClosed struct {
	FormID string `json:"form_id"`
	// Reason is "submitted", "cancelled", "answered_elsewhere" or "closed".
	Reason string `json:"reason"`
}

// SecureSubmit answers a secure form.
type SecureSubmit struct {
	// Answers holds the ordinary questions' answers.
	Answers []QuestionAnswer `json:"answers"`
	// Values maps each secure question ID to its value.
	Values map[string]string `json:"values"`
}
