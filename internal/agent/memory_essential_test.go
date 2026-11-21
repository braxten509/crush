package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A memory marked "essential: true" reaches sub-agents in full; an unmarked
// one stays a line in the index, and the block keeps to its size limit.
func TestEssentialMemories(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("MEMORY.md", "- [Video](video.md) — one frame at a time\n")
	write("video.md", "---\nname: video\ndescription: stream frames\nmetadata:\n  type: feedback\n  essential: true\n---\nRead one frame at a time.\n")
	write("other.md", "---\nname: other\ndescription: not essential\nmetadata:\n  type: project\n---\nSome project detail.\n")
	write("late.md", "---\nname: late\ndescription: mentions it in the body only\n---\nessential: true\n")

	got := essentialMemories(dir, 10_000, nil, memoryScope{})
	if !strings.Contains(got, "Read one frame at a time.") {
		t.Errorf("the essential memory's text is missing:\n%s", got)
	}
	if strings.Contains(got, "Some project detail.") || strings.Contains(got, "mentions it in the body") {
		t.Errorf("an unmarked memory got in:\n%s", got)
	}
	write("big.md", "---\nname: big\nmetadata:\n  essential: true\n---\n"+strings.Repeat("x", 5000)+"\n")
	if got := essentialMemories(dir, 1000, nil, memoryScope{}); strings.Contains(got, strings.Repeat("x", 5000)) || !strings.Contains(got, "big.md") {
		t.Errorf("a memory over the limit should be named, not pasted:\n%.300s", got)
	}
}

// A project note cannot leak into a neighboring project, including as an
// essential note. The titles and order of the entries may change freely.
func TestProjectMemoryScope(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "game")
	write := func(file, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("MEMORY.md", "## General\n\n- [General note](general.md)\n\n"+
		"## Project: "+project+"\n\n- [Game note](game.md)\n")
	write("general.md", "---\nmetadata:\n  essential: true\n---\nGlobal memory body.\n\n"+
		"## Project: "+project+"\n\nAn exception for the game.\n")
	write("game.md", "---\nmetadata:\n  essential: true\n---\nGame memory body.\n")

	for _, entry := range []struct {
		directory string
		applies   bool
	}{
		{filepath.Join(project, "screens"), true},
		{project + "-unrelated", false},
	} {
		for _, subAgent := range []bool{false, true} {
			prompt := formatMemoryInstructions(dir, entry.directory, subAgent)
			if !strings.Contains(prompt, "(general.md)") || strings.Contains(prompt, "Global memory body.") != subAgent {
				t.Errorf("general memory delivery failed for %s (sub-agent %t)", entry.directory, subAgent)
			}
			if strings.Contains(prompt, "game.md") != entry.applies {
				t.Errorf("wrong project index entries for %s (sub-agent %t)", entry.directory, subAgent)
			}
			if strings.Contains(prompt, "Game memory body.") != (entry.applies && subAgent) {
				t.Errorf("wrong essential project memory delivery for %s (sub-agent %t)", entry.directory, subAgent)
			}
			if strings.Contains(prompt, "An exception for the game.") != (entry.applies && subAgent) {
				t.Errorf("wrong project exception delivery for %s (sub-agent %t)", entry.directory, subAgent)
			}
		}
	}
}
