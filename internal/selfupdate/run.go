package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// fixAgent runs the fixing agent's command line in dir; tests replace it so
// that no real agent runs.
var fixAgent = func(ctx context.Context, dir string, log io.Writer, args []string) error {
	return run(ctx, dir, log, args...)
}

// SetFixAgentForTest replaces the fixing agent with agent, which gets the
// command line [Fix] would run, and returns a function that restores it.
func SetFixAgentForTest(agent func(ctx context.Context, dir string, log io.Writer, args []string) error) func() {
	prev := fixAgent
	fixAgent = agent
	return func() { fixAgent = prev }
}

// Run is the whole background update the terminal runs once the user says
// yes: it saves local changes in a commit, merges tag, has an AI agent fix
// clashes, builds and tests, and has the agent fix a failing build or test
// run, writing everything to the log at [LogPath]. It returns the files
// the agent fixed clashes in and the staged version, ready for [Install]
// (desktop design 5-6, 2.19; G-19).
func Run(ctx context.Context, dir, tag string) (fixed []string, staged string, err error) {
	log, err := CreateLog()
	if err != nil {
		return nil, "", err
	}
	defer log.Close()
	clashes, err := Merge(ctx, dir, tag)
	if err != nil {
		return nil, "", err
	}
	if len(clashes) > 0 {
		fmt.Fprintf(log, "Clashes in %s; an AI agent is fixing them.\n", strings.Join(clashes, ", "))
		err = Fix(ctx, dir, tag, clashes, "", log)
	} else if err = Build(ctx, dir, tag, log); err != nil && !errors.Is(err, ErrDirty) {
		fmt.Fprintf(log, "%v\nAn AI agent is fixing this.\n", err)
		err = Fix(ctx, dir, tag, nil, err.Error(), log)
	}
	if err != nil {
		return clashes, "", err
	}
	return clashes, Staged(ctx, dir, tag), nil
}
