package agent

import (
	"encoding/json"

	"charm.land/fantasy"
)

// currentBuildOf returns the latest build of the model a request started
// with: the same saved choice built again after a refreshed sign-in or key,
// so a retry gets the new credential. It never moves a request onto another
// model: there is no shared model to follow (owner, 2026-10-24: every chat
// owns its model), so a switch made in any chat waits for that chat's next
// turn. An agent made for one model keeps the build it has.
func (a *sessionAgent) currentBuildOf(started Model) fantasy.LanguageModel {
	if started.Model == nil || a.chatModels.build == nil || started.ModelCfg.Provider == "" {
		return started.Model
	}
	choice, err := json.Marshal(started.ModelCfg)
	if err != nil {
		return started.Model
	}
	if fresh, ok := a.modelForChat(choice); ok && fresh.Model != nil {
		return fresh.Model
	}
	return started.Model
}
