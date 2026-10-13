package diffreview

import (
	"testing"

	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/stretchr/testify/require"
)

// Whole-file edits of one file combine into one diff, from before the first
// edit to after the last, with real line numbers.
func TestBuildCombinesFullEdits(t *testing.T) {
	t.Parallel()

	v1 := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\n"
	v2 := "a\nB\nc\nd\ne\nf\ng\nh\ni\nj\n"
	v3 := "a\nB\nc\nd\ne\nf\ng\nh\ni\nJ\n"
	files := Build([]Edit{
		{Path: "/p/x.txt", Before: v1, After: v2, Full: true},
		{Path: "/p/new.txt", Before: "", After: "one\ntwo\n", Full: true},
		{Path: "/p/x.txt", Before: v2, After: v3, Full: true},
	})
	require.Len(t, files, 2)

	x := files[0]
	require.Equal(t, "/p/x.txt", x.Path)
	require.Equal(t, Modified, x.Kind)
	require.Equal(t, 2, x.Adds)
	require.Equal(t, 2, x.Dels)
	require.Equal(t, Line{Kind: Hunk, Text: "@@ -1,5 +1,5 @@"}, x.Lines[0])
	require.Equal(t, Line{Kind: Del, Old: 2, Text: "b"}, x.Lines[2])
	require.Equal(t, Line{Kind: Add, New: 2, Text: "B"}, x.Lines[3])
	last := x.Lines[len(x.Lines)-1]
	require.Equal(t, Line{Kind: Add, New: 10, Text: "J"}, last)

	n := files[1]
	require.Equal(t, Added, n.Kind)
	require.Equal(t, 2, n.Adds)
	require.Equal(t, Line{Kind: Add, New: 1, Text: "one"}, n.Lines[1])

	adds, dels := Stats(files)
	require.Equal(t, 4, adds)
	require.Equal(t, 2, dels)
}

func TestSnapshotPresenceAndNetChanges(t *testing.T) {
	t.Parallel()
	state := func(text string) *filechange.State {
		return &filechange.State{Content: text, Size: int64(len(text)), Mode: 0o644}
	}
	edit := func(path string, before, after *filechange.State) Edit {
		return Edit{Path: path, Snapshot: &filechange.Change{Path: path, Before: before, After: after}}
	}
	files := Build([]Edit{
		edit("emptied", state("text\n"), state("")),
		edit("filled", state(""), state("text\n")),
		edit("new-empty", nil, state("")),
		edit("gone-empty", state(""), nil),
		edit("temporary", nil, state("temporary")),
		edit("temporary", state("temporary"), nil),
		edit("reverted", state("old"), state("new")),
		edit("reverted", state("new"), state("old")),
		edit("twice", state("first\n"), state("second\n")),
		edit("twice", state("second\n"), state("last\n")),
	})
	require.Len(t, files, 5)
	require.Equal(t, Modified, files[0].Kind, "empty is different from deleted")
	require.Equal(t, Modified, files[1].Kind, "empty is different from absent")
	require.Equal(t, Added, files[2].Kind)
	require.Equal(t, "Added empty file", files[2].Lines[0].Text)
	require.Equal(t, Deleted, files[3].Kind)
	require.Equal(t, "Deleted empty file", files[3].Lines[0].Text)
	require.Equal(t, 1, files[4].Adds)
	require.Equal(t, 1, files[4].Dels)
	require.Equal(t, "first", files[4].Lines[1].Text)
	require.Equal(t, "last", files[4].Lines[2].Text)
}

func TestSnapshotBinaryAndModeChanges(t *testing.T) {
	t.Parallel()
	files := Build([]Edit{
		{Path: "image", Snapshot: &filechange.Change{Path: "image",
			Before: &filechange.State{Digest: "old", Size: 50, Mode: 0o644, Omitted: "Binary file"},
			After:  &filechange.State{Digest: "new", Size: 50, Mode: 0o644, Omitted: "Binary file"},
		}},
		{Path: "script", Snapshot: &filechange.Change{Path: "script",
			Before: &filechange.State{Content: "script", Mode: 0o644},
			After:  &filechange.State{Content: "script", Mode: 0o755},
		}},
	})
	require.Len(t, files, 2)
	require.Equal(t, "Binary file; 50 → 50 bytes", files[0].Lines[0].Text)
	require.Contains(t, files[1].Lines[0].Text, "-rw-r--r-- → -rwxr-xr-x")
	adds, dels := Stats(files)
	require.Zero(t, adds)
	require.Zero(t, dels)
}

func TestParallelSnapshotCompletionOrder(t *testing.T) {
	t.Parallel()
	state := func(text string) *filechange.State { return &filechange.State{Content: text, Mode: 0o644} }
	files := Build([]Edit{
		// The first tool in the chat finished last.
		{Path: "file", Snapshot: &filechange.Change{Path: "file", Order: 2, Before: state("middle\n"), After: state("last\n")}},
		{Path: "file", Snapshot: &filechange.Change{Path: "file", Order: 1, Before: state("first\n"), After: state("middle\n")}},
	})
	require.Len(t, files, 1)
	require.Equal(t, "first", files[0].Lines[1].Text)
	require.Equal(t, "last", files[0].Lines[2].Text)
}

// Edits that only know the replaced text show on their own, without line
// numbers; unchanged edits show nothing.
func TestBuildSnippetEdits(t *testing.T) {
	t.Parallel()

	files := Build([]Edit{
		{Path: "/p/x.go", Before: "return 5", After: "return 500"},
		{Path: "/p/x.go", Before: "same", After: "same"},
	})
	require.Len(t, files, 1)
	require.Equal(t, []Line{
		{Kind: Del, Text: "return 5"},
		{Kind: Add, Text: "return 500"},
	}, files[0].Lines)
}

// A unified diff (Crush's own write tool) keeps its hunks and numbers.
func TestBuildUnified(t *testing.T) {
	t.Parallel()

	diff := "--- a/p/x.go\n+++ b/p/x.go\n@@ -10,3 +10,3 @@\n ctx\n-old\n+new\n ctx2\n\\ No newline at end of file\n"
	files := Build([]Edit{{Path: "/p/x.go", Unified: diff}})
	require.Len(t, files, 1)
	require.Equal(t, []Line{
		{Kind: Hunk, Text: "@@ -10,3 +10,3 @@"},
		{Old: 10, New: 10, Text: "ctx"},
		{Kind: Del, Old: 11, Text: "old"},
		{Kind: Add, New: 11, Text: "new"},
		{Old: 12, New: 12, Text: "ctx2"},
	}, files[0].Lines)

	created := Build([]Edit{{Path: "/p/n.go", Unified: "--- /dev/null\n+++ b/p/n.go\n@@ -0,0 +1 @@\n+hi\n"}})
	require.Equal(t, Added, created[0].Kind)
}

func TestSnapshotSpecialFilesAreNotChanges(t *testing.T) {
	t.Parallel()
	tty := func(stamp string) *filechange.State {
		return &filechange.State{Digest: "stat:0:" + stamp, Mode: 0o20620, Omitted: "Special file (Dc)"}
	}
	files := Build([]Edit{{Path: "/dev/tty", Snapshot: &filechange.Change{Path: "/dev/tty", Before: tty("1"), After: tty("2")}}})
	require.Empty(t, files, "a terminal whose timestamp moved is not an edit")
}

// A deleted file counts as a removed file, and files whose text wasn't
// compared have no line counts, rather than "+0 −0".
func TestRemovedAndUncomparedFilesHaveNoLineCounts(t *testing.T) {
	t.Parallel()
	big := func(digest string, size int64) *filechange.State {
		return &filechange.State{Digest: digest, Size: size, Mode: 0o755, Omitted: "File exceeds 1048576 byte text preview limit"}
	}
	files := Build([]Edit{
		{Path: "/w/crush-readonly.new", Snapshot: &filechange.Change{Path: "/w/crush-readonly.new", Before: big("new", 136414586)}},
		{Path: "/w/crush", Snapshot: &filechange.Change{Path: "/w/crush", Before: big("old", 136422753), After: big("new", 136414586)}},
	})
	require.Len(t, files, 2)
	require.Equal(t, Deleted, files[0].Kind)
	require.False(t, files[0].HasEdits(), "a deleted file isn't an edit")
	require.False(t, files[1].Counted(), "a file too large to compare has no line counts")
	require.False(t, AnyCounted(files))
	require.Equal(t, 1, Removed(files))
	require.Equal(t, "removed 1 file · 1 file edited", Summary(files))

	// A deleted text file is one removed file, not its lines.
	text := Build([]Edit{
		{Path: "/w/a.go", Snapshot: &filechange.Change{Path: "/w/a.go", Before: &filechange.State{Content: "one\ntwo\n", Mode: 0o644}}},
		{Path: "/w/b.go", Snapshot: &filechange.Change{Path: "/w/b.go", Before: &filechange.State{Content: "x\n", Mode: 0o644}, After: &filechange.State{Content: "y\n", Mode: 0o644}}},
	})
	adds, dels := Stats(text)
	require.Equal(t, [2]int{1, 1}, [2]int{adds, dels})
	require.Equal(t, "removed 1 file · 1 file edited", Summary(text))
}
