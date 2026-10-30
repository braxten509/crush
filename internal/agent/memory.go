package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The shared memory is one folder of memory files that every agent the
// user runs reads and writes. Every CLI receives its index and writing rules
// from Crush, independently of the CLI's native memory settings.

const memoryIndexLimit = 25_000 // what Claude Code loads of MEMORY.md

// essentialLimit caps the full text of essential memories given to sub-agents.
const essentialLimit = 12_000

func memoryDir() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "agent-memory")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "agent-memory")
}

// sharedMemoryEnv sets the shared memory for one Crush process: "off" leaves
// it out, "readonly" gives every agent the sub-agents' read-only version (for
// runs that must not change what other agents remember, like benchmarks).
const sharedMemoryEnv = "CRUSH_SHARED_MEMORY"

func sharedMemoryOff() bool      { return os.Getenv(sharedMemoryEnv) == "off" }
func sharedMemoryReadOnly() bool { return os.Getenv(sharedMemoryEnv) == "readonly" }

// memoryInstructions tells a CLI where the shared memory is, what's in it
// and how to add to it. Sub-agents only read it. It's empty when there is
// no memory yet, or when CRUSH_SHARED_MEMORY=off.
func memoryInstructions(subAgent bool) string {
	if testing.Testing() || sharedMemoryOff() {
		return ""
	}
	subAgent = subAgent || sharedMemoryReadOnly()
	dir := memoryDir()
	index, err := os.ReadFile(filepath.Join(dir, "MEMORY.md"))
	if err != nil {
		return ""
	}
	if len(index) > memoryIndexLimit {
		index = index[:memoryIndexLimit]
	}
	write := memoryWriteRules
	if subAgent {
		write = "The memory is read-only for you as a sub-agent: don't write to it. If you learn something worth remembering, say so in your final report."
		// Sub-agents rarely open memory files, so must-follow rules come in full.
		if essential := essentialMemories(dir, essentialLimit); essential != "" {
			write += "\n\n" + essential
		}
	}
	return fmt.Sprintf(`<shared_memory>
You share a persistent, file-based memory with the other agents the user runs (Claude Code and every agent CLI in Crush). It lives in %s. Its index, MEMORY.md, is below: one line per memory file. Sections marked "Project:" only apply when working in that project.

Read a memory file when its line looks relevant to the task. Follow feedback memories as the user's standing instructions. Memories reflect what was true when they were written: if one names a file, function or flag, check it still exists before relying on it.

%s

<MEMORY.md>
%s
</MEMORY.md>
</shared_memory>`, dir, write, index)
}

// essentialMemories is the full text of every memory whose frontmatter says
// "essential: true", in file name order, up to limit bytes; one that doesn't
// fit is only named, so the agent knows to read it.
func essentialMemories(dir string, limit int) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var b strings.Builder
	var skipped []string
	for _, e := range entries {
		if e.IsDir() || e.Name() == "MEMORY.md" || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		body, essential := essentialBody(string(data))
		if err != nil || !essential {
			continue
		}
		part := fmt.Sprintf("<memory file=%q>\n%s\n</memory>\n", e.Name(), body)
		if b.Len()+len(part) > limit {
			skipped = append(skipped, e.Name())
			continue
		}
		b.WriteString(part)
	}
	if b.Len() == 0 && len(skipped) == 0 {
		return ""
	}
	out := "These memories are essential: follow them exactly, whatever your task says.\n<essential_memories>\n" + b.String()
	if len(skipped) > 0 {
		out += "Also essential, read before you start: " + strings.Join(skipped, ", ") + " (in the memory folder).\n"
	}
	return out + "</essential_memories>"
}

// essentialBody returns a memory's text without its frontmatter, and whether
// that frontmatter has "essential: true".
func essentialBody(memory string) (string, bool) {
	if !strings.HasPrefix(memory, "---\n") {
		return "", false
	}
	front, body, found := strings.Cut(memory[4:], "\n---\n")
	if !found {
		return "", false
	}
	for _, line := range strings.Split(front, "\n") {
		if strings.TrimSpace(line) == "essential: true" {
			return strings.TrimSpace(body), true
		}
	}
	return "", false
}

const memoryWriteRules = `Save a memory when you learn something worth keeping across sessions, or when the user asks you to remember something:
- user: who the user is, their role, expertise and preferences.
- feedback: guidance on how to work, both corrections and approaches they confirmed. State the rule, then **Why:** and **How to apply:** lines.
- project: ongoing work, goals or constraints the code doesn't show. Write dates as absolute dates.
- reference: pointers to outside resources (URLs, dashboards, tickets).
Don't save what the code, git history, CLAUDE.md or AGENTS.md already record, or what only matters to this conversation.

Each memory is one file holding one fact, with this frontmatter:
---
name: <short-kebab-case-slug>
description: <one-line summary, used to decide relevance>
metadata:
  type: user | feedback | project | reference
---
<the fact; link related memories with [[their-name]]>

Add "  essential: true" under metadata only when the user marks a rule as must-follow: sub-agents then get its full text, so keep those memories few and short.

Before saving, check for a file that already covers it and update that one instead; delete memories that turn out to be wrong. After writing a file, add a one-line pointer to MEMORY.md under the right section ("General", or "Project: <path>" for project-only facts): - [Title](file.md) — hook. Never put memory content in MEMORY.md itself.`
