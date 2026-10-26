package diffreview

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Many scattered edits plus a moved block: the line counts equal Git's.
func TestLineCountsMatchGit(t *testing.T) {
	var before, after []string
	for i := range 600 {
		before = append(before, fmt.Sprintf("line %d", i))
	}
	for i, line := range before {
		switch {
		case i >= 100 && i < 160: // moved below
		case i%7 == 0:
			after = append(after, line+" changed")
		default:
			after = append(after, line)
		}
		if i == 450 {
			after = append(after, before[100:160]...)
		}
	}
	old, new := strings.Join(before, "\n")+"\n", strings.Join(after, "\n")+"\n"

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a"), []byte(old), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b"), []byte(new), 0o600))
	out, _ := exec.Command("git", "diff", "--no-index", "--numstat", filepath.Join(dir, "a"), filepath.Join(dir, "b")).Output()
	var gitAdds, gitDels int
	_, err := fmt.Sscanf(string(out), "%d\t%d", &gitAdds, &gitDels)
	require.NoError(t, err, string(out))

	adds, dels := Stats(Build([]Edit{{Path: "f.txt", Before: old, After: new, Full: true}}))
	require.Equal(t, [2]int{gitAdds, gitDels}, [2]int{adds, dels})
}
