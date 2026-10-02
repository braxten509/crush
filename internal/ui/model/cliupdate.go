package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/cliupdate"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// cliUpdatesMsg carries the result of an agent CLI update check. Manual
// checks (from the command palette) also report "up to date" and errors.
type cliUpdatesMsg struct {
	updates []cliupdate.Update
	errs    []error
	manual  bool
}

// cliUpdatesInstalledMsg reports which CLI updates were installed.
type cliUpdatesInstalledMsg struct {
	installed []cliupdate.Update
	errs      []error
}

// cliUpdatePrompt is an open CLI update form and the updates it offers.
type cliUpdatePrompt struct {
	form    *dialog.QuestionForm
	updates []cliupdate.Update
}

// checkCLIUpdates looks for newer releases of the installed agent CLIs.
// The startup check runs at most once per [cliupdate.AutoCheckEvery].
func (m *UI) checkCLIUpdates(manual bool) tea.Cmd {
	if testing.Testing() {
		return nil
	}
	if manual {
		m.status.SetInfoMsg(util.InfoMsg{Type: util.InfoTypeInfo, Msg: "Checking agent CLIs for updates…"})
	}
	return func() tea.Msg {
		if !manual && !cliupdate.StartAutoCheck() {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		updates, errs := cliupdate.Check(ctx)
		return cliUpdatesMsg{updates: updates, errs: errs, manual: manual}
	}
}

func (m *UI) handleCLIUpdates(msg cliUpdatesMsg) tea.Cmd {
	updates := msg.updates
	if !msg.manual {
		updates = cliupdate.WithoutDeclined(updates)
	}
	if len(updates) == 0 {
		if !msg.manual {
			return nil
		}
		if len(msg.errs) > 0 {
			return m.showCLIUpdateStatus(util.InfoTypeWarn, "Couldn't check every CLI: "+strings.ReplaceAll(errors.Join(msg.errs...).Error(), "\n", "; "))
		}
		return m.showCLIUpdateStatus(util.InfoTypeSuccess, "All agent CLIs are up to date")
	}
	if m.activeInline != nil {
		// Don't replace a question or plan form that's waiting on the user.
		return m.showCLIUpdatesAvailable(updates)
	}
	if msg.manual {
		m.status.ClearInfoMsg()
	}
	m.openCLIUpdateForm(updates)
	return nil
}

func (m *UI) openCLIUpdateForm(updates []cliupdate.Update) {
	names := make([]string, len(updates))
	changes := make([]string, len(updates))
	for i, u := range updates {
		names[i] = u.Name
		changes[i] = fmt.Sprintf("%s %s → %s", u.Name, u.Current, u.Latest)
	}
	form := dialog.NewQuestionForm(m.com.Styles, question.Request{
		ID: "cli-updates",
		Questions: []question.Question{{
			ID:          "install",
			Type:        question.TypeYesNo,
			Label:       "CLI updates",
			Text:        fmt.Sprintf("Install updates for %s?", joinNames(names)),
			Description: strings.Join(changes, " · ") + ". Each CLI runs its own updater in the background. No skips these versions until newer ones come out.",
		}},
	})
	form.OnAnswerCmd = func(responses []question.Answer) tea.Cmd {
		if len(responses) == 0 || responses[0].Yes == nil {
			return nil
		}
		if !*responses[0].Yes {
			cliupdate.Decline(updates)
			return util.ReportInfo("Skipped these CLI updates")
		}
		return m.installCLIUpdates(updates)
	}
	m.cliUpdatePrompt = &cliUpdatePrompt{form: form, updates: updates}
	m.activeInline = form
	m.textarea.Blur()
	m.focus = uiFocusEditor
	m.activeInline.SetFocused(true)
	m.updateLayoutAndSize()
}

// cliUpdatePromptOpen reports whether the CLI update form is the active
// inline editor.
func (m *UI) cliUpdatePromptOpen() bool {
	return m.cliUpdatePrompt != nil && m.activeInline == m.cliUpdatePrompt.form
}

// showCLIUpdatesAvailable points at the command palette for updates the
// user wasn't asked about.
func (m *UI) showCLIUpdatesAvailable(updates []cliupdate.Update) tea.Cmd {
	return m.showCLIUpdateStatus(util.InfoTypeUpdate, fmt.Sprintf("%s available. Run Update Agent CLIs from the command palette.", updateList(updates)))
}

func (m *UI) installCLIUpdates(updates []cliupdate.Update) tea.Cmd {
	return tea.Batch(
		m.showCLIUpdateStatus(util.InfoTypeUpdate, fmt.Sprintf("Updating %s…", joinNames(namesOf(updates)))),
		func() tea.Msg {
			errs := make([]error, len(updates))
			var wg sync.WaitGroup
			for i, u := range updates {
				wg.Add(1)
				go func() {
					defer wg.Done()
					errs[i] = cliupdate.Install(context.Background(), u)
				}()
			}
			wg.Wait()
			var msg cliUpdatesInstalledMsg
			for i, err := range errs {
				if err != nil {
					msg.errs = append(msg.errs, err)
				} else {
					msg.installed = append(msg.installed, updates[i])
				}
			}
			return msg
		},
	)
}

func (m *UI) handleCLIUpdatesInstalled(msg cliUpdatesInstalledMsg) tea.Cmd {
	var parts []string
	if len(msg.installed) > 0 {
		done := make([]string, len(msg.installed))
		for i, u := range msg.installed {
			done[i] = u.Name + " " + u.Latest
		}
		parts = append(parts, "Updated "+joinNames(done))
	}
	if len(msg.errs) > 0 {
		parts = append(parts, "failed: "+errors.Join(msg.errs...).Error())
	}
	typ := util.InfoTypeSuccess
	if len(msg.errs) > 0 {
		typ = util.InfoTypeError
	}
	return m.showCLIUpdateStatus(typ, strings.ReplaceAll(strings.Join(parts, "; "), "\n", "; "))
}

func (m *UI) showCLIUpdateStatus(typ util.InfoType, text string) tea.Cmd {
	ttl := 10 * time.Second
	m.status.SetInfoMsg(util.InfoMsg{Type: typ, Msg: text, TTL: ttl})
	return clearInfoMsgCmd(ttl)
}

func updateList(updates []cliupdate.Update) string {
	if len(updates) == 1 {
		return updates[0].Name + " " + updates[0].Latest + " is"
	}
	return "Updates for " + joinNames(namesOf(updates)) + " are"
}

func namesOf(updates []cliupdate.Update) []string {
	names := make([]string, len(updates))
	for i, u := range updates {
		names[i] = u.Name
	}
	return names
}

// joinNames joins names as "a", "a and b" or "a, b and c".
func joinNames(names []string) string {
	if len(names) <= 1 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
