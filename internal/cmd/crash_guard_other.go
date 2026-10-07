//go:build !windows

package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	crushlog "github.com/charmbracelet/crush/internal/log"
	"github.com/charmbracelet/x/term"
	"golang.org/x/sys/unix"
)

// terminalReset undoes the modes the TUI turns on: alternate screen, hidden
// cursor, mouse tracking, focus events, bracketed paste, keyboard
// enhancements, synchronized output and text attributes.
const terminalReset = "\x1b[?1049l\x1b[?25h" +
	"\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l" +
	"\x1b[?1004l\x1b[?2004l\x1b[?2026l\x1b[?2027l" +
	"\x1b[<99u\x1b[>4;0m\x1b[0m"

// runCrashGuarded starts the interactive Crush as a child and waits for
// it. A Go crash can't be caught inside the crashing process, and it dies
// with the terminal still in the TUI's modes; the guard, as the process
// the shell waits for, puts the terminal back before the shell prompt
// returns and prints one line pointing at the crash report. It reports
// false, doing nothing, when not on a terminal or already guarded.
func runCrashGuarded() (exitCode int, guarded bool) {
	if os.Getenv(crashReportEnv) != "" || !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
		return 0, false
	}
	exe, err := os.Executable()
	if err != nil {
		return 0, false
	}
	dir, err := crushlog.CrashDir()
	if err != nil {
		return 0, false
	}
	state, err := term.GetState(os.Stdin.Fd())
	if err != nil {
		return 0, false
	}
	report := filepath.Join(dir, fmt.Sprintf("crash-%s-%d.log", time.Now().Format("20060102-150405"), os.Getpid()))

	child := exec.Command(exe, os.Args[1:]...)
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	child.Env = append(os.Environ(), crashReportEnv+"="+report)
	restore := func() { _ = term.Restore(os.Stdin.Fd(), state) }
	return superviseChild(child, report, restore, os.Stdout, os.Stderr)
}

// superviseChild runs child to the end. When it dies from a crash or a
// signal, it calls restore, writes terminalReset to out and a one-line
// notice to errOut. It reports false only when child could not start.
func superviseChild(child *exec.Cmd, report string, restore func(), out, errOut io.Writer) (exitCode int, started bool) {
	// The child shares the terminal's process group, so terminal signals
	// reach it directly; the guard catches them only to outlive it, and
	// passes SIGTERM on. Catching, unlike ignoring, isn't inherited.
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGHUP, syscall.SIGTERM)
	defer signal.Stop(signals)
	if err := child.Start(); err != nil {
		return 0, false
	}
	go func() {
		for sig := range signals {
			if sig == syscall.SIGTERM {
				_ = child.Process.Signal(sig)
			}
		}
	}()

	err := child.Wait()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return 1, true
	}
	status, _ := child.ProcessState.Sys().(syscall.WaitStatus)
	// Go exits with status 2 after an unrecovered panic or fatal error.
	if !status.Signaled() && status.ExitStatus() != 2 {
		return status.ExitStatus(), true
	}

	restore()
	reset := terminalReset
	if os.Getenv("KONSOLE_VERSION") != "" {
		reset += konsoleShowScrollMarker
	}
	_, _ = io.WriteString(out, reset+"\r\n")
	switch {
	case reportHasContent(report):
		fmt.Fprintf(errOut, "Crush crashed. Details saved to %s\n", report)
	case status.Signaled():
		fmt.Fprintf(errOut, "Crush was stopped (%s).\n", status.Signal())
	default:
		fmt.Fprintln(errOut, "Crush crashed.")
	}
	if status.Signaled() {
		return 128 + int(status.Signal()), true
	}
	return status.ExitStatus(), true
}

func reportHasContent(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

// quietStderr points standard error at file while the TUI draws, so a
// fatal crash's report lands only in file instead of over the screen.
// The runtime's crash output is paused meanwhile; it would write the same
// report to file a second time. restore undoes both.
func quietStderr(file *os.File) (restore func()) {
	if file == nil || !term.IsTerminal(os.Stderr.Fd()) {
		return func() {}
	}
	saved, err := unix.Dup(int(os.Stderr.Fd()))
	if err != nil {
		return func() {}
	}
	if unix.Dup2(int(file.Fd()), int(os.Stderr.Fd())) != nil {
		_ = unix.Close(saved)
		return func() {}
	}
	_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
	return func() {
		_ = unix.Dup2(saved, int(os.Stderr.Fd()))
		_ = unix.Close(saved)
		_ = debug.SetCrashOutput(file, debug.CrashOptions{})
	}
}
