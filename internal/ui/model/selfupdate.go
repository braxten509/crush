package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/cliupdate"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/selfupdate"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// selfUpdateMsg carries the result of a check for a newer stable Crush.
type selfUpdateMsg struct {
	release *selfupdate.Release
	err     error
	manual  bool
}

// selfUpdateBuiltMsg reports the end of a merge, build and test run.
type selfUpdateBuiltMsg struct {
	release *selfupdate.Release
	version string // the tested build, ready to install
	fixed   []string
	err     error
}

// selfUpdateInstalledMsg reports an installed update.
type selfUpdateInstalledMsg struct {
	version string
	pushErr error
	err     error
}

// declinedCrush is how a skipped Crush release is remembered, next to
// skipped agent CLI releases.
func declinedCrush(tag string) []cliupdate.Update {
	return []cliupdate.Update{{Name: "Crush", Bin: "crush", Latest: tag}}
}

// checkSelfUpdate looks for a stable upstream release newer than this fork.
// Upstream's own notice is replaced by this for fork builds.
func (m *UI) checkSelfUpdate(manual bool) tea.Cmd {
	if testing.Testing() {
		return nil
	}
	dir := selfupdate.Dir()
	if dir == "" {
		if manual {
			return util.ReportWarn("This Crush has no fork checkout to update")
		}
		return nil
	}
	if m.selfUpdateRunning {
		if manual {
			return util.ReportInfo("Crush is already updating")
		}
		return nil
	}
	if manual {
		m.status.SetInfoMsg(util.InfoMsg{Type: util.InfoTypeInfo, Msg: "Checking for a new Crush release…"})
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		release, err := selfupdate.Check(ctx, dir)
		return selfUpdateMsg{release: release, err: err, manual: manual}
	}
}

func (m *UI) handleSelfUpdate(msg selfUpdateMsg) tea.Cmd {
	switch {
	case msg.err != nil:
		if msg.manual {
			return m.showCLIUpdateStatus(util.InfoTypeWarn, "Couldn't check for a new Crush: "+msg.err.Error())
		}
		return nil
	case msg.release == nil:
		if msg.manual {
			return m.showCLIUpdateStatus(util.InfoTypeSuccess, "Crush is on the latest stable release")
		}
		return nil
	case !msg.manual && len(cliupdate.WithoutDeclined(declinedCrush(msg.release.Tag))) == 0:
		return nil
	}
	if msg.manual {
		m.status.ClearInfoMsg()
	}
	release := msg.release
	if version := selfupdate.Staged(context.Background(), release.Dir, release.Tag); version != "" {
		return m.offerSelfUpdateInstall(release, nil)
	}
	return m.openSelfUpdateForm("crush-update",
		fmt.Sprintf("Update Crush to %s?", release.Tag),
		fmt.Sprintf("Upstream released %s (you have %s). Your changes stay: Crush merges it into %s, builds it and runs every test in the background. If files clash or tests fail, an AI agent fixes them. You're asked again before anything is installed. Saying No skips this version.", release.Tag, release.Current, release.Dir),
		func() tea.Cmd { return m.startSelfUpdate(release) },
		func() tea.Cmd {
			cliupdate.Decline(declinedCrush(release.Tag))
			return util.ReportInfo("Skipped Crush " + release.Tag)
		},
		fmt.Sprintf("Crush %s is out. Run Update Crush from the command palette.", release.Tag))
}

// openSelfUpdateForm asks a yes/no question about the update. When another
// form is open, the offer goes to the status bar instead.
func (m *UI) openSelfUpdateForm(id, text, description string, yes, no func() tea.Cmd, waiting string) tea.Cmd {
	moved := func() tea.Cmd { return m.showCLIUpdateStatus(util.InfoTypeUpdate, waiting) }
	if m.activeInline != nil || m.secureQuestionFormOpen() {
		return moved()
	}
	form := dialog.NewQuestionForm(m.com.Styles, question.Request{
		ID: id,
		Questions: []question.Question{{
			ID:          "answer",
			Type:        question.TypeYesNo,
			Label:       "Crush update",
			Text:        text,
			Description: description,
		}},
	})
	form.OnAnswerCmd = func(responses []question.Answer) tea.Cmd {
		if len(responses) == 0 || responses[0].Yes == nil {
			return nil
		}
		if *responses[0].Yes {
			return yes()
		}
		return no()
	}
	m.cliUpdatePrompt = &cliUpdatePrompt{form: form, moved: moved}
	m.activeInline = form
	m.textarea.Blur()
	m.focus = uiFocusEditor
	m.activeInline.SetFocused(true)
	m.updateLayoutAndSize()
	return nil
}

// startSelfUpdate merges, builds and tests the release in the background,
// handing clashes and failures to an AI agent.
func (m *UI) startSelfUpdate(release *selfupdate.Release) tea.Cmd {
	m.selfUpdateRunning = true
	m.status.SetInfoMsg(util.InfoMsg{Type: util.InfoTypeUpdate, Msg: fmt.Sprintf("Updating Crush to %s in the background: merging, building and testing…", release.Tag), TTL: time.Hour})
	return func() tea.Msg {
		ctx := context.Background()
		log, err := selfupdate.CreateLog()
		if err != nil {
			return selfUpdateBuiltMsg{release: release, err: err}
		}
		defer log.Close()
		clashes, err := selfupdate.Merge(ctx, release.Dir, release.Tag)
		if err != nil {
			return selfUpdateBuiltMsg{release: release, err: err}
		}
		if len(clashes) > 0 {
			fmt.Fprintf(log, "Clashes in %s; an AI agent is fixing them.\n", strings.Join(clashes, ", "))
			err = selfupdate.Fix(ctx, release.Dir, release.Tag, clashes, "", log)
		} else if err = selfupdate.Build(ctx, release.Dir, release.Tag, log); err != nil && !errors.Is(err, selfupdate.ErrDirty) {
			fmt.Fprintf(log, "%v\nAn AI agent is fixing this.\n", err)
			err = selfupdate.Fix(ctx, release.Dir, release.Tag, nil, err.Error(), log)
		}
		if err != nil {
			return selfUpdateBuiltMsg{release: release, fixed: clashes, err: err}
		}
		return selfUpdateBuiltMsg{release: release, fixed: clashes, version: selfupdate.Staged(ctx, release.Dir, release.Tag)}
	}
}

func (m *UI) handleSelfUpdateBuilt(msg selfUpdateBuiltMsg) tea.Cmd {
	m.selfUpdateRunning = false
	if msg.err != nil {
		m.status.ClearInfoMsg()
		return m.showCLIUpdateStatus(util.InfoTypeError, fmt.Sprintf("Crush %s update stopped: %s. Details: %s", msg.release.Tag, firstLine(msg.err.Error()), selfupdate.LogPath()))
	}
	m.status.ClearInfoMsg()
	return m.offerSelfUpdateInstall(msg.release, msg.fixed)
}

func (m *UI) offerSelfUpdateInstall(release *selfupdate.Release, fixed []string) tea.Cmd {
	how := "It merged cleanly"
	if len(fixed) > 0 {
		how = "An AI agent fixed the clashes in " + joinNames(fixed)
	}
	return m.openSelfUpdateForm("crush-install",
		fmt.Sprintf("Install Crush %s now?", release.Tag),
		how+" and every test passed. The old version is backed up, and the merge is uploaded to your fork on GitHub. Restart Crush afterwards to use it. Saying No keeps the tested build for later (Update Crush in the command palette).",
		func() tea.Cmd {
			return func() tea.Msg {
				version, pushErr, err := selfupdate.Install(context.Background(), release.Dir)
				return selfUpdateInstalledMsg{version: version, pushErr: pushErr, err: err}
			}
		},
		func() tea.Cmd { return util.ReportInfo("The tested Crush " + release.Tag + " waits in Update Crush") },
		fmt.Sprintf("Crush %s is built and tested. Run Update Crush from the command palette to install it.", release.Tag))
}

func (m *UI) handleSelfUpdateInstalled(msg selfUpdateInstalledMsg) tea.Cmd {
	switch {
	case msg.err != nil:
		return m.showCLIUpdateStatus(util.InfoTypeError, "Couldn't install the Crush update: "+firstLine(msg.err.Error()))
	case msg.pushErr != nil:
		return m.showCLIUpdateStatus(util.InfoTypeWarn, fmt.Sprintf("Installed Crush %s, but %s. Restart Crush to use it.", msg.version, firstLine(msg.pushErr.Error())))
	}
	return m.showCLIUpdateStatus(util.InfoTypeSuccess, fmt.Sprintf("Installed Crush %s and uploaded it to GitHub. Restart Crush to use it.", msg.version))
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
