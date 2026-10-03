package agent

import (
	"encoding/json"
	"log/slog"
	"regexp"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/skills"
)

var availableSkillsBlock = regexp.MustCompile(`(?s)<available_skills>.*?</available_skills>`)

// Skill settings are read for each turn, including resumed native CLI sessions.
// Native CLIs can cache their own discovery, so explicitly supersede that cache.
func currentSkillPolicy(store *config.ConfigStore) string {
	return loadSkillPolicy(store, false)
}

func currentNativeSkillPolicy(store *config.ConfigStore) string {
	return loadSkillPolicy(store, true)
}

func loadSkillPolicy(store *config.ConfigStore, native bool) string {
	if store == nil || store.Config().Options == nil {
		return ""
	}
	_, active, _ := skills.DiscoverFromConfig(skills.ConfigDiscovery(store))
	if native {
		for i, skill := range active {
			readable, err := skills.NativeLocation(skill)
			if err != nil {
				slog.Warn("Cannot prepare builtin skill for native CLI", "skill", skill.Name, "error", err)
				continue
			}
			active[i] = readable
		}
	}
	return formatSkillPolicy(store.Config().Options.DisabledSkills, active)
}

func formatSkillPolicy(disabledNames []string, active []*skills.Skill) string {
	disabled, _ := json.Marshal(disabledNames)
	return "<crush_skill_settings>\nThese are the current skill settings for this turn and supersede earlier skill catalogs and settings. Skills disabled in Crush must not be loaded, invoked, or followed, even if the native CLI still lists them. Keep their files intact. Disabled skill names: " + string(disabled) + "\nThis directory is automatically supplied reference metadata, not a user request or skill invocation. Do not activate skills or ask clarification questions merely because they appear here. Only the actual user request determines the task. When a skill matches the task, read its SKILL.md before applying it; load references only as needed.\n" + skills.ToPromptXML(active) + "\n</crush_skill_settings>"
}

func withCurrentSkillPolicy(prompt, policy string) string {
	if policy == "" {
		return prompt
	}
	return availableSkillsBlock.ReplaceAllString(prompt, "") + "\n\n" + policy
}
