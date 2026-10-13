package jsonfile

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// helperEnv makes the test binary act as a separate Crush process that
// adds keys to a shared file.
const helperEnv = "JSONFILE_TEST_WRITER"

func TestMain(m *testing.M) {
	if spec := os.Getenv(helperEnv); spec != "" {
		var path, name string
		var count int
		if _, err := fmt.Sscanf(spec, "%s %s %d", &path, &name, &count); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		for i := range count {
			key := name + "-" + strconv.Itoa(i)
			if err := Update(path, func(m *map[string]int) bool {
				if *m == nil {
					*m = map[string]int{}
				}
				(*m)[key] = i
				return true
			}); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// Separate processes changing one file at once keep every change.
func TestUpdateKeepsChangesFromSeveralProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.json")
	const processes, each = 4, 25
	var wait sync.WaitGroup
	errs := make(chan error, processes)
	for p := range processes {
		cmd := exec.Command(os.Args[0], "-test.run=^$")
		cmd.Env = append(os.Environ(), fmt.Sprintf("%s=%s p%d %d", helperEnv, path, p, each))
		wait.Add(1)
		go func() {
			defer wait.Done()
			if out, err := cmd.CombinedOutput(); err != nil {
				errs <- fmt.Errorf("%v: %s", err, out)
			}
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	saved := Read[map[string]int](path)
	require.Len(t, saved, processes*each)
	for p := range processes {
		for i := range each {
			require.Equal(t, i, saved[fmt.Sprintf("p%d-%d", p, i)])
		}
	}
	leftovers, err := filepath.Glob(path + ".*.tmp")
	require.NoError(t, err)
	require.Empty(t, leftovers, "temporary files are cleaned up")
}

func TestUpdateSkipsWritesAndRemovesEmptyValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "list.json")
	require.Nil(t, Read[[]string](path), "a missing file reads as the zero value")

	require.NoError(t, Update(path, func(list *[]string) bool { *list = append(*list, "a"); return true }))
	require.Equal(t, []string{"a"}, Read[[]string](path))

	require.NoError(t, Update(path, func(list *[]string) bool { *list = nil; return false }))
	require.Equal(t, []string{"a"}, Read[[]string](path), "change returning false writes nothing")

	require.NoError(t, Update(path, func(list *[]string) bool { *list = (*list)[:0]; return true }))
	_, err := os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist, "an empty list removes the file")

	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o600))
	require.Nil(t, Read[[]string](path), "an unreadable file reads as the zero value")
	require.NoError(t, Update(path, func(list *[]string) bool { *list = append(*list, "b"); return true }))
	require.Equal(t, []string{"b"}, Read[[]string](path))
}
