package backend

import (
	"os"
	"regexp"
	"strings"
)

// "Is $VAR set" (engine-design.md 5.9 b).

// envName is a bare environment variable name.
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// maxEnvNames is how many names one check may ask about.
const maxEnvNames = 64

// EnvIsSet tells, per name, whether the variable has a non-empty value in
// the environment the workspace's `!` commands get: the workspace's own Env
// over the engine's. It never returns a value and never expands or runs
// anything. A name that is not a bare variable name, or more than 64
// names, is ErrBadEnvName.
func (b *Backend) EnvIsSet(workspaceID string, names []string) (map[string]bool, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	if len(names) > maxEnvNames {
		return nil, ErrBadEnvName
	}
	for _, name := range names {
		if !envName.MatchString(name) {
			return nil, ErrBadEnvName
		}
	}
	// As the `!` commands get append(os.Environ(), ws.Env...): the last
	// entry of a name wins, and one with no "=" sets nothing. Only whether
	// each value is empty is kept.
	own := make(map[string]bool)
	for _, entry := range ws.Env {
		if name, value, ok := strings.Cut(entry, "="); ok && name != "" {
			own[name] = value != ""
		}
	}
	set := make(map[string]bool, len(names))
	for _, name := range names {
		nonEmpty, ok := own[name]
		if !ok {
			nonEmpty = os.Getenv(name) != ""
		}
		set[name] = nonEmpty
	}
	return set, nil
}
