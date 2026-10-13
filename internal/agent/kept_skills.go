package agent

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/skills"
)

// Kept skills are skills turned on for a whole chat, by the user or by the
// agent (`crush skill`). API models
// rebuild their context from the database every turn, so they get the full
// text in the system prompt each turn. Agent CLIs keep their own history, so
// they get the full text once (and again after a fresh native session or a
// native compaction) and a one-line reminder on every other turn.

// keptSkillsSent records, per chat and agent CLI, which kept skills that CLI
// has been told about, keyed by skill ID.
var keptSkillsSent = struct {
	sync.Mutex
	m map[string]map[string]*keptSkillSent
}{m: map[string]map[string]*keptSkillSent{}}

type keptSkillSent struct {
	name string
	// full is true when the CLI has the full text since its last
	// compaction.
	full bool
}

func keptSkillsKey(sessionID, cli string) string { return sessionID + "\x00" + cli }

// forgetKeptSkillsSent makes the next turn of this chat resend every kept
// skill in full, for every agent CLI, after the agent's memory was compacted.
// The CLIs still know the skills were on, so a skill turned off since then is
// still reported as off.
func forgetKeptSkillsSent(sessionID string) {
	keptSkillsSent.Lock()
	defer keptSkillsSent.Unlock()
	prefix := sessionID + "\x00"
	for key, sent := range keptSkillsSent.m {
		if strings.HasPrefix(key, prefix) {
			for _, skill := range sent {
				skill.full = false
			}
		}
	}
}

// sessionTurnedOffSkills lists the skills that were on in this chat and are
// now off.
func (a *sessionAgent) sessionTurnedOffSkills(ctx context.Context, sessionID string) []session.KeptSkill {
	if a.sessions == nil {
		return nil
	}
	off, err := a.sessions.TurnedOffSkills(ctx, sessionID)
	if err != nil {
		slog.Warn("Failed to read turned-off skills", "session_id", sessionID, "error", err)
	}
	return off
}

// sessionKeptSkills resolves the chat's kept skills against the skills that
// are currently enabled. Disabled or removed skills are left out.
func (a *sessionAgent) sessionKeptSkills(ctx context.Context, sessionID string, native bool) []*skills.Skill {
	if a.sessions == nil || a.cfg == nil || a.cfg.Config().Options == nil {
		return nil
	}
	sess, err := a.sessions.Get(ctx, sessionID)
	if err != nil || len(sess.KeptSkills) == 0 {
		return nil
	}
	return resolveKeptSkills(a.cfg, sess.KeptSkills, native)
}

func resolveKeptSkills(store *config.ConfigStore, kept []session.KeptSkill, native bool) []*skills.Skill {
	_, active, _ := skills.DiscoverFromConfig(skills.ConfigDiscovery(store))
	var out []*skills.Skill
	for _, k := range kept {
		skill, err := skills.FindEffective(active, k.ID)
		if err != nil {
			continue
		}
		if native {
			readable, err := skills.NativeLocation(skill)
			if err != nil {
				slog.Warn("Cannot prepare kept builtin skill for native CLI", "skill", skill.Name, "error", err)
				continue
			}
			skill = readable
		}
		out = append(out, skill)
	}
	return out
}

const keptSkillsIntro = "These skills are on for this whole chat, turned on by the user or by you. Keep following them on every turn until they are turned off. This is a standing setting, not a new request."

// apiKeptSkillsText is the system prompt block for API models: every kept
// skill in full, every turn.
func apiKeptSkillsText(kept []*skills.Skill) string {
	if len(kept) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<crush_kept_skills>\n" + keptSkillsIntro + "\n")
	for _, skill := range kept {
		writeKeptSkillFull(&b, skill)
	}
	b.WriteString("</crush_kept_skills>")
	return b.String()
}

// cliKeptSkillsText is the prompt block for an agent CLI turn: the full text
// of each kept skill the CLI hasn't had since its last compaction (or at all,
// when fresh is true), a one-line reminder for the rest, and a line for each
// skill turned off since the last turn, since the CLI's own history still has
// its text. turnedOff is the chat's saved list of skills turned off; it is only
// read when Crush has no record of this CLI, such as after a restart, so the
// resumed CLI is still told. It is empty when there is nothing to say.
func cliKeptSkillsText(sessionID, cli string, kept []*skills.Skill, turnedOff func() []session.KeptSkill, fresh bool) string {
	keptSkillsSent.Lock()
	defer keptSkillsSent.Unlock()
	key := keptSkillsKey(sessionID, cli)
	sent := keptSkillsSent.m[key]
	if sent == nil || fresh {
		// A fresh native session has none of the earlier history.
		sent = map[string]*keptSkillSent{}
		keptSkillsSent.m[key] = sent
		if !fresh && turnedOff != nil {
			for _, skill := range turnedOff() {
				sent[skill.ID] = &keptSkillSent{name: skill.Name}
			}
		}
	}

	on := make(map[string]bool, len(kept))
	for _, skill := range kept {
		on[skill.SkillFilePath] = true
	}
	var off []string
	for id, skill := range sent {
		if !on[id] {
			off = append(off, skill.name)
			delete(sent, id)
		}
	}
	slices.Sort(off)
	if len(kept) == 0 && len(off) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("<crush_kept_skills>\n")
	if len(kept) > 0 {
		b.WriteString(keptSkillsIntro + "\n")
	}
	for _, skill := range kept {
		if prior := sent[skill.SkillFilePath]; prior != nil && prior.full {
			b.WriteString("- " + skill.Name + " is on (" + skill.SkillFilePath + "). Its full text was given earlier in this chat; reread that file if you no longer have it.\n")
			continue
		}
		writeKeptSkillFull(&b, skill)
		sent[skill.SkillFilePath] = &keptSkillSent{name: skill.Name, full: true}
	}
	for _, name := range off {
		b.WriteString("- " + name + " is off for this chat now. Stop following it.\n")
	}
	b.WriteString("</crush_kept_skills>")
	return b.String()
}

func writeKeptSkillFull(b *strings.Builder, skill *skills.Skill) {
	b.WriteString("<skill name=\"" + skill.Name + "\" location=\"" + skill.SkillFilePath + "\">\n")
	b.WriteString(strings.TrimSpace(skill.Instructions))
	b.WriteString("\n</skill>\n")
}
