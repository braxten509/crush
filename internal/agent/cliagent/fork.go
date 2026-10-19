package cliagent

import (
	"errors"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
)

// ForkPoint is where a branch cuts an AI tool's own memory: the tool's
// session and its marker for the last message the fork keeps.
type ForkPoint struct{ Native, Marker, MarkerKind string }

// ErrNoFork means the tool can't fork its memory at the point given, or its
// fork call failed. The branch then gets the written copy.
var ErrNoFork = errors.New("fork not available")

// CanFork reports whether an AI tool forks its own memory at a marker of
// the given kind (desktop-design 3.4, as the live checks of
// docs/evidence/phase4/live-summary.md found). The fork itself runs inside
// the branch's first turn ([Turn.Fork]): Claude names its new session only
// on that turn's stream, and Codex and Grok fork in the process that then
// runs the turn, as the live checks did.
//
// OpenCode has a real fork (HTTP POST /session/{id}/fork) that could not be
// checked with a model yet, and AGY has none: both get the written copy.
func CanFork(kind catwalk.Type, markerKind string) error {
	switch {
	case kind == config.TypeClaudeCode && markerKind == MarkerMessage,
		kind == config.TypeCodexCLI && markerKind == MarkerTurn,
		kind == config.TypeGrokCLI && markerKind == MarkerPromptIndex:
		return nil
	}
	return ErrNoFork
}
