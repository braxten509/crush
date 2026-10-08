package filechange

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestIdenticalShellCommandsKeepSeparateOccurrences(t *testing.T) {
	for _, late := range []bool{false, true} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("late=%t/reverse=%t", late, reverse), func(t *testing.T) {
				root := visibleRestoreRoot(t)
				p := newProcessReview(root, nil)
				if !late {
					p.Begin("first", "python3 script.py")
					p.Begin("second", "python3 script.py")
				}
				first := p.executed(nil, []string{"python3", "script.py"})
				second := p.executed(nil, []string{"python3", "script.py"})
				write := func(record *invocation, name string) {
					path := filepath.Join(root, name)
					p.before(record, path)
					require.NoError(t, os.WriteFile(path, []byte(name), 0600))
				}
				write(first, "first.txt")
				write(second, "second.txt")
				// A child whose argv also matches belongs to its existing subtree.
				child := p.executed(first, []string{"python3", "script.py"})
				write(child, "child.txt")
				if late {
					p.Begin("first", "python3 script.py")
					p.Begin("second", "python3 script.py")
				}
				var firstReview, secondReview *Review
				if reverse {
					secondReview = p.End("second")
					write(child, "later-first.txt")
					firstReview = p.End("first")
				} else {
					firstReview = p.End("first")
					write(second, "later-second.txt")
					secondReview = p.End("second")
				}
				require.NotNil(t, firstReview)
				require.NotNil(t, secondReview)
				for _, change := range firstReview.Changes {
					require.NotContains(t, filepath.Base(change.Path), "second")
				}
				for _, change := range secondReview.Changes {
					require.Contains(t, filepath.Base(change.Path), "second")
				}
				if reverse {
					require.Len(t, firstReview.Changes, 3)
					require.Len(t, secondReview.Changes, 1)
				} else {
					require.Len(t, firstReview.Changes, 2)
					require.Len(t, secondReview.Changes, 2)
				}
				require.Empty(t, p.invocations)
			})
		}
	}
}

func retainedPreviewBytes(p *ProcessReview) int {
	contents := map[*byte]int{}
	for _, record := range p.invocations {
		for _, value := range record.tracker.files {
			if value.state.Content != "" {
				contents[unsafe.StringData(value.state.Content)] = len(value.state.Content)
			}
		}
	}
	total := 0
	for _, size := range contents {
		total += size
	}
	return total
}

func TestProcessSnapshotsShareBudgetAcrossInvocations(t *testing.T) {
	root := visibleRestoreRoot(t)
	p := newProcessReview(root, nil)
	for index := range maxTextTotal/maxTextSize + 2 {
		path := filepath.Join(root, fmt.Sprintf("file-%d.txt", index))
		data := fmt.Sprintf("%08d", index) + strings.Repeat("x", maxTextSize-8)
		require.NoError(t, os.WriteFile(path, []byte(data), 0600))
		p.before(p.executed(nil, []string{"writer"}), path)
	}
	require.LessOrEqual(t, retainedPreviewBytes(p), maxTextTotal)
	require.Equal(t, maxTextTotal, retainedPreviewBytes(p))
}

func TestProcessSnapshotsReuseDuplicateBaselines(t *testing.T) {
	root := visibleRestoreRoot(t)
	path := filepath.Join(root, "large.txt")
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("x", maxTextSize)), 0600))
	p := newProcessReview(root, nil)
	for range 65 {
		p.before(p.executed(nil, []string{"writer"}), path)
	}
	require.Equal(t, maxTextSize, retainedPreviewBytes(p))
}

func TestLateShellReportsKeepReadOnlyOccurrenceSeparate(t *testing.T) {
	root := visibleRestoreRoot(t)
	p := newProcessReview(root, nil)
	p.executed(nil, []string{"python3", "script.py"})
	writer := p.executed(nil, []string{"python3", "script.py"})
	path := filepath.Join(root, "second.txt")
	p.before(writer, path)
	require.NoError(t, os.WriteFile(path, []byte("second"), 0600))
	p.Begin("read-only", "python3 script.py")
	require.Nil(t, p.End("read-only"))
	p.Begin("writer", "python3 script.py")
	review := p.End("writer")
	require.NotNil(t, review)
	require.Len(t, review.Changes, 1)
	require.Equal(t, path, review.Changes[0].Path)
	require.Equal(t, "second", review.Changes[0].After.Content)
}
