package sessionhost

import (
	"os"
	"os/signal"
	"syscall"
)

// QuitOnHangup calls quit, once, when the terminal goes away (its window
// closed). Go's default for SIGHUP is to exit at once, which skips the
// shutdown that stops the sessions, background jobs, sub-agents and agent
// CLIs a Crush started. Later hangups are caught and dropped, so a second
// one (the shell and the crash guard both pass it on) can't cut that
// shutdown short.
func QuitOnHangup(quit func()) {
	hangup := make(chan os.Signal, 1)
	signal.Notify(hangup, syscall.SIGHUP)
	go func() {
		<-hangup
		quit()
	}()
}
