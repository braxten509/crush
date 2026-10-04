package filechange

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLiteralTransferCommands(t *testing.T) {
	t.Parallel()
	for command, want := range map[string]string{
		`git clone --no-hardlinks --branch main /source /destination`:    "checkout",
		`git -C '/source dir' clone . '/destination dir'`:                "checkout",
		`cp -a source destination`:                                       "copy",
		`echo 'git clone source destination'`:                            "",
		`git clone source destination && echo edited > destination/file`: "",
		`cp source destination; printf changed > destination`:            "",
		`git clone "$SOURCE" destination`:                                "",
		`git clone source destination > output`:                          "",
		`git -c alias.inspect=clone inspect source destination`:          "",
	} {
		t.Run(command, func(t *testing.T) { require.Equal(t, want, CommandTransferKind(command)) })
	}
}

func TestTransferDoesNotDuplicateUnchangedSnapshot(t *testing.T) {
	t.Parallel()
	content := "unique copied contents"
	change := Change{Path: "file", After: &State{Content: content}, Transfer: &Transfer{Kind: "copy"}}
	encoded, err := json.Marshal(change)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(encoded), content))
	var decoded Change
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Equal(t, content, decoded.ImportedState().Content)
}

func TestNativeCommandReportPreservesImportedBaseline(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")
	base := &State{Content: "copied\n", Mode: 0o600}
	report := &CommandReview{root: root, store: newSnapshotStore(), changes: []Change{
		{Path: path, After: base, Transfer: &Transfer{Kind: "copy"}},
	}}
	require.NoError(t, os.WriteFile(path, []byte("edited\n"), 0o600))
	original := report.changes[0]
	review := report.Finish()
	require.Len(t, review.Changes, 1)
	require.Equal(t, "copied\n", review.Changes[0].ImportedState().Content)
	require.Equal(t, "edited\n", review.Changes[0].After.Content)
	require.Nil(t, original.Transfer.Baseline, "saved reports stay immutable")
}

func TestCloneAndEditWithinSameReportedCommand(t *testing.T) {
	root := t.TempDir()
	p := newProcessReview(root, nil)
	p.Begin("shell", "run-fixture")
	wrapper := p.executed(nil, []string{"sh", "-c", "run-fixture"})
	clone := p.executed(wrapper, []string{"git", "clone", "source", root})
	path := filepath.Join(root, "file.txt")
	p.before(clone, path)
	require.NoError(t, os.WriteFile(path, []byte("copied\n"), 0o600))
	// The wrapper existed first, but its mutation happened after the clone.
	p.before(wrapper, path)
	require.NoError(t, os.WriteFile(path, []byte("edited\n"), 0o600))
	review := p.End("shell")
	require.Len(t, review.Changes, 1)
	change := review.Changes[0]
	require.Nil(t, change.Before)
	require.NotNil(t, change.Transfer)
	require.Equal(t, "checkout", change.Transfer.Kind)
	require.Equal(t, "copied\n", change.Transfer.Baseline.Content)
	require.Equal(t, "edited\n", change.After.Content)
}

func TestCopyOverExistingFileRemainsAnEdit(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	require.NoError(t, os.WriteFile(path, []byte("old\n"), 0o600))
	p := newProcessReview(root, nil)
	p.Begin("copy", "cp source file.txt")
	record := p.executed(nil, []string{"cp", "source", "file.txt"})
	p.before(record, path)
	require.NoError(t, os.WriteFile(path, []byte("copied\n"), 0o600))
	review := p.End("copy")
	require.Len(t, review.Changes, 1)
	require.Nil(t, review.Changes[0].Transfer)
	require.Equal(t, "old\n", review.Changes[0].Before.Content)
}
