package noninteractive

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

// Status lines written to stderr when the agent starts and stops waiting
// on its sub-agents.
const (
	WaitingLine = "Waiting for sub-agents to finish..."
	ResumedLine = "Sub-agents finished; continuing."
)

// Spinner is the activity indicator shown on stderr while the run has
// produced no output yet.
type Spinner interface {
	Start()
	Stop()
}

// Terminal describes where and how a run presents itself. The caller
// supplies the spinner so this package stays free of TUI dependencies.
type Terminal struct {
	// Stdout receives the agent's reply.
	Stdout io.Writer
	// HideSpinner suppresses the spinner (--quiet or --verbose).
	HideSpinner bool
	// Progress enables the terminal's indeterminate progress bar.
	Progress bool
	// NewSpinner builds the spinner. Its key handling may call cancel to
	// abort the run. It is only called when stderr is a terminal and the
	// spinner is not hidden; nil means no spinner.
	NewSpinner func(ctx context.Context, cancel context.CancelFunc) Spinner
}

// Output is the presentation state of a running turn that a [Backend]'s
// Stream writes through: the reply on stdout, and the spinner, progress
// bar and status lines on stderr.
type Output struct {
	// Stdout receives the agent's reply.
	Stdout io.Writer

	spinner     Spinner
	hideSpinner bool
	stderrTTY   bool
	progress    bool
}

// newOutput starts the spinner when stderr is a terminal and it is not
// hidden.
func newOutput(ctx context.Context, cancel context.CancelFunc, t Terminal) *Output {
	o := &Output{
		Stdout:      t.Stdout,
		hideSpinner: t.HideSpinner,
		stderrTTY:   term.IsTerminal(os.Stderr.Fd()),
		progress:    t.Progress,
	}
	if !o.hideSpinner && o.stderrTTY && t.NewSpinner != nil {
		o.spinner = t.NewSpinner(ctx, cancel)
		o.spinner.Start()
	}
	return o
}

// StopSpinner stops the spinner if it is running. It is safe to call more
// than once and on a nil Output.
func (o *Output) StopSpinner() {
	if o == nil {
		return
	}
	if !o.hideSpinner && o.spinner != nil {
		o.spinner.Stop()
		o.spinner = nil
	}
}

// Heartbeat re-arms the terminal's indeterminate progress bar. Call it on
// every iteration of the stream loop so the terminal does not hide the bar
// for inactivity.
func (o *Output) Heartbeat() {
	if o.progress && o.stderrTTY {
		_, _ = fmt.Fprint(os.Stderr, ansi.SetIndeterminateProgressBar)
	}
}

// Finish clears the progress bar and ends stdout with a newline, so that
// when stdout is a terminal the shell prompt does not overwrite the last
// line of the reply.
func (o *Output) Finish() {
	if o.progress && o.stderrTTY {
		_, _ = fmt.Fprint(os.Stderr, ansi.ResetProgressBar)
	}
	_, _ = fmt.Fprintln(o.Stdout)
}

// SubAgentStatus reports on stderr that the agent started (waiting) or
// stopped waiting on its sub-agents. printed says whether any reply text
// has been written yet, in which case the status starts on a new line.
func (o *Output) SubAgentStatus(waiting, printed bool) {
	o.StopSpinner()
	if printed && o.stderrTTY {
		fmt.Fprintln(os.Stderr)
	}
	if waiting {
		fmt.Fprintln(os.Stderr, WaitingLine)
	} else {
		fmt.Fprintln(os.Stderr, ResumedLine)
	}
}
