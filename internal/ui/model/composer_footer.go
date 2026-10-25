package model

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/dialog/questionform"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/charmbracelet/x/ansi"
)

// composerFooter is the line under the composer: in a chat without the
// sidebar, the active model, context and quota. The chat's kept skills
// (also the ones picked before the chat starts) go to the status row, after
// the mode badge, so they never push this line aside.
func (m *UI) composerFooter(width int) string {
	m.skillTags = m.skillTags[:0]
	if m.state != uiChat && m.state != uiLanding {
		m.status.SetSkills("")
		return ""
	}
	m.status.SetSkills(m.keptSkillsFooter())
	line := ""
	if m.state == uiChat && m.isCompact && m.hasSession() {
		// Unwrapped here; the whole line wraps below.
		stats := renderFooterItems(m.com.Styles, m.footerItems(), m.shownChatModel(), m.session,
			m.compactionLimit(m.shownChatModel(), m.session), m.selectedCLILimits(), math.MaxInt32)
		if n := m.selectedCLIRefills(); n > 0 && stats != "" {
			stats += m.com.Styles.ComposerFooter.Separator.Render(" · ") + m.com.Styles.ComposerFooter.LimitOther.Render(refillsText(n))
		}
		switch {
		case line == "":
			line = stats
		case stats != "":
			line += m.com.Styles.ComposerFooter.Separator.Render(" · ") + stats
		}
	}
	if line == "" {
		return ""
	}
	// Indented so the footer starts in the composer's text column.
	pad := strings.Repeat(" ", footerIndent)
	return pad + strings.ReplaceAll(ansi.Wrap(line, max(width-footerIndent, 1), ""), "\n", "\n"+pad)
}

// skillTag is where a kept skill's × sits on the status row, counted from
// where the skills start.
type skillTag struct {
	id string
	x  int
}

// keptSkillsFooter renders the chat's kept skills, each with an × that
// turns it off, and remembers where each × is.
func (m *UI) keptSkillsFooter() string {
	s := m.com.Styles.ComposerFooter
	var b strings.Builder
	x := 0
	for i, skill := range m.keptSkills() {
		if i > 0 {
			b.WriteString("  ")
			x += 2
		}
		name := s.Skill.Render(skill.Name) + " "
		b.WriteString(name + s.SkillRemove.Render(styles.RemoveIcon))
		x += ansi.StringWidth(name)
		m.skillTags = append(m.skillTags, skillTag{id: skill.ID, x: x})
		x += ansi.StringWidth(styles.RemoveIcon)
	}
	return b.String()
}

// skillTagAt returns the kept skill whose × is at column x of the skills on
// the status row.
func (m *UI) skillTagAt(x int) (string, bool) {
	for _, tag := range m.skillTags {
		if x == tag.x {
			return tag.id, true
		}
	}
	return "", false
}

// footerIndent lines the stats up with the composer's text, after its
// prompt column.
const footerIndent = promptWidth

// Stats the composer footer can show, in the order they appear. "resets"
// adds each usage limit's reset time beside it.
const (
	footerModel       = "model"
	footerSettings    = "settings"
	footerTokens      = "tokens"
	footerContext     = "context"
	footerLimit5h     = "limit_5h"
	footerLimitWeekly = "limit_weekly"
	footerLimitOther  = "limit_other"
	footerResets      = "resets"
	// footerNone marks a deliberately empty footer, since an unset list
	// means the defaults.
	footerNone = "none"
)

// footerChoices are the Customize Composer form's rows.
var footerChoices = []question.Choice{
	{ID: footerModel, Label: "Model name"},
	{ID: footerSettings, Label: "Reasoning, fast and Ultracode"},
	{ID: footerTokens, Label: "Tokens used"},
	{ID: footerContext, Label: "Context left"},
	{ID: footerLimit5h, Label: "5-hour usage left"},
	{ID: footerLimitWeekly, Label: "Weekly usage left"},
	{ID: footerLimitOther, Label: "Other usage limits"},
	{ID: footerResets, Label: "When usage limits reset"},
}

// defaultFooterItems is every stat except reset times.
var defaultFooterItems = []string{
	footerModel, footerSettings, footerTokens, footerContext,
	footerLimit5h, footerLimitWeekly, footerLimitOther,
}

// footerItems returns the stats chosen in options.tui.composer_footer.
func (m *UI) footerItems() []string {
	if cfg := m.com.Config(); cfg != nil && cfg.Options != nil && cfg.Options.TUI != nil && cfg.Options.TUI.ComposerFooter != nil {
		return cfg.Options.TUI.ComposerFooter
	}
	return defaultFooterItems
}

// openComposerFooterForm lets the user pick the footer's stats.
func (m *UI) openComposerFooterForm() {
	if m.activeInline != nil {
		return
	}
	form := questionform.NewQuestionForm(m.com.Styles, question.Request{
		ID: "composer-footer",
		Questions: []question.Question{{
			ID:          "items",
			Type:        question.TypeMultiChoice,
			Label:       "Composer",
			Text:        "What should show under the composer?",
			Description: "Space toggles a row, Enter saves. Usage limits show only for agent CLIs that report them.",
			Choices:     footerChoices,
			Selected:    m.footerItems(),
		}},
	})
	form.OnAnswerCmd = func(responses []question.Answer) tea.Cmd {
		if len(responses) == 0 {
			return nil
		}
		items := []string{}
		for _, c := range footerChoices {
			if slices.Contains(responses[0].SelectedIDs, c.ID) {
				items = append(items, c.ID)
			}
		}
		if len(items) == 0 {
			items = []string{footerNone}
		}
		return m.writeConfigField("options.tui.composer_footer", items, func(err error) tea.Cmd {
			if err != nil {
				return util.ReportError(err)
			}
			m.updateLayoutAndSize()
			return util.ReportInfo("Composer updated")
		})
	}
	m.openInline(form)
	m.updateLayoutAndSize()
}

func (m *UI) compactionLimit(model *workspace.AgentModel, sess *session.Session) int64 {
	fallback := config.DefaultAutoCompactTokenLimit
	var kind catwalk.Type
	if model != nil {
		kind = catwalk.Type(model.ModelCfg.Provider)
	}
	if cfg := m.com.Config(); cfg != nil {
		fallback = cfg.Options.GetAutoCompactTokenLimit()
		if model != nil && cfg.Providers != nil {
			if provider, ok := cfg.Providers.Get(model.ModelCfg.Provider); ok {
				kind = provider.Type
			}
		}
	}
	if config.NativeCompaction(kind) {
		if sess != nil && sess.ContextBudget.Provider == string(kind) && sess.ContextBudget.Model == model.ModelCfg.Model {
			return sess.ContextBudget.Tokens
		}
		return 0 // Missing native telemetry is not a fallback compaction policy.
	}
	if model != nil {
		return config.FallbackCompactionLimit(int64(model.CatwalkCfg.ContextWindow), fallback)
	}
	return fallback
}

// subagentFooter resolves the child's model without changing the parent model.
func (m *UI) subagentFooter(width int) string {
	v := m.subagentView
	if v == nil {
		return ""
	}
	model := workspace.AgentModel{
		CatwalkCfg: catwalk.Model{Name: v.task.Model},
		ModelCfg: config.SelectedModel{
			Model: v.task.Model, ReasoningEffort: v.task.Effort,
		},
	}
	if v.task.Fast {
		model.ModelCfg.ServiceTier = "fast"
	}
	var limits []cliagent.Limit
	if cfg := m.com.Config(); cfg != nil && cfg.Providers != nil {
		for _, p := range cfg.Providers.Seq2() {
			cliName := strings.TrimSuffix(strings.TrimSuffix(string(p.Type), "-cli"), "-code")
			if v.task.CLI != p.ID && v.task.CLI != cliName {
				continue
			}
			for _, candidate := range p.Models {
				if candidate.ID == v.task.Model {
					model.CatwalkCfg = candidate
					break
				}
			}
			limits = cliagent.Limits(p.Type, v.task.Model)
			model.ModelCfg.Provider = p.ID
			break
		}
	}
	return renderFooterItems(m.com.Styles, m.footerItems(), &model, v.session, m.compactionLimit(&model, v.session), limits, width)
}

// renderComposerFooter renders the default stats.
func renderComposerFooter(t *styles.Styles, model *workspace.AgentModel, sess *session.Session, limit int64, limits []cliagent.Limit, width int) string {
	return renderFooterItems(t, defaultFooterItems, model, sess, limit, limits, width)
}

// renderFooterItems renders the chosen stats, in the footer's fixed order.
func renderFooterItems(t *styles.Styles, items []string, model *workspace.AgentModel, sess *session.Session, limit int64, limits []cliagent.Limit, width int) string {
	if width <= 0 {
		return ""
	}
	show := func(id string) bool { return slices.Contains(items, id) }
	s := t.ComposerFooter
	var fields []string
	if model != nil {
		name := cmp.Or(model.CatwalkCfg.Name, model.ModelCfg.Model)
		var field string
		if name != "" && show(footerModel) {
			field = s.Model.Render(name)
		}
		if show(footerSettings) {
			var settings []string
			if effort := cmp.Or(model.ModelCfg.ReasoningEffort, model.CatwalkCfg.DefaultReasoningEffort); effort != "" {
				settings = append(settings, strings.ToLower(effort))
			} else if model.ModelCfg.Think {
				settings = append(settings, "thinking")
			}
			if model.ModelCfg.ServiceTier == "fast" {
				settings = append(settings, "fast")
			}
			if model.ModelCfg.Ultracode {
				settings = append(settings, "ultracode")
			}
			if len(settings) > 0 {
				text := strings.Join(settings, " ")
				if field != "" {
					text = " " + text
				}
				field += s.Text.Render(text)
			}
		}
		if field != "" {
			fields = append(fields, field)
		}
	}
	if sess != nil {
		used := max(int64(0), sess.PromptTokens+sess.CompletionTokens)
		prefix := ""
		if sess.EstimatedUsage {
			prefix = "~"
		}
		if show(footerTokens) {
			text := prefix + footerTokenCount(used)
			if limit > 0 {
				limitPrefix := ""
				if sess.ContextBudget.Estimated && sess.ContextBudget.Tokens == limit {
					limitPrefix = "~"
				}
				text += "/" + limitPrefix + footerTokenCount(limit)
			}
			fields = append(fields, s.Tokens.Render(text+" tokens"))
		}
		if show(footerContext) {
			text := "Context unknown"
			if limit > 0 {
				if sess.ContextBudget.Estimated && sess.ContextBudget.Tokens == limit {
					prefix = "~"
				}
				left := max(0, min(100, 100-float64(used)*100/float64(limit)))
				text = fmt.Sprintf("Context %s%.0f%% left", prefix, left)
			}
			fields = append(fields, s.Context.Render(text))
		}
	}
	for _, limit := range limits {
		id, style := footerLimitOther, s.LimitOther
		switch limit.Name {
		case "5h":
			id, style = footerLimit5h, s.Limit5h
		case "Weekly", "Monthly":
			id, style = footerLimitWeekly, s.LimitWeekly
		}
		if !show(id) {
			continue
		}
		field := style.Render(fmt.Sprintf("%s %.0f%% left", limit.Name, limit.Left()))
		if in := time.Until(limit.ResetsAt); show(footerResets) && !limit.ResetsAt.IsZero() && in > 0 {
			field += s.Resets.Render(" resets " + shortDuration(in))
		}
		fields = append(fields, field)
	}
	return ansi.Wrap(strings.Join(fields, s.Separator.Render(" · ")), width, "")
}

// refillsText is "1 free refill" or "N free refills".
func refillsText(n int) string {
	if n == 1 {
		return "1 free refill"
	}
	return strconv.Itoa(n) + " free refills"
}

func footerTokenCount(tokens int64) string {
	divisor, suffix := float64(1), ""
	switch {
	case tokens >= 1_000_000:
		divisor, suffix = 1_000_000, "m"
	case tokens >= 1_000:
		divisor, suffix = 1_000, "k"
	default:
		return strconv.FormatInt(tokens, 10)
	}
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(float64(tokens)/divisor, 'f', 2, 64), "0"), ".") + suffix
}
