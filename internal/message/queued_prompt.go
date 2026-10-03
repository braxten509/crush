package message

// QueuedPrompt is a prompt withdrawn before an agent took ownership of it.
type QueuedPrompt struct {
	Prompt       string
	SubmissionID string
	Attachments  []Attachment
}
