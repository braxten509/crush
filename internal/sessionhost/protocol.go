// Package sessionhost shows several separate Crush sessions in one
// terminal. Each session is its own Crush process running in a hidden
// terminal (a pty read by a terminal emulator); the host draws the session
// list on the left and the chosen session beside it, and passes typing and
// clicks to that session. Sessions tell the host what they are doing with a
// private escape sequence (see [Status]).
package sessionhost

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
)

// ChildEnv is set in the environment of every session the host starts, so
// that a session doesn't start a host of its own and knows to report its
// [Status].
const ChildEnv = "CRUSH_SESSION_HOST"

// statusOSC is the private OSC number sessions report their [Status] with.
const statusOSC = 7373

// stopServerOSC is the private OSC number the host sends a session, as
// input, to stop one of its [Status.Servers].
const stopServerOSC = 7374

// State is what a session is doing.
type State string

const (
	StateReady   State = "ready"
	StateWorking State = "working"
	// StateBackground: the main agent yielded while waiting for helpers
	// or their results.
	StateBackground State = "background"
	// StateWaiting: a permission prompt or a question is waiting for the
	// user.
	StateWaiting State = "waiting"
)

// Status is what a session tells the host about itself.
type Status struct {
	Title                 string `json:"title,omitempty"`
	Dir                   string `json:"dir,omitempty"`
	Model                 string `json:"model,omitempty"`
	Theme                 string `json:"theme,omitempty"`
	State                 State  `json:"state,omitempty"`
	UseTerminalBackground bool   `json:"use_terminal_background,omitempty"`
	// Servers are the ports the session's background work listens on.
	Servers []int `json:"servers,omitempty"`
}

// Equal reports whether s and o say the same.
func (s Status) Equal(o Status) bool {
	return s.Title == o.Title && s.Dir == o.Dir && s.Model == o.Model && s.Theme == o.Theme &&
		s.State == o.State && s.UseTerminalBackground == o.UseTerminalBackground &&
		slices.Equal(s.Servers, o.Servers)
}

// InHost reports whether this Crush runs as a session inside the host.
func InHost() bool {
	return os.Getenv(ChildEnv) != ""
}

// Sequence returns the escape sequence that reports s to the host. JSON
// escapes every control character, so the payload can't end the sequence
// early.
func (s Status) Sequence() string {
	b, _ := json.Marshal(s)
	return fmt.Sprintf("\x1b]%d;%s\x07", statusOSC, b)
}

// parseStatus reads the data of a status sequence, which still starts with
// the OSC number.
func parseStatus(data []byte) (Status, bool) {
	_, payload, ok := bytes.Cut(data, []byte{';'})
	if !ok {
		return Status{}, false
	}
	var s Status
	if err := json.Unmarshal(payload, &s); err != nil {
		return Status{}, false
	}
	return s, true
}

// stopServerSequence asks a session to stop its server on port.
func stopServerSequence(port int) string {
	return fmt.Sprintf("\x1b]%d;%d\x07", stopServerOSC, port)
}

// ParseStopServer reads the port out of a whole sequence from the host
// asking to stop a server.
func ParseStopServer(seq string) (port int, ok bool) {
	data, ok := strings.CutPrefix(seq, "\x1b]"+strconv.Itoa(stopServerOSC)+";")
	if !ok {
		return 0, false
	}
	data = strings.TrimSuffix(strings.TrimSuffix(data, "\x07"), "\x1b\\")
	port, err := strconv.Atoi(data)
	return port, err == nil && port > 0
}
