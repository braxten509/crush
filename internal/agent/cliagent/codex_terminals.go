package cliagent

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// A Codex process has one reader at a time, either the active turn or the
// idle drain. Both hand terminal events to the same table across turn changes.
type codexTerminals struct {
	mu   sync.Mutex
	jobs map[string]*codexTerminal
}

type codexTerminal struct {
	finish func(status, output string)
	output tailBuffer
}

func (r *codexRun) trackTerminal(id, command string, pid int) {
	if r.t.CodexBackground == nil || r.m.Guarded || r.t.NoTools {
		return
	}
	t := r.terminals
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.jobs[id] != nil {
		return
	}
	if t.jobs == nil {
		t.jobs = make(map[string]*codexTerminal)
	}
	if command == "" {
		var input struct{ Command string }
		_ = json.Unmarshal([]byte(r.calls[id][1]), &input)
		command = input.Command
	}
	job := &codexTerminal{finish: r.t.CodexBackground(r.t.SessionID, unwrapShell(command), pid), output: tailBuffer{max: 32 << 10}}
	if out := r.output[id]; out != nil {
		job.output.Write([]byte(out.String()))
	}
	t.jobs[id] = job
}

func (t *codexTerminals) busy() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.jobs) != 0
}

func (l *codexLive) readTerminal(raw []byte) bool {
	if l.terminals.busy() {
		var msg rpcMessage
		if json.Unmarshal(raw, &msg) == nil {
			l.terminals.consume(l.thread, msg)
		}
	}
	return false // keep draining until another turn takes the process
}

func (t *codexTerminals) consume(thread string, msg rpcMessage) bool {
	if msg.Method != "item/completed" && msg.Method != "item/commandExecution/outputDelta" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.jobs) == 0 {
		return false
	}
	var params codexParams
	if json.Unmarshal(msg.Params, &params) != nil || (params.ThreadID != "" && params.ThreadID != thread) {
		return false
	}
	if msg.Method == "item/commandExecution/outputDelta" {
		if job := t.jobs[params.ItemID]; job != nil {
			job.output.Write([]byte(params.Delta))
			return true
		}
		return false
	}
	item := params.Item
	job := t.jobs[item.ID]
	if job == nil || item.Type != "commandExecution" {
		return false
	}
	delete(t.jobs, item.ID)
	output := job.output.String()
	if item.AggregatedOutput != nil {
		output = *item.AggregatedOutput
	}
	status := item.Status
	if status == "" {
		status = "ended"
	}
	if item.ExitCode != nil {
		if *item.ExitCode != 0 {
			status = "failed"
		}
		output = fmt.Sprintf("Exit code %d.\n%s", *item.ExitCode, strings.TrimSpace(output))
	}
	job.finish(status, output)
	return true
}

func (t *codexTerminals) end() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, job := range t.jobs {
		delete(t.jobs, id)
		job.finish("interrupted", "Codex closed before reporting the command’s exit status.")
	}
}
