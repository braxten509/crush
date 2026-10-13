package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/skills"
)

// SkillRequest is what `crush skill on|off <name>` asks for.
type SkillRequest struct {
	Name string `json:"name"`
	On   bool   `json:"on"`
}

// chatSkill turns a skill on or off for the whole chat on the agent's
// behalf. The agent may turn on any skill that is off, but turn off only
// toggleable ones; a skill the user set to plain on is theirs. Each change
// leaves a notice in the chat for the user; it never reaches the model.
func (h *taskHub) chatSkill(req TaskRequest) (string, error) {
	if req.Session == "" {
		return "", errors.New("sub-agents can't change the chat's skills; mention the skill in your final report instead")
	}
	if h.c == nil || h.c.sessions == nil || h.c.cfg == nil {
		return "", errors.New("this Crush can't change chat skills")
	}
	ctx := context.Background()
	sess, err := h.c.sessions.Get(ctx, req.Session)
	if err != nil {
		return "", fmt.Errorf("unknown session %q", req.Session)
	}
	name := strings.TrimSpace(req.Skill.Name)
	_, active, _ := skills.DiscoverFromConfig(skills.ConfigDiscovery(h.c.cfg))
	var skill *skills.Skill
	for _, s := range active {
		if strings.EqualFold(s.Name, name) {
			skill = s
			break
		}
	}
	if skill == nil {
		return "", fmt.Errorf("no skill named %q is enabled; use a name from the skills list", name)
	}
	var current *session.KeptSkill
	for i := range sess.KeptSkills {
		if sess.KeptSkills[i].ID == skill.SkillFilePath {
			current = &sess.KeptSkills[i]
		}
	}

	if !req.Skill.On {
		switch {
		case current == nil:
			return skill.Name + " is already off for this chat.", nil
		case !current.ByAgent:
			return "", fmt.Errorf("the user turned %s on and did not make it toggleable, so only the user can turn it off", skill.Name)
		}
		if _, err := h.c.sessions.SetKeptSkill(ctx, req.Session, session.KeptSkill{ID: skill.SkillFilePath}, false); err != nil {
			return "", err
		}
		h.skillNotice(ctx, req.Session, skill.Name+" turned off by the agent")
		return skill.Name + " is off for this chat. Stop following it.", nil
	}

	if current != nil {
		return skill.Name + " is already on for this chat.", nil
	}
	if _, err := h.c.sessions.SetKeptSkill(ctx, req.Session, session.KeptSkill{ID: skill.SkillFilePath, Name: skill.Name, ByAgent: true}, true); err != nil {
		return "", err
	}
	h.skillNotice(ctx, req.Session, skill.Name+" turned on by the agent")
	return fmt.Sprintf("%s is on for this chat. Read %s now if you haven't; Crush reminds you of it every turn until it is turned off.", skill.Name, skill.SkillFilePath), nil
}

// skillNotice adds a one-line notice to the chat. It has the system role,
// which never becomes part of the model's context, so it can't break a
// running turn's tool calls.
func (h *taskHub) skillNotice(ctx context.Context, sessionID, text string) {
	if h.c.messages == nil {
		return
	}
	_, _ = h.c.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.System,
		Parts: []message.ContentPart{message.TextContent{Text: text}},
	})
}
