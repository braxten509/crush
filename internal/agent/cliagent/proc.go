package cliagent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"mvdan.cc/sh/v3/shell"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/filechange"
)

// proc is a CLI child process speaking newline-delimited JSON on stdio.
type proc struct {
	fileReview *filechange.ProcessReview
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	lines      *bufio.Scanner
	stderr     *tailBuffer
	mu         sync.Mutex
	closed     atomic.Bool
	quitOnce   sync.Once
	quit       chan struct{} // closed by finish; see readLines
}

func startProc(dir, name string, args ...string) (*proc, error) {
	return startProcEnv(dir, nil, name, args...)
}

// startProcEnv is startProc with extra environment variables.
func startProcEnv(dir string, env []string, name string, args ...string) (*proc, error) {
	return startProcCommand(exec.Command(name, args...), dir, env)
}

func startReviewProc(dir string, env []string, review bool, name string, args ...string) (*proc, error) {
	return startProcCommandReview(exec.Command(name, args...), dir, env, review)
}

func startProcCommand(cmd *exec.Cmd, dir string, env []string) (*proc, error) {
	return startProcCommandReview(cmd, dir, env, false)
}

func startProcCommandReview(cmd *exec.Cmd, dir string, env []string, review bool) (*proc, error) {
	cmd.Dir = dir
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	ownGroup(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	p := &proc{cmd: cmd, stdin: stdin, stderr: &tailBuffer{max: 8 << 10}, quit: make(chan struct{})}
	cmd.Stderr = p.stderr
	p.lines = bufio.NewScanner(stdout)
	p.lines.Buffer(make([]byte, 0, 1<<20), 64<<20)
	if review {
		p.fileReview, err = filechange.StartProcess(cmd, dir, config.GlobalCacheDir(), filepath.Dir(config.GlobalConfigData()))
	} else {
		err = cmd.Start()
	}
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("starting %s: %w", cmd.Path, err)
	}
	return p, nil
}

func (p *proc) send(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed.Load() {
		return io.ErrClosedPipe
	}
	_, err = p.stdin.Write(append(data, '\n'))
	return err
}

// closeInput ends stdin, which both CLIs treat as "exit when done".
func (p *proc) closeInput() {
	if p.closed.CompareAndSwap(false, true) {
		_ = p.stdin.Close()
	}
}

// readLines delivers stdout lines on a channel, closed when output ends,
// for procs that outlive one turn. Once the proc is finished, lines nobody
// takes are dropped so the CLI never blocks writing on its way out.
func (p *proc) readLines() <-chan []byte {
	ch := make(chan []byte)
	go func() {
		defer close(ch)
		for p.lines.Scan() {
			select {
			case ch <- bytes.Clone(p.lines.Bytes()):
			case <-p.quit:
			}
		}
	}()
	return ch
}

// finish waits for the process, killing it if it lingers.
func (p *proc) finish() {
	timer := time.AfterFunc(5*time.Second, p.kill)
	defer timer.Stop()
	p.quitOnce.Do(func() { close(p.quit) })
	p.closeInput()
	if err := p.fileReview.Wait(p.cmd); err != nil {
		slog.Debug("Agent CLI exited", "cmd", p.cmd.Path, "error", err, "stderr", p.stderr.String())
	}
}

// openCalls tracks the shell commands a turn is waiting on. Stopping a turn
// stops only those: ones the model or the user moved to the background keep
// running.
type openCalls struct {
	mu    sync.Mutex
	calls map[string]openCall
}

// openCall is a shell command and when the CLI reported it.
type openCall struct {
	command string
	words   []string // the command split as a shell would, if it could be
	at      time.Time
}

type openCallsKey struct{}

func (o *openCalls) track(emit func(Event) error) func(Event) error {
	return func(e Event) error {
		o.mu.Lock()
		switch e.Type {
		case EventToolCall:
			var in struct {
				Command string `json:"command"`
			}
			if _, ok := o.calls[e.ID]; !ok && json.Unmarshal([]byte(e.Input), &in) == nil && in.Command != "" {
				words, _ := shell.Fields(in.Command, func(string) string { return "" })
				o.calls[e.ID] = openCall{command: in.Command, words: words, at: time.Now()}
			}
		case EventToolResult:
			delete(o.calls, e.ID)
		}
		o.mu.Unlock()
		return emit(e)
	}
}

func (o *openCalls) list() []openCall {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Collect(maps.Values(o.calls))
}

// watchCancel calls interrupt once ctx is done, then gives the CLI a few
// seconds to wind down before killing it. The returned func stops watching.
func (p *proc) watchCancel(ctx context.Context, interrupt func()) func() {
	calls, _ := ctx.Value(openCallsKey{}).(*openCalls)
	if calls != nil {
		foregroundCalls.Store(p.cmd.Process.Pid, calls)
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			timer := time.AfterFunc(5*time.Second, func() {
				p.kill()
				p.closeInput()
			})
			defer timer.Stop()
			// Stop the commands the turn is waiting on first: some CLIs
			// start them detached, where stopping the CLI doesn't reach
			// them. Older ones run in the background and stay.
			if calls, _ := ctx.Value(openCallsKey{}).(*openCalls); calls != nil {
				p.killCommands(calls.list())
			}
			interrupt()
			<-done
		case <-done:
		}
	}()
	return func() {
		close(done)
		if calls != nil {
			foregroundCalls.CompareAndDelete(p.cmd.Process.Pid, calls)
		}
	}
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func (t *tailBuffer) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf.Write(b)
	if over := t.buf.Len() - t.max; over > 0 {
		t.buf.Next(over)
	}
	return len(b), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(t.buf.String())
}

// exitError explains a CLI that stopped without finishing the turn.
func exitError(name string, p *proc) error {
	if msg := p.stderr.String(); msg != "" {
		return fmt.Errorf("%s exited: %s", name, lastLines(msg, 5))
	}
	return fmt.Errorf("%s exited before finishing the turn", name)
}

func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// Observe shell tool calls at the protocol boundary so mutations are attached
// to the action which made them, including tools that exit with an error.
func (p *proc) reviewEvents(emit func(Event) error) func(Event) error {
	return func(event Event) error {
		if p.fileReview != nil {
			if event.Type == EventToolCall && event.Name == tools.BashToolName {
				var input struct {
					Command string `json:"command"`
				}
				if json.Unmarshal([]byte(event.Input), &input) == nil {
					p.fileReview.Begin(event.ID, input.Command)
				}
			}
			if event.Type == EventToolResult {
				event.Metadata = filechange.WithReview(event.Metadata, p.fileReview.End(event.ID))
			}
		}
		return emit(event)
	}
}
