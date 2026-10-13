package config

import (
	"github.com/charmbracelet/crush/internal/config/resolve"
	"github.com/charmbracelet/crush/internal/env"
)

// Variable resolution lives in package resolve. These aliases keep the
// config API source-compatible for its importers.
type (
	// VariableResolver expands variable references in config values.
	VariableResolver = resolve.VariableResolver
	// Expander is the single-value shell expansion seam used by the shell
	// variable resolver.
	Expander = resolve.Expander
	// ShellResolverOption customizes shell variable resolver construction.
	ShellResolverOption = resolve.ShellResolverOption
)

// IdentityResolver returns a VariableResolver that passes values through
// unchanged.
func IdentityResolver() VariableResolver {
	return resolve.IdentityResolver()
}

// NewShellVariableResolver returns a VariableResolver that delegates to
// the embedded shell. See resolve.NewShellVariableResolver.
func NewShellVariableResolver(e env.Env, opts ...ShellResolverOption) VariableResolver {
	return resolve.NewShellVariableResolver(e, opts...)
}
