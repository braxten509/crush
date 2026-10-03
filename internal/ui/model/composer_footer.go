package model

import (
	"cmp"
	"fmt"
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
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/ui/util"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/charmbracelet/x/ansi"
)

// compactUsage keeps the active model, context, and quota beside the composer.
func (m *UI) compactUsage(width int) string {
	if m.state != uiChat || !m.isCompact || !m.hasSession() {
		return ""
	}
	// Indented so the stats start in the composer's text column.
	footer := renderFooterItems(m.com.Styles, m.footerItems(), m.selectedLargeModel(), m.session,
		m.compactionLimit(), m.selectedCLILimits(), width-footerIndent)
	if footer == "" {
		return ""
	}
	pad := strings.Repeat(" ", footerIndent)
	return pad + strings.ReplaceAll(footer, "\n", "\n"+pad)
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
	form := dialog.NewQuestionForm(m.com.Styles, question.Request{
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
		if err := m.com.Workspace.SetConfigField(config.ScopeGlobal, "options.tui.composer_footer", items); err != nil {
			return util.ReportError(err)
		}
		m.updateLayoutAndSize()
		return util.ReportInfo("Composer updated")
	}
	m.activeInline = form
	m.textarea.Blur()
	m.focus = uiFocusEditor
	m.activeInline.SetFocused(true)
	m.updateLayoutAndSize()
}

func (m *UI) compactionLimit() int64 {
	if cfg := m.com.Config(); cfg != nil {
		return cfg.Options.GetAutoCompactTokenLimit()
	}
	return config.DefaultAutoCompactTokenLimit
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
			break
		}
	}
	return renderFooterItems(m.com.Styles, m.footerItems(), &model, v.session, m.compactionLimit(), limits, width)
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
	if sess != nil && limit > 0 {
		used := max(int64(0), sess.PromptTokens+sess.CompletionTokens)
		prefix := ""
		if sess.EstimatedUsage {
			prefix = "~"
		}
		if show(footerTokens) {
			fields = append(fields, s.Tokens.Render(fmt.Sprintf("%s%s/%s tokens", prefix, footerTokenCount(used), footerTokenCount(limit))))
		}
		if show(footerContext) {
			left := max(0, min(100, 100-float64(used)*100/float64(limit)))
			fields = append(fields, s.Context.Render(fmt.Sprintf("Context %s%.0f%% left", prefix, left)))
		}
	}
	for _, limit := range limits {
		id, style := footerLimitOther, s.LimitOther
		switch limit.Name {
		case "5h":
			id, style = footerLimit5h, s.Limit5h
		case "Weekly":
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
