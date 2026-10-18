package backend

import (
	"os"
	"regexp"
)

// "Is $VAR set" (engine-design.md 5.9 b).

// envName is a bare environment variable name.
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// maxEnvNames is how many names one check may ask about.
const maxEnvNames = 64

// EnvIsSet tells, per name, whether the variable has a non-empty value in
// the environment the workspace's config resolver reads: the engine's own
// (config.Load builds its resolver from env.New()). It never returns a
// value and never expands or runs anything. A name that is not a bare
// variable name, or more than 64 names, is ErrBadEnvName.
func (b *Backend) EnvIsSet(workspaceID string, names []string) (map[string]bool, error) {
	if _, err := b.GetWorkspace(workspaceID); err != nil {
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
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = os.Getenv(name) != ""
	}
	return set, nil
}
