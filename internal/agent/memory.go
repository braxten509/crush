package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// The shared memory is one folder of memory files that every agent the
// user runs reads and writes. Every CLI receives its index and writing rules
// from Crush, independently of the CLI's native memory settings.

const memoryIndexLimit = 25_000 // what Claude Code loads of MEMORY.md

func memoryDir() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "agent-memory")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "agent-memory")
}

// memoryInstructions tells a CLI where the shared memory is, what's in it
// and how to add to it. Sub-agents only read it. It's empty when there is
// no memory yet.
func memoryInstructions(subAgent bool) string {
	if testing.Testing() {
		return ""
	}
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

Before saving, check for a file that already covers it and update that one instead; delete memories that turn out to be wrong. After writing a file, add a one-line pointer to MEMORY.md under the right section ("General", or "Project: <path>" for project-only facts): - [Title](file.md) — hook. Never put memory content in MEMORY.md itself.`
