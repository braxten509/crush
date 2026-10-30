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

	got := essentialMemories(dir, 10_000)
	if !strings.Contains(got, "Read one frame at a time.") {
		t.Errorf("the essential memory's text is missing:\n%s", got)
	}
	if strings.Contains(got, "Some project detail.") || strings.Contains(got, "mentions it in the body") {
		t.Errorf("an unmarked memory got in:\n%s", got)
	}
	write("big.md", "---\nname: big\nmetadata:\n  essential: true\n---\n"+strings.Repeat("x", 5000)+"\n")
	if got := essentialMemories(dir, 1000); strings.Contains(got, strings.Repeat("x", 5000)) || !strings.Contains(got, "big.md") {
		t.Errorf("a memory over the limit should be named, not pasted:\n%.300s", got)
	}
}
