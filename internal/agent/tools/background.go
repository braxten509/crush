package tools

import (
	"fmt"
	"sync"
)

const NativeShellEnv = "CRUSH_NATIVE_SHELL"

var shellBackgrounds = struct {
	sync.Mutex
	sessions   map[string]map[chan struct{}]struct{}
	foreground map[string]struct{}
}{sessions: make(map[string]map[chan struct{}]struct{}), foreground: make(map[string]struct{})}

func registerShellBackground(sessionID string, signal chan struct{}) func() {
	shellBackgrounds.Lock()
	if shellBackgrounds.sessions[sessionID] == nil {
		shellBackgrounds.sessions[sessionID] = make(map[chan struct{}]struct{})
	}
	shellBackgrounds.sessions[sessionID][signal] = struct{}{}
	shellBackgrounds.foreground[fmt.Sprintf("%p", signal)] = struct{}{}
	shellBackgrounds.Unlock()
	return func() {
		shellBackgrounds.Lock()
		defer shellBackgrounds.Unlock()
		delete(shellBackgrounds.sessions[sessionID], signal)
		delete(shellBackgrounds.foreground, fmt.Sprintf("%p", signal))
		if len(shellBackgrounds.sessions[sessionID]) == 0 {
			delete(shellBackgrounds.sessions, sessionID)
		}
	}
}

func IsForegroundShell(id string) bool {
	shellBackgrounds.Lock()
	defer shellBackgrounds.Unlock()
	_, active := shellBackgrounds.foreground[id]
	return active
}

func BackgroundShell(sessionID string) bool {
	shellBackgrounds.Lock()
	defer shellBackgrounds.Unlock()
	active := false
	for signal := range shellBackgrounds.sessions[sessionID] {
		active = true
		select {
		case signal <- struct{}{}:
		default:
		}
	}
	return active
}
