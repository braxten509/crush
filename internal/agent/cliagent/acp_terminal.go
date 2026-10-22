package cliagent

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"unicode/utf8"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/filechange"
)

// Crush runs an ACP agent's commands when the agent asks (terminal/create
// and the rest of ACP's terminal methods). Grok runs its own in one
// long-lived shell, where Crush's watcher can't tell commands apart; run
// here, each command is its own watched process, like Crush's own shell
// tool, and the files it changes become the record of the tool call it
// belongs to.

// acpTerminals are the commands one turn asked Crush to run.
type acpTerminals struct {
	dir   string
	mu    sync.Mutex
	next  int
	terms map[string]*acpTerminal
	// released are terminals the agent let go whose changes its call's
	// result has not taken yet.
	released map[string]*acpTerminal
	// open are the shell calls announced and not yet finished, in order;
	// owner is the call each terminal belongs to.
	open  []string
	owner map[string]string
}

// acpTerminal is one command: its output, kept to the agent's limit, and
// once it ends, its exit and the files it changed.
type acpTerminal struct {
	cmd       *exec.Cmd
	limit     int
	mu        sync.Mutex
	out       []byte
	truncated bool
	done      chan struct{}
	exit      map[string]any
	changes   *filechange.Review
}

func newACPTerminals(dir string) *acpTerminals {
	return &acpTerminals{dir: dir, terms: map[string]*acpTerminal{}, released: map[string]*acpTerminal{}, owner: map[string]string{}}
}

// Write keeps the newest output within the limit, cut at a character.
func (term *acpTerminal) Write(b []byte) (int, error) {
	term.mu.Lock()
	defer term.mu.Unlock()
	term.out = append(term.out, b...)
	if term.limit > 0 && len(term.out) > term.limit {
		cut := len(term.out) - term.limit
		for cut < len(term.out) && !utf8.RuneStart(term.out[cut]) {
			cut++
		}
		term.out = append([]byte(nil), term.out[cut:]...)
		term.truncated = true
	}
	return len(b), nil
}

// handle answers one terminal request.
func (ts *acpTerminals) handle(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	var params struct {
		TerminalID string   `json:"terminalId"`
		Command    string   `json:"command"`
		Args       []string `json:"args"`
		Env        []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"env"`
		Cwd             string `json:"cwd"`
		OutputByteLimit int    `json:"outputByteLimit"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	if method == "terminal/create" {
		var env []string
		for _, e := range params.Env {
			env = append(env, e.Name+"="+e.Value)
		}
		return ts.create(params.Command, params.Args, env, cmp.Or(params.Cwd, ts.dir), params.OutputByteLimit)
	}
	ts.mu.Lock()
	term := ts.terms[params.TerminalID]
	ts.mu.Unlock()
	if term == nil {
		return nil, fmt.Errorf("no terminal %q", params.TerminalID)
	}
	switch method {
	case "terminal/output":
		term.mu.Lock()
		result := map[string]any{"output": string(term.out), "truncated": term.truncated}
		term.mu.Unlock()
		select {
		case <-term.done:
			result["exitStatus"] = term.exit
		default:
		}
		return result, nil
	case "terminal/wait_for_exit":
		select {
		case <-term.done:
			return term.exit, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	case "terminal/kill":
		term.kill()
		return map[string]any{}, nil
	case "terminal/release":
		term.kill()
		ts.mu.Lock()
		delete(ts.terms, params.TerminalID)
		if _, owned := ts.owner[params.TerminalID]; owned {
			ts.released[params.TerminalID] = term
		}
		ts.mu.Unlock()
		return map[string]any{}, nil
	}
	return nil, fmt.Errorf("%s is not supported by Crush", method)
}

// create starts a command as its own watched process. A bare command line
// runs through bash, as the agent's own shell would.
func (ts *acpTerminals) create(command string, args, env []string, dir string, limit int) (any, error) {
	cmd := exec.Command(command, args...)
	if len(args) == 0 {
		cmd = exec.Command("bash", "-c", command)
	}
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	ownGroup(cmd)
	term := &acpTerminal{cmd: cmd, limit: limit, done: make(chan struct{})}
	cmd.Stdout, cmd.Stderr = term, term
	ctx, report := filechange.WithCommandReview(context.Background(), ts.dir, config.GlobalCacheDir(), filepath.Dir(config.GlobalConfigData()))
	observer, err := filechange.StartCommand(ctx, cmd)
	if err != nil {
		return nil, err
	}
	ts.mu.Lock()
	ts.next++
	id := "crush-term-" + strconv.Itoa(ts.next)
	ts.terms[id] = term
	if len(ts.open) == 1 {
		// An agent that doesn't show the terminal in its call: the one
		// shell call running is the one that asked.
		ts.owner[id] = ts.open[0]
	}
	ts.mu.Unlock()
	go func() {
		_ = observer.Wait(cmd)
		term.exit = map[string]any{"exitCode": nil, "signal": nil}
		if state := cmd.ProcessState; state != nil && state.Exited() {
			term.exit["exitCode"] = state.ExitCode()
		} else if state != nil {
			term.exit["signal"] = state.String()
		}
		term.changes = report.Finish()
		close(term.done)
	}()
	return map[string]any{"terminalId": id}, nil
}

func (term *acpTerminal) kill() {
	select {
	case <-term.done:
	default:
		if term.cmd.Process != nil {
			(&proc{cmd: term.cmd}).kill()
		}
	}
}

// claim ties a terminal shown in a tool call's content to that call.
func (ts *acpTerminals) claim(terminalID, callID string) {
	ts.mu.Lock()
	ts.owner[terminalID] = callID
	ts.mu.Unlock()
}

// attach adds the changes of a call's finished commands to its result,
// and keeps track of the shell calls that are running.
func (ts *acpTerminals) attach(emit func(Event) error) func(Event) error {
	return func(e Event) error {
		switch {
		case e.Type == EventToolCall && e.Name == tools.BashToolName:
			ts.mu.Lock()
			ts.open = append(ts.open, e.ID)
			ts.mu.Unlock()
		case e.Type == EventToolResult:
			e.Metadata = filechange.WithReview(ts.take(e.ID, e.Metadata))
		}
		return emit(e)
	}
}

// take removes call's shell call and merges the changes of its finished
// commands into the review metadata already carries.
func (ts *acpTerminals) take(call, metadata string) (string, *filechange.Review) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for i, id := range ts.open {
		if id == call {
			ts.open = append(ts.open[:i:i], ts.open[i+1:]...)
			break
		}
	}
	metadata, review := filechange.TakeReview(metadata)
	for id, owner := range ts.owner {
		if owner != call {
			continue
		}
		term := ts.findLocked(id)
		if term == nil {
			continue
		}
		select {
		case <-term.done:
		default:
			continue // still running, as a background command does
		}
		delete(ts.owner, id)
		delete(ts.released, id)
		if term.changes == nil {
			continue
		}
		if review == nil {
			review = &filechange.Review{Root: ts.dir}
		}
		review.Changes = append(review.Changes, term.changes.Changes...)
		review.Deletions = review.Deletions || term.changes.Deletions
	}
	return metadata, review
}

// findLocked finds a terminal, released ones included.
func (ts *acpTerminals) findLocked(id string) *acpTerminal {
	if term := ts.terms[id]; term != nil {
		return term
	}
	return ts.released[id]
}

// closeAll stops every command still running when the turn ends.
func (ts *acpTerminals) closeAll() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for _, term := range ts.terms {
		term.kill()
	}
	for _, term := range ts.released {
		term.kill()
	}
}
