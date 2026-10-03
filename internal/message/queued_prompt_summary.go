package message

// QueuedPromptSummary identifies a pending delivery without copying attachments.
type QueuedPromptSummary struct {
	Prompt       string `json:"prompt"`
	SubmissionID string `json:"submission_id,omitempty"`
}
