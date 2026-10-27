package proto

// Server control commands accepted by the /control endpoint.
const (
	// ServerControlShutdown is the original, unconditional spelling.
	// Servers still accept it, but now apply the same idleness check as
	// ServerControlShutdownIfIdle so that clients predating the check
	// cannot take live sessions down.
	ServerControlShutdown = "shutdown"
	// ServerControlShutdownIfIdle asks the server to shut down only if it
	// is idle. It exists so a client can tell whether the server checks
	// that condition at all: servers predating the check reject the
	// unknown command instead of shutting down and taking other sessions
	// with them.
	ServerControlShutdownIfIdle = "shutdown_if_idle"
	// ServerControlStop stops every chat's turn and sub-agent for good and
	// shuts the server down although it is busy: the desktop's "Stop engine"
	// when its last window closes. Servers predating it reject it.
	ServerControlStop = "stop"
)

// ServerControl represents a server control request.
type ServerControl struct {
	Command string `json:"command"`
}
