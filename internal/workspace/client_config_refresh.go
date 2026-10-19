package workspace

import "context"

// ConfigRefreshedMsg tells the UI that the client refreshed its cached
// config after a config_changed event, so it can re-apply what it copied at
// start (desktop design 5-6, G-3).
type ConfigRefreshedMsg struct{}

// AgentMainName names the engine's main agent ("coder" or "plan"; "" while
// no coordinator runs), read from GET …/agent.
func (w *ClientWorkspace) AgentMainName() (string, error) {
	info, err := w.client.GetAgentInfo(context.Background(), w.workspaceID())
	if err != nil {
		return "", err
	}
	return info.Agent, nil
}
