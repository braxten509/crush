package cliagent

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// acpForkID is the ID of Grok's fork request.
const acpForkID = "fork"

// grokForkMethod is Grok's ACP extension for a fork. The leading
// underscore is ACP's extension prefix; Grok answers "Method not found" to
// x.ai/session/fork and session/fork, and does not list it in initialize.
const grokForkMethod = "_x.ai/session/fork"

// grokForkParams asks Grok for a new session holding the source session's
// prompts through the cut. Crush's marker counts the prompts the session
// held when a prompt ended; targetPromptIndex is the 0-based index of the
// last prompt kept, so it is the count minus one.
func grokForkParams(p ForkPoint, dir string) (map[string]any, error) {
	count, err := strconv.Atoi(p.Marker)
	if err != nil || count < 1 {
		return nil, fmt.Errorf("%w: prompt count %q", ErrNoFork, p.Marker)
	}
	return map[string]any{
		"sourceSessionId":   p.Native,
		"sourceCwd":         dir,
		"newCwd":            dir,
		"targetPromptIndex": count - 1,
	}, nil
}

// grokForkSession reads the new session's ID from the fork's answer.
func grokForkSession(result json.RawMessage) (string, error) {
	var res struct {
		NewSessionID string `json:"newSessionId"`
	}
	if json.Unmarshal(result, &res) != nil || res.NewSessionID == "" {
		return "", fmt.Errorf("%w: grok's fork named no session", ErrNoFork)
	}
	return res.NewSessionID, nil
}
